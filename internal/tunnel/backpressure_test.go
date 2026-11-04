package tunnel

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Both sinks stop inside their real interface Write. Queued source messages
// remain outside the bridge; consuming them early would violate the bound.
type boundedBridgeConn struct {
	in                    chan []byte
	gate, entered, closed chan struct{}
	once, enter           sync.Once
	reads                 atomic.Int64
	written               atomic.Int64
}

func (c *boundedBridgeConn) Read(p []byte) (int, error) {
	c.reads.Add(1)
	select {
	case b := <-c.in:
		if b == nil {
			return 0, io.EOF
		}
		return copy(p, b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}
func (c *boundedBridgeConn) Write(p []byte) (int, error) {
	c.enter.Do(func() { close(c.entered) })
	select {
	case <-c.gate:
		c.written.Add(int64(len(p)))
		return len(p), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}
func (c *boundedBridgeConn) Close() error                     { c.once.Do(func() { close(c.closed) }); return nil }
func (c *boundedBridgeConn) CloseWrite() error                { return nil }
func (c *boundedBridgeConn) SetLinger(int) error              { return nil }
func (c *boundedBridgeConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *boundedBridgeConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *boundedBridgeConn) SetDeadline(time.Time) error      { return nil }
func (c *boundedBridgeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *boundedBridgeConn) SetWriteDeadline(time.Time) error { return nil }

type boundedBridgeLeg struct {
	*fakeLeg
	gate, entered  chan struct{}
	enter          sync.Once
	reads, written atomic.Int64
}

func (l *boundedBridgeLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	l.reads.Add(1)
	return l.fakeLeg.Read(ctx)
}
func (l *boundedBridgeLeg) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	f, err := protocol.DecodeFrame(p, 32<<10)
	if err != nil {
		return err
	}
	if f.Type == protocol.FrameData {
		l.enter.Do(func() { close(l.entered) })
		select {
		case <-l.gate:
			l.written.Add(int64(len(f.Data)))
		case <-ctx.Done():
			return ctx.Err()
		case <-l.closed:
			return net.ErrClosed
		}
	}
	return l.fakeLeg.Write(ctx, typ, p)
}
func waitBufferLatch(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("buffer latch not reached")
	}
}

func TestBridgeBackpressureBounded(t *testing.T) {
	const f = 32 << 10
	c := &boundedBridgeConn{in: make(chan []byte, 3), gate: make(chan struct{}), entered: make(chan struct{}), closed: make(chan struct{})}
	l := &boundedBridgeLeg{fakeLeg: newLeg(), gate: make(chan struct{}), entered: make(chan struct{})}
	t.Cleanup(func() { _ = c.Close(); _ = l.CloseNow() })
	b := New(context.Background(), l, c, f, grace, nil)
	l.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	block := make([]byte, f)
	for i := range block {
		block[i] = byte(i)
	}
	c.in <- block
	c.in <- block
	c.in <- nil
	l.push(protocol.DataFrame(block))
	l.push(protocol.DataFrame(block))
	l.push(protocol.FinFrame())
	result := run(b)
	waitBufferLatch(t, c.entered)
	waitBufferLatch(t, l.entered)
	if got := b.Buffered(); got != 2*f {
		t.Fatalf("both sinks blocked: buffered=%d want exactly 2F", got)
	}
	if c.reads.Load() != 1 || l.reads.Load() != 2 || len(c.in) != 2 || len(l.in) != 2 {
		t.Fatalf("prefetched past blocked sink: conn reads=%d leg reads=%d queued=%d/%d", c.reads.Load(), l.reads.Load(), len(c.in), len(l.in))
	}
	if c.written.Load() != 0 || l.written.Load() != 0 {
		t.Fatal("a blocked sink already forwarded payload")
	}
	close(c.gate)
	close(l.gate)
	// OIS closes only after both FINs. Supply EOF after that handshake,
	// rather than inventing a peer that disappears before seeing our FIN.
	waitBufferLatch(t, b.Done())
	l.in <- msg{err: io.EOF}
	r := wait(t, result)
	if r.Code != protocol.CodeCompleted || r.Up != 2*f || r.Down != 2*f || c.written.Load() != 2*f || l.written.Load() != 2*f || b.Buffered() != 0 {
		t.Fatalf("release lost data or retained buffer: %+v held=%d", r, b.Buffered())
	}
}
