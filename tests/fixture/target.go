// Package fixture provides the deterministic TCP targets of B4 §4: they
// answer with their identity, record every connection's bytes and how it
// ended (clean FIN or reset), and can answer slowly, stream a large body
// or reset mid-response.
package fixture

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// Mode selects a target's behaviour.
type Mode int

// Target modes.
const (
	// Identify reads until EOF, waits Delay, then answers "<name>:<request>".
	Identify Mode = iota
	// Echo writes every chunk back and half-closes after EOF.
	Echo
	// Hold records what arrives and never answers.
	Hold
	// ResetMidway reads until EOF, writes half an answer, then resets.
	ResetMidway
	// Stream reads until EOF, then sends Size pattern bytes.
	Stream
)

// Record describes one accepted connection.
type Record struct {
	Data  []byte
	EOF   bool // a clean FIN arrived from the OIC
	Reset bool // a reset was observed
	Done  bool // the target finished with the connection
}

// Target is one fixture listener.
type Target struct {
	Name  string
	Mode  Mode
	Delay time.Duration
	Size  int64

	ln     net.Listener
	closed chan struct{}
	mu     sync.Mutex
	conns  []*Record
}

// New starts a target on loopback; it stops with the test.
func New(t testing.TB, name string, mode Mode) *Target {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tg := &Target{Name: name, Mode: mode, ln: ln, closed: make(chan struct{})}
	go tg.accept()
	t.Cleanup(func() {
		close(tg.closed)
		_ = ln.Close()
	})
	return tg
}

// Addr is the target's host:port.
func (tg *Target) Addr() string { return tg.ln.Addr().String() }

func (tg *Target) accept() {
	for {
		c, err := tg.ln.Accept()
		if err != nil {
			return
		}
		r := &Record{}
		tg.update(func() { tg.conns = append(tg.conns, r) })
		go tg.serve(c.(*net.TCPConn), r)
	}
}

func (tg *Target) update(fn func()) {
	tg.mu.Lock()
	fn()
	tg.mu.Unlock()
}

// readAll records bytes until EOF or an error.
func (tg *Target) readAll(c *net.TCPConn, r *Record, echo bool) {
	buf := make([]byte, 32<<10)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			tg.update(func() { r.Data = append(r.Data, chunk...) })
			if echo {
				if _, werr := c.Write(chunk); werr != nil {
					tg.update(func() { r.Reset = true })
					return
				}
			}
		}
		if errors.Is(err, io.EOF) {
			tg.update(func() { r.EOF = true })
			return
		}
		if err != nil {
			tg.update(func() { r.Reset = true })
			return
		}
	}
}

func (tg *Target) serve(c *net.TCPConn, r *Record) {
	defer func() {
		_ = c.Close()
		tg.update(func() { r.Done = true })
	}()
	tg.readAll(c, r, tg.Mode == Echo)
	tg.mu.Lock()
	eof, req := r.EOF, string(r.Data)
	tg.mu.Unlock()
	if !eof {
		return
	}
	time.Sleep(tg.Delay)
	var err error
	switch tg.Mode {
	case Identify:
		_, err = c.Write([]byte(tg.Name + ":" + req))
	case Echo:
		err = c.CloseWrite()
	case Hold:
		<-tg.closed
	case ResetMidway:
		_, err = c.Write([]byte(tg.Name + ":part"))
		time.Sleep(50 * time.Millisecond)
		_ = c.SetLinger(0)
	case Stream:
		err = writePattern(c, tg.Size)
	}
	if err != nil {
		tg.update(func() { r.Reset = true })
		return
	}
	// Watch for a reset after the answer (FIN-then-RST, TV-B1-07).
	_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, rerr := c.Read(make([]byte, 1)); rerr != nil && !errors.Is(rerr, io.EOF) && !isTimeout(rerr) {
		tg.update(func() { r.Reset = true })
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Pattern returns byte i of the Stream body.
func Pattern(i int64) byte { return byte(i*31 + i/251) }

func writePattern(c net.Conn, size int64) error {
	buf := make([]byte, 64<<10)
	for off := int64(0); off < size; {
		n := min(int64(len(buf)), size-off)
		for i := range n {
			buf[i] = Pattern(off + i)
		}
		if _, err := c.Write(buf[:n]); err != nil {
			return err
		}
		off += n
	}
	return nil
}

// Conns returns copies of the connection records.
func (tg *Target) Conns() []Record {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	out := make([]Record, len(tg.conns))
	for i, r := range tg.conns {
		out[i] = *r
		out[i].Data = append([]byte(nil), r.Data...)
	}
	return out
}

// Wait blocks until cond holds for the records or timeout passes; it
// reports whether cond held.
func (tg *Target) Wait(timeout time.Duration, cond func([]Record) bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond(tg.Conns()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}
