package fixture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const PathFrameBytes = 32 << 10

type PathEvent string

const (
	PathAccepted        PathEvent = "accepted"
	PathRequestFIN      PathEvent = "request_fin"
	PathEffectCommitted PathEvent = "effect_committed"
	PathWriterEntered   PathEvent = "writer_entered"
	PathWriterReturned  PathEvent = "writer_returned"
	PathResponseFIN     PathEvent = "response_fin"
	PathClosed          PathEvent = "closed"
)

// PathPlan is immutable once installed. A repeated request ID executes the
// same plan again and increments Effects again: this fixture never deduplicates.
// RequestBytes=-1 accepts an unknown-length stream, useful for control markers.
type PathPlan struct {
	RequestBytes, ResponseBytes                    int64
	Pattern, ResponseFirst, HoldRead, HoldResponse bool
	Observe                                        []string
}

type PathExchangeSnapshot struct {
	ID                           uint64
	RequestID                    string
	ReceivedBytes, ResponseBytes int64
	// WireReceivedBytes includes the envelope and invalid body bytes. Use
	// it, not only ReceivedBytes, to prove zero payload before READY.
	WireReceivedBytes                                              int64
	SocketBufferRequested                                          int
	SocketBufferNote                                               string
	RequestFIN, ResponseFIN, WriterEntered, WriterReturned, Closed bool
	Error, SHA256                                                  string
	Markers                                                        map[string]int
	Events                                                         map[PathEvent]int64
}
type PathSnapshot struct {
	Exchanges []PathExchangeSnapshot
	Effects   map[string]int
	// Workers counts active exchange groups, excluding the accept loop.
	// A response-first group's writer is joined before the group is Closed.
	Workers int
	Closed  bool
}

type PathExchange struct {
	target                 *PathTarget
	conn                   *net.TCPConn
	snapshot               PathExchangeSnapshot
	readGate, responseGate chan struct{}
	readOnce, responseOnce sync.Once
	plan                   PathPlan
}

