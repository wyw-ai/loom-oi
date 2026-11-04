package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"
)

const g3FrameBytes = 32 << 10
const (
	g3ClientToServer = "client_to_server"
	g3ServerToClient = "server_to_client"
)

type g3DirectionSnapshot struct {
	ReadBytes, ForwardedBytes       int64
	BufferedBytes, MaxBufferedBytes int
	Paused, EOFObserved             bool
}
type g3ProxyConnSnapshot struct {
	ID                                                   uint64
	Role, AccessID, ChannelID                            string
	Pending, ServerConnected, ClientClosed, ServerClosed bool
	ResetAtNS                                            int64
	ClientToServer, ServerToClient                       g3DirectionSnapshot
}
type g3ProxySnapshot struct {
	Conns   []g3ProxyConnSnapshot
	Workers int
	Closed  bool
}
type g3PauseEvent struct {
	ID        uint64
	Direction string
	AppliedNS int64
	State     g3DirectionSnapshot
}
type g3ProxyDirection struct {
	snapshot  g3DirectionSnapshot
	requested bool
	appliedNS int64
}
type g3ProxyConn struct {
	snapshot       g3ProxyConnSnapshot
	client, server *net.TCPConn
	directions     [2]g3ProxyDirection
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
}

// g3FaultProxy forwards opaque TLS. All bytes belong to exactly one pump;
// neither observing a pause nor closing the other direction reads ahead.
type g3FaultProxy struct {
	ln           *net.TCPListener
	target       string
	now          func() int64
	mu           sync.Mutex
	conns        map[uint64]*g3ProxyConn
	next         uint64
	hold, closed bool
	workers      int
	changed      chan struct{}
	wg           sync.WaitGroup
	closeOnce    sync.Once
}

func newG3FaultProxy(target string, now func() int64) (*g3FaultProxy, error) {
	if now == nil {
		return nil, errors.New("G3 proxy requires the controller clock")
	}
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	p := &g3FaultProxy{ln: ln, target: target, now: now, conns: make(map[uint64]*g3ProxyConn), changed: make(chan struct{}), workers: 1}
	p.wg.Add(1)
	go p.accept()
	return p, nil
}
func (p *g3FaultProxy) Addr() string  { return p.ln.Addr().String() }
func (p *g3FaultProxy) signalLocked() { close(p.changed); p.changed = make(chan struct{}) }
func (p *g3FaultProxy) workerDone() {
	p.mu.Lock()
	p.workers--
	p.signalLocked()
	p.mu.Unlock()
	p.wg.Done()
}
func (p *g3FaultProxy) accept() {
	defer p.workerDone()
	for {
		c, err := p.ln.AcceptTCP()
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = c.Close()
			return
		}
		p.next++
		ctx, cancel := context.WithCancel(context.Background())
		x := &g3ProxyConn{client: c, ctx: ctx, cancel: cancel, done: make(chan struct{}), snapshot: g3ProxyConnSnapshot{ID: p.next, Pending: p.hold, ServerClosed: true}}
		p.conns[p.next] = x
		p.workers++
		p.wg.Add(1)
		p.signalLocked()
		p.mu.Unlock()
		go p.serve(x)
	}
}
func (p *g3FaultProxy) serve(c *g3ProxyConn) {
	defer p.workerDone()
	defer close(c.done)
	defer p.closeConn(c, false)
	for {
		p.mu.Lock()
		wait := p.changed
		stop, held := c.ctx.Err() != nil, c.snapshot.Pending
		p.mu.Unlock()
		if stop {
			return
		}
		if !held {
			break
		}
		select {
		case <-wait:
		case <-c.ctx.Done():
			return
		}
	}
	x, err := (&net.Dialer{}).DialContext(c.ctx, "tcp", p.target)
	if err != nil {
		return
	}
	s := x.(*net.TCPConn)
	p.mu.Lock()
	if c.ctx.Err() != nil {
		p.mu.Unlock()
		_ = s.Close()
		return
	}
	c.server = s
	c.snapshot.ServerConnected = true
	c.snapshot.ServerClosed = false
	p.workers += 2
	p.wg.Add(2)
	p.signalLocked()
	p.mu.Unlock()
	var pumps sync.WaitGroup
	pumps.Add(2)
	for i := range 2 {
		go func(i int) { defer pumps.Done(); defer p.workerDone(); p.pump(c, i) }(i)
	}
	pumps.Wait()
}
func (p *g3FaultProxy) gate(c *g3ProxyConn, i int) bool {
	for {
		p.mu.Lock()
		d := &c.directions[i]
		if c.ctx.Err() != nil {
			p.mu.Unlock()
			return false
		}
		if !d.requested {
			p.mu.Unlock()
			return true
		}
		if !d.snapshot.Paused {
			d.snapshot.Paused = true
			d.appliedNS = p.now()
			p.signalLocked()
		}
		wait := p.changed
		p.mu.Unlock()
		select {
		case <-wait:
		case <-c.ctx.Done():
			return false
		}
	}
}
func (p *g3FaultProxy) pump(c *g3ProxyConn, i int) {
	src, dst := c.client, c.server
	if i == 1 {
		src, dst = c.server, c.client
	}
	buf := make([]byte, g3FrameBytes)
	for p.gate(c, i) {
		n, err := src.Read(buf)
		p.mu.Lock()
		d := &c.directions[i]
		d.snapshot.ReadBytes += int64(n)
		d.snapshot.BufferedBytes = n
		d.snapshot.MaxBufferedBytes = max(d.snapshot.MaxBufferedBytes, n)
		p.signalLocked()
		p.mu.Unlock()
		// A pause can interrupt a blocked read. Holding its returned bytes
		// before any write makes the acknowledgement a real directional gate.
		if !p.gate(c, i) {
			return
		}
		for off := 0; off < n; {
			w, werr := dst.Write(buf[off:n])
			off += w
			p.mu.Lock()
			d.snapshot.ForwardedBytes += int64(w)
			d.snapshot.BufferedBytes = n - off
			p.signalLocked()
			p.mu.Unlock()
			if werr != nil {
				p.closeConn(c, true)
				return
			}
			if w == 0 {
				p.closeConn(c, true)
				return
			}
		}
		if errors.Is(err, io.EOF) {
			p.mu.Lock()
			d.snapshot.EOFObserved = true
			p.signalLocked()
			p.mu.Unlock()
			_ = dst.CloseWrite()
			return
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			} // only our pause sets a read deadline
			p.closeConn(c, true)
			return
		}
	}
}
func (p *g3FaultProxy) closeConn(c *g3ProxyConn, reset bool) {
	p.mu.Lock()
	c.cancel()
	c.snapshot.Pending = false
	client, server := c.client, c.server
	p.signalLocked()
	p.mu.Unlock()
	for _, s := range []*net.TCPConn{client, server} {
		if s != nil {
			if reset {
				_ = s.SetLinger(0)
			}
			_ = s.Close()
		}
	}
	p.mu.Lock()
	c.snapshot.ClientClosed, c.snapshot.ServerClosed = true, true
	p.signalLocked()
	p.mu.Unlock()
}
func (p *g3FaultProxy) Mark() uint64 { p.mu.Lock(); defer p.mu.Unlock(); return p.next }
func (p *g3FaultProxy) WaitAccepted(ctx context.Context, after uint64, n int) ([]uint64, error) {
	if n < 1 {
		return nil, errors.New("accepted count must be positive")
	}
	for {
		p.mu.Lock()
		var ids []uint64
		for id := range p.conns {
			if id > after {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		wait, closed := p.changed, p.closed
		p.mu.Unlock()
		if len(ids) >= n {
			return ids, nil
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
func (p *g3FaultProxy) BindRole(id uint64, role, accessID, channelID string) error {
	if role != "control" && role != "provider" && role != "consumer" && role != "api" {
		return errors.New("invalid G3 role")
	}
	if ((role == "provider" || role == "consumer") != (channelID != "")) || ((role == "control" || role == "provider") && accessID == "") {
		return errors.New("invalid role identity")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.conns[id]
	if c == nil {
		return errors.New("unknown G3 connection")
	}
	s := &c.snapshot
	if s.Role != "" && (s.Role != role || s.AccessID != accessID || s.ChannelID != channelID) {
		return errors.New("G3 connection cannot be rebound")
	}
	s.Role, s.AccessID, s.ChannelID = role, accessID, channelID
	p.signalLocked()
	return nil
}
func (p *g3FaultProxy) HoldNew(hold bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hold = hold
	if !hold {
		for _, c := range p.conns {
			c.snapshot.Pending = false
		}
	}
	p.signalLocked()
}
func (p *g3FaultProxy) ReleaseNew() { p.HoldNew(false) }
func g3DirectionIndex(dir string) (int, error) {
	switch dir {
	case g3ClientToServer:
		return 0, nil
	case g3ServerToClient:
		return 1, nil
	}
	return 0, fmt.Errorf("unknown G3 direction %q", dir)
}
func (p *g3FaultProxy) PauseRead(ctx context.Context, id uint64, dir string) (g3PauseEvent, error) {
	i, err := g3DirectionIndex(dir)
	if err != nil {
		return g3PauseEvent{}, err
	}
	p.mu.Lock()
	c := p.conns[id]
	if c == nil || !c.snapshot.ServerConnected || c.ctx.Err() != nil {
		p.mu.Unlock()
		return g3PauseEvent{}, errors.New("connection not forwarding")
	}
	d := &c.directions[i]
	d.requested = true
	src := c.client
	if i == 1 {
		src = c.server
	}
	_ = src.SetReadDeadline(time.Now())
	p.signalLocked()
	p.mu.Unlock()
	for {
		p.mu.Lock()
		state := d.snapshot
		wait := p.changed
		closed := c.ctx.Err() != nil
		at := d.appliedNS
		p.mu.Unlock()
		if state.Paused {
			return g3PauseEvent{ID: id, Direction: dir, AppliedNS: at, State: state}, nil
		}
		if closed {
			return g3PauseEvent{}, net.ErrClosed
		}
		select {
		case <-ctx.Done():
			return g3PauseEvent{}, ctx.Err()
		case <-wait:
		}
	}
}
func (p *g3FaultProxy) ResumeRead(id uint64, dir string) error {
	i, err := g3DirectionIndex(dir)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.conns[id]
	if c == nil || !c.snapshot.ServerConnected {
		return errors.New("connection not forwarding")
	}
	src := c.client
	if i == 1 {
		src = c.server
	}
	if err := src.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	c.directions[i].requested = false
	c.directions[i].snapshot.Paused = false
	p.signalLocked()
	return nil
}
func (p *g3FaultProxy) Reset(id uint64) (int64, error) {
	p.mu.Lock()
	c := p.conns[id]
	if c == nil || c.ctx.Err() != nil {
		p.mu.Unlock()
		return 0, errors.New("unknown or closed G3 connection")
	}
	for _, s := range []*net.TCPConn{c.client, c.server} {
		if s != nil {
			if err := s.SetLinger(0); err != nil {
				p.mu.Unlock()
				return 0, err
			}
		}
	}
	at := p.now()
	c.snapshot.ResetAtNS = at
	p.mu.Unlock()
	// No WaitClosed/join here: the caller's cleanup deadline starts at at.
	p.closeConn(c, true)
	return at, nil
}
func (p *g3FaultProxy) WaitClosed(ctx context.Context, id uint64) error {
	p.mu.Lock()
	c := p.conns[id]
	p.mu.Unlock()
	if c == nil {
		return errors.New("unknown G3 connection")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return nil
	}
}
func (p *g3FaultProxy) Snapshot() g3ProxySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := g3ProxySnapshot{Workers: p.workers, Closed: p.closed}
	for _, c := range p.conns {
		x := c.snapshot
		x.ClientToServer = c.directions[0].snapshot
		x.ServerToClient = c.directions[1].snapshot
		s.Conns = append(s.Conns, x)
	}
	sort.Slice(s.Conns, func(i, j int) bool { return s.Conns[i].ID < s.Conns[j].ID })
	return s
}
func (p *g3FaultProxy) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		cs := make([]*g3ProxyConn, 0, len(p.conns))
		for _, c := range p.conns {
			cs = append(cs, c)
		}
		p.signalLocked()
		p.mu.Unlock()
		_ = p.ln.Close()
		for _, c := range cs {
			p.closeConn(c, false)
		}
		p.wg.Wait()
	})
	return nil
}