// PathTarget is a bounded TCP test instrument, not a business server. Its
// only request envelope is "G3 <id>\n" followed by a body and TCP FIN.
// Body bytes are checked/hashed incrementally and never retained in full.
type PathTarget struct {
	ln        *net.TCPListener
	now       func() int64
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	plans     map[string]PathPlan
	exchanges map[uint64]*PathExchange
	effects   map[string]int
	next      uint64
	workers   int
	closed    bool
	changed   chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func NewPathTarget(now func() int64) (*PathTarget, error) {
	if now == nil {
		return nil, errors.New("path target requires controller clock")
	}
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &PathTarget{ln: ln, now: now, ctx: ctx, cancel: cancel, plans: map[string]PathPlan{}, exchanges: map[uint64]*PathExchange{}, effects: map[string]int{}, changed: make(chan struct{})}
	t.wg.Add(1)
	go t.accept()
	return t, nil
}
func (t *PathTarget) Addr() string { return t.ln.Addr().String() }
func validPathID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return false
		}
	}
	return true
}
func PathRequestHeader(id string) []byte { return []byte("G3 " + id + "\n") }
func (t *PathTarget) AddPlan(id string, p PathPlan) error {
	if !validPathID(id) || p.RequestBytes < -1 || p.ResponseBytes < 0 {
		return errors.New("invalid path request plan")
	}
	seen := map[string]bool{}
	for _, m := range p.Observe {
		if m == "" || len(m) > PathFrameBytes || seen[m] {
			return errors.New("invalid or duplicate observed marker")
		}
		seen[m] = true
	}
	p.Observe = append([]string(nil), p.Observe...)
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.plans[id]; ok {
		return errors.New("path request plan cannot be replaced")
	}
	if t.closed {
		return net.ErrClosed
	}
	t.plans[id] = p
	return nil
}
func (t *PathTarget) signalLocked() { close(t.changed); t.changed = make(chan struct{}) }
func (x *PathExchange) eventLocked(e PathEvent) {
	x.snapshot.Events[e] = x.target.now()
	x.target.signalLocked()
}
func (t *PathTarget) accept() {
	defer t.wg.Done()
	for {
		c, err := t.ln.AcceptTCP()
		if err != nil {
			return
		}
		bufferErr := errors.Join(c.SetReadBuffer(PathFrameBytes), c.SetWriteBuffer(PathFrameBytes))
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			_ = c.Close()
			return
		}
		t.next++
		x := &PathExchange{target: t, conn: c, readGate: make(chan struct{}), responseGate: make(chan struct{}), snapshot: PathExchangeSnapshot{ID: t.next, Markers: map[string]int{}, Events: map[PathEvent]int64{}}}
		x.snapshot.SocketBufferRequested = PathFrameBytes
		x.snapshot.SocketBufferNote = runtime.GOOS + ": SetReadBuffer/SetWriteBuffer request only; effective kernel size not sampled"
		if bufferErr != nil {
			x.snapshot.Error = "socket buffer request: " + bufferErr.Error()
		}
		t.exchanges[t.next] = x
		t.workers++
		t.wg.Add(1)
		x.eventLocked(PathAccepted)
		t.mu.Unlock()
		go x.serve()
	}
}
func (x *PathExchange) waitGate(gate <-chan struct{}) error {
	select {
	case <-x.target.ctx.Done():
		return x.target.ctx.Err()
	case <-gate:
		return nil
	}
}
func (x *PathExchange) ReleaseRead()     { x.readOnce.Do(func() { close(x.readGate) }) }
func (x *PathExchange) ReleaseResponse() { x.responseOnce.Do(func() { close(x.responseGate) }) }
func (x *PathExchange) fail(err error) {
	if err == nil {
		return
	}
	t := x.target
	t.mu.Lock()
	if x.snapshot.Error == "" {
		x.snapshot.Error = err.Error()
	}
	t.signalLocked()
	t.mu.Unlock()
}
func (x *PathExchange) serve() {
	t := x.target
	defer t.wg.Done()
	defer func() {
		_ = x.conn.Close()
		t.mu.Lock()
		x.snapshot.Closed = true
		t.workers--
		x.eventLocked(PathClosed)
		t.mu.Unlock()
	}()
	if x.snapshot.Error != "" {
		return
	}
	// Read only the envelope, never prefetching a paused body.
	var header [132]byte
	n := 0
	for {
		if n == len(header) {
			x.fail(errors.New("path request header too long"))
			return
		}
		read, err := io.ReadFull(x.conn, header[n:n+1])
		t.mu.Lock()
		x.snapshot.WireReceivedBytes += int64(read)
		t.signalLocked()
		t.mu.Unlock()
		if err != nil {
			x.fail(err)
			return
		}
		if header[n] == '\n' {
			break
		}
		n++
	}
	id := strings.TrimPrefix(string(header[:n]), "G3 ")
	if n < 3 || string(header[:3]) != "G3 " || !validPathID(id) {
		x.fail(errors.New("invalid path request ID"))
		return
	}
	t.mu.Lock()
	p, ok := t.plans[id]
	x.snapshot.RequestID = id
	x.plan = p
	t.signalLocked()
	t.mu.Unlock()
	if !ok {
		x.fail(fmt.Errorf("unregistered path request ID %q", id))
		return
	}
	if !p.HoldRead {
		x.ReleaseRead()
	}
	if !p.HoldResponse {
		x.ReleaseResponse()
	}
	if p.ResponseFirst {
		// FIN in this direction must not prevent reading the peer's remaining
		// request. The reader and writer each own at most one F-sized block.
		written := make(chan error, 1)
		go func() { written <- x.writeResponse() }()
		err := x.readRequest()
		if err != nil {
			_ = x.conn.Close()
			x.ReleaseResponse()
		}
		werr := <-written
		x.fail(err)
		x.fail(werr)
		return
	}
	if err := x.readRequest(); err != nil {
		x.fail(err)
		return
	}
	x.fail(x.writeResponse())
}
func (x *PathExchange) readRequest() error {
	if err := x.waitGate(x.readGate); err != nil {
		return err
	}
	p, t := x.plan, x.target
	buf := make([]byte, PathFrameBytes)
	h := sha256.New()
	var off int64
	// Tails are bounded by the fixed, pre-registered marker lengths. They
	// permit split-read detection without storing the request stream.
	tails := make([]string, len(p.Observe))
	for {
		n, err := x.conn.Read(buf)
		if n > 0 {
			t.mu.Lock()
			x.snapshot.WireReceivedBytes += int64(n)
			x.snapshot.ReceivedBytes += int64(n)
			t.signalLocked()
			t.mu.Unlock()
			if p.RequestBytes >= 0 && off+int64(n) > p.RequestBytes {
				return errors.New("request exceeds declared bytes")
			}
			if p.Pattern {
				for i, b := range buf[:n] {
					if b != Pattern(off+int64(i)) {
						return fmt.Errorf("request pattern differs at %d", off+int64(i))
					}
				}
			}
			_, _ = h.Write(buf[:n])
			off += int64(n)
			t.mu.Lock()
			for i, m := range p.Observe {
				joined := tails[i] + string(buf[:n])
				x.snapshot.Markers[m] += strings.Count(joined, m)
				keep := min(len(m)-1, len(joined))
				tails[i] = joined[len(joined)-keep:]
			}
			t.signalLocked()
			t.mu.Unlock()
		}
		if errors.Is(err, io.EOF) {
			t.mu.Lock()
			x.snapshot.RequestFIN = true
			x.snapshot.SHA256 = hex.EncodeToString(h.Sum(nil))
			x.eventLocked(PathRequestFIN)
			t.mu.Unlock()
			if p.RequestBytes >= 0 && off != p.RequestBytes {
				return fmt.Errorf("request bytes %d want %d", off, p.RequestBytes)
			}
			t.mu.Lock()
			t.effects[x.snapshot.RequestID]++
			x.eventLocked(PathEffectCommitted)
			t.mu.Unlock()
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func (x *PathExchange) writeResponse() (err error) {
	if err = x.waitGate(x.responseGate); err != nil {
		return err
	}
	t := x.target
	enter := func() {
		t.mu.Lock()
		if !x.snapshot.WriterEntered {
			x.snapshot.WriterEntered = true
			x.eventLocked(PathWriterEntered)
		}
		t.mu.Unlock()
	}
	defer func() {
		t.mu.Lock()
		x.snapshot.WriterReturned = true
		x.eventLocked(PathWriterReturned)
		t.mu.Unlock()
	}()
	buf := make([]byte, PathFrameBytes)
	var off int64
	for off < x.plan.ResponseBytes {
		n := int(min(int64(len(buf)), x.plan.ResponseBytes-off))
		for i := range n {
			buf[i] = Pattern(off + int64(i))
		}
		for pos := 0; pos < n; {
			enter()
			w, werr := x.conn.Write(buf[pos:n])
			pos += w
			off += int64(w)
			t.mu.Lock()
			x.snapshot.ResponseBytes = off
			t.signalLocked()
			t.mu.Unlock()
			if werr != nil {
				return werr
			}
			if w == 0 {
				return io.ErrShortWrite
			}
		}
	}
	enter()
	if err = x.conn.CloseWrite(); err != nil {
		return err
	}
	t.mu.Lock()
	x.snapshot.ResponseFIN = true
	x.eventLocked(PathResponseFIN)
	t.mu.Unlock()
	return nil
}
func clonePathSnapshot(s PathExchangeSnapshot) PathExchangeSnapshot {
	m := make(map[string]int, len(s.Markers))
	for k, v := range s.Markers {
		m[k] = v
	}
	s.Markers = m
	e := make(map[PathEvent]int64, len(s.Events))
	for k, v := range s.Events {
		e[k] = v
	}
	s.Events = e
	return s
}
func (x *PathExchange) Snapshot() PathExchangeSnapshot {
	t := x.target
	t.mu.Lock()
	defer t.mu.Unlock()
	return clonePathSnapshot(x.snapshot)
}
func (x *PathExchange) Wait(ctx context.Context, event PathEvent) error {
	switch event {
	case PathAccepted, PathRequestFIN, PathEffectCommitted, PathWriterEntered, PathWriterReturned, PathResponseFIN, PathClosed:
	default:
		return errors.New("unknown path event")
	}
	t := x.target
	for {
		t.mu.Lock()
		_, ok := x.snapshot.Events[event]
		closed := x.snapshot.Closed
		why := x.snapshot.Error
		wait := t.changed
		t.mu.Unlock()
		if ok {
			return nil
		}
		if closed {
			return fmt.Errorf("path closed before %s: %s", event, why)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
}
func (t *PathTarget) WaitRequest(ctx context.Context, id string, occurrence int) (*PathExchange, error) {
	if occurrence < 1 {
		return nil, errors.New("request occurrence must be positive")
	}
	for {
		t.mu.Lock()
		var matches []*PathExchange
		for _, x := range t.exchanges {
			if x.snapshot.RequestID == id {
				matches = append(matches, x)
			}
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].snapshot.ID < matches[j].snapshot.ID })
		wait, closed := t.changed, t.closed
		t.mu.Unlock()
		if len(matches) >= occurrence {
			return matches[occurrence-1], nil
		}
		if closed {
			return nil, net.ErrClosed
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
	}
}
func (t *PathTarget) Snapshot() PathSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := PathSnapshot{Effects: map[string]int{}, Workers: t.workers, Closed: t.closed}
	for k, v := range t.effects {
		s.Effects[k] = v
	}
	for _, x := range t.exchanges {
		s.Exchanges = append(s.Exchanges, clonePathSnapshot(x.snapshot))
	}
	sort.Slice(s.Exchanges, func(i, j int) bool { return s.Exchanges[i].ID < s.Exchanges[j].ID })
	return s
}
func (t *PathTarget) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.cancel()
		cs := make([]*PathExchange, 0, len(t.exchanges))
		for _, x := range t.exchanges {
			cs = append(cs, x)
		}
		t.signalLocked()
		t.mu.Unlock()
		_ = t.ln.Close()
		for _, x := range cs {
			_ = x.conn.Close()
			x.ReleaseRead()
			x.ReleaseResponse()
		}
		t.wg.Wait()
	})
	return nil
}
func WritePathPattern(w io.Writer, n int64) error {
	if n < 0 {
		return errors.New("negative pattern length")
	}
	buf := make([]byte, PathFrameBytes)
	for off := int64(0); off < n; {
		size := int(min(int64(len(buf)), n-off))
		for i := range size {
			buf[i] = Pattern(off + int64(i))
		}
		written, err := w.Write(buf[:size])
		off += int64(written)
		if err != nil {
			return err
		}
		if written != size {
			return io.ErrShortWrite
		}
	}
	return nil
}

// CheckPathPattern checks exactly n bytes. The caller separately checks EOF
// so the reverse direction can remain open in a target-first FIN exchange.
func CheckPathPattern(r io.Reader, n int64) error {
	if n < 0 {
		return errors.New("negative pattern length")
	}
	buf := make([]byte, PathFrameBytes)
	for off := int64(0); off < n; {
		size := int(min(int64(len(buf)), n-off))
		if _, err := io.ReadFull(r, buf[:size]); err != nil {
			return err
		}
		for i, b := range buf[:size] {
			if b != Pattern(off+int64(i)) {
				return fmt.Errorf("response pattern differs at %d", off+int64(i))
			}
		}
		off += int64(size)
	}
	return nil
}
