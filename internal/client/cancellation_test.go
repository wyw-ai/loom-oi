package client

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type cancelMessage struct {
	frame []byte
	err   error
}
type cancelLeg struct {
	in                             chan cancelMessage
	out                            chan []byte
	closed                         chan struct{}
	writeLock                      chan struct{}
	closeGate                      <-chan struct{}
	readEntered, writeEntered      chan struct{}
	blockWrite                     bool
	closeOnce, readOnce, writeOnce sync.Once
	reads                          atomic.Int64
	closeCalls                     atomic.Int64
	active                         atomic.Int64
	mu                             sync.Mutex
	closedAt                       time.Time
}

func newCancelLeg() *cancelLeg {
	return &cancelLeg{in: make(chan cancelMessage, 8), out: make(chan []byte, 8), closed: make(chan struct{}), readEntered: make(chan struct{}), writeEntered: make(chan struct{}), writeLock: make(chan struct{}, 1)}
}
func (l *cancelLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	l.reads.Add(1)
	l.active.Add(1)
	defer l.active.Add(-1)
	l.readOnce.Do(func() { close(l.readEntered) })
	select {
	case m := <-l.in:
		return websocket.MessageBinary, m.frame, m.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-l.closed:
		return 0, nil, net.ErrClosed
	}
}
func (l *cancelLeg) Write(ctx context.Context, _ websocket.MessageType, p []byte) error {
	l.active.Add(1)
	defer l.active.Add(-1)
	// Model the leg's single writer with a channel so synctest can advance
	// its clock while ABORT waits behind a blocked DATA write.
	select {
	case l.writeLock <- struct{}{}:
		defer func() { <-l.writeLock }()
	case <-ctx.Done():
		return ctx.Err()
	case <-l.closed:
		return net.ErrClosed
	}
	l.writeOnce.Do(func() { close(l.writeEntered) })
	if l.blockWrite {
		select {
		case <-l.closed:
			return net.ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case <-l.closed:
		return net.ErrClosed
	default:
	}
	l.out <- append([]byte(nil), p...)
	return nil
}
func (l *cancelLeg) Close(websocket.StatusCode, string) error { return l.CloseNow() }
func (l *cancelLeg) CloseNow() error {
	l.closeCalls.Add(1)
	first := false
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closedAt = time.Now()
		l.mu.Unlock()
		close(l.closed)
		first = true
	})
	if first && l.closeGate != nil {
		<-l.closeGate
	}
	return nil
}
func (l *cancelLeg) push(p []byte) { l.in <- cancelMessage{frame: p} }

type cancelConn struct {
	net.Conn
	in                                              chan []byte
	closed, readEntered, writeEntered, closeEntered chan struct{}
	closeGate                                       <-chan struct{}
	writeGate                                       <-chan struct{}
	writeErr                                        error
	blockWrite                                      bool
	closeOnce, readOnce, writeOnce                  sync.Once
	reads, active                                   atomic.Int64
	reset                                           atomic.Bool
}

func newCancelConn() *cancelConn {
	return &cancelConn{in: make(chan []byte, 4), closed: make(chan struct{}), readEntered: make(chan struct{}), writeEntered: make(chan struct{}), closeEntered: make(chan struct{})}
}

// The bridge's bounded diagnostics may inspect both addresses; this fake
// implements the complete connection contract without a nil embedded Conn.
func (c *cancelConn) LocalAddr() net.Addr  { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }
func (c *cancelConn) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2} }
func (c *cancelConn) Read(p []byte) (int, error) {
	c.reads.Add(1)
	c.active.Add(1)
	defer c.active.Add(-1)
	c.readOnce.Do(func() { close(c.readEntered) })
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
func (c *cancelConn) Write(p []byte) (int, error) {
	c.active.Add(1)
	defer c.active.Add(-1)
	c.writeOnce.Do(func() { close(c.writeEntered) })
	if c.writeGate != nil {
		<-c.writeGate
	}
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.blockWrite {
		<-c.closed
		return 0, net.ErrClosed
	}
	return len(p), nil
}
func (c *cancelConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeEntered)
		if c.closeGate != nil {
			<-c.closeGate
		}
		close(c.closed)
	})
	return nil
}
func (c *cancelConn) CloseWrite() error     { return nil }
func (c *cancelConn) SetLinger(n int) error { c.reset.Store(n == 0); return nil }

type cancelResult struct {
	result tunnel.Result
	err    error
}

func startCancelBridge(ctx context.Context, l *cancelLeg, c *cancelConn, grace time.Duration) <-chan cancelResult {
	done := make(chan cancelResult, 1)
	go func() { r, e := forwardBridge(ctx, l, c, 32<<10, grace); done <- cancelResult{r, e} }()
	return done
}
func assertCancelClosed(t *testing.T, l *cancelLeg, c *cancelConn) {
	t.Helper()
	synctest.Wait()
	select {
	case <-l.closed:
	default:
		t.Fatal("leg was not closed")
	}
	select {
	case <-c.closed:
	default:
		t.Fatal("local socket was not closed")
	}
	if l.active.Load() != 0 || c.active.Load() != 0 {
		t.Fatalf("I/O still active: leg=%d local=%d", l.active.Load(), c.active.Load())
	}
}

func TestForwardCancellation(t *testing.T) {
	const grace = 100 * time.Millisecond
	for _, name := range []string{"already_cancelled_before_registration", "before_READY", "active_read", "blocked_leg_write", "blocked_local_write", "Completed_waiting_for_remote_close", "remote_ABORT_first", "cancel_first", "cancel_before_FIN"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				l, c := newCancelLeg(), newCancelConn()
				if name == "blocked_leg_write" {
					l.blockWrite = true
					c.in <- []byte("queued")
				}
				if name == "blocked_local_write" {
					c.blockWrite = true
				}
				if name == "already_cancelled_before_registration" {
					cancel()
					c.in <- []byte("must not read")
				}
				var releaseClose chan struct{}
				if name == "remote_ABORT_first" {
					releaseClose = make(chan struct{})
					c.closeGate = releaseClose
				}
				if name != "before_READY" {
					l.push(protocol.ReadyFrame())
				}
				if name == "blocked_local_write" {
					l.push(protocol.DataFrame([]byte("blocked")))
				}
				if name == "Completed_waiting_for_remote_close" {
					l.push(protocol.FinFrame())
					c.in <- nil
				}
				done := startCancelBridge(ctx, l, c, grace)
				if name != "already_cancelled_before_registration" {
					switch name {
					case "before_READY":
						<-l.readEntered
					case "blocked_leg_write":
						<-l.writeEntered
					case "blocked_local_write":
						<-c.writeEntered
					case "Completed_waiting_for_remote_close":
						<-c.closed
						synctest.Wait()
					default:
						<-c.readEntered
					}
				}
				if name == "remote_ABORT_first" {
					l.push(protocol.AbortFrame(protocol.CodeRevoked, ""))
					<-c.closeEntered
				}
				start := time.Now()
				cancel()
				if releaseClose != nil {
					close(releaseClose)
				} else {
					synctest.Wait()
				}
				if name == "cancel_first" {
					<-c.closed
					l.push(protocol.AbortFrame(protocol.CodeRevoked, ""))
				}
				if name == "cancel_before_FIN" {
					<-c.closed
					c.in <- nil
					l.push(protocol.FinFrame())
				}
				r := <-done
				want, remote := protocol.CodeClosedByClient, false
				if name == "Completed_waiting_for_remote_close" {
					want = protocol.CodeCompleted
				}
				if name == "remote_ABORT_first" {
					want, remote = protocol.CodeRevoked, true
				}
				if r.result.Code != want || r.result.Remote != remote {
					t.Fatalf("terminal result changed: %+v %v", r.result, r.err)
				}
				if time.Since(start) > grace {
					t.Fatalf("cancel elapsed %v exceeds T_close %v", time.Since(start), grace)
				}
				if name == "before_READY" || name == "already_cancelled_before_registration" {
					if protocol.CodeOf(r.err) != protocol.CodeClosedByClient || c.reads.Load() != 0 {
						t.Fatalf("payload/READY consumed before cancellation: reads=%d result=%+v", c.reads.Load(), r)
					}
				} else if r.err != nil {
					t.Fatal(r.err)
				}
				if name == "already_cancelled_before_registration" && len(c.in) != 1 {
					t.Fatal("already-cancelled forwarding consumed local payload")
				}
				if want != protocol.CodeCompleted && !c.reset.Load() {
					t.Fatal("interrupted local connection was not reset")
				}
				assertCancelClosed(t, l, c)
			})
		})
	}
}

func TestForwardNormalCompletionCancelsWatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		l, c := newCancelLeg(), newCancelConn()
		l.push(protocol.ReadyFrame())
		l.push(protocol.FinFrame())
		c.in <- nil
		done := startCancelBridge(ctx, l, c, 100*time.Millisecond)
		<-c.closed
		synctest.Wait() // Completed won, but the peer has not closed yet.
		l.in <- cancelMessage{err: io.EOF}
		r := <-done
		if r.err != nil || r.result.Code != protocol.CodeCompleted || c.reset.Load() {
			t.Fatal(r)
		}
		assertCancelClosed(t, l, c)
		l.mu.Lock()
		closedAt := l.closedAt
		l.mu.Unlock()
		closeCalls := l.closeCalls.Load()
		cancel()
		synctest.Wait()
		l.mu.Lock()
		unchanged := l.closedAt.Equal(closedAt)
		l.mu.Unlock()
		if !unchanged || l.closeCalls.Load() != closeCalls {
			t.Fatal("late cancellation closed again")
		}
		assertCancelClosed(t, l, c)
	})
}

func TestForwardCancellationJoinsForcedClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		l, c := newCancelLeg(), newCancelConn()
		release := make(chan struct{})
		l.closeGate = release
		l.blockWrite = true // The bridge's later timer cannot win this close.
		done := startCancelBridge(ctx, l, c, 100*time.Millisecond)
		<-l.readEntered
		cancel()
		<-l.closed // The T_close callback is in CloseNow, before its return.
		synctest.Wait()
		select {
		case r := <-done:
			close(release)
			t.Fatalf("Forward returned with a running close callback: %+v", r)
		default:
		}
		close(release)
		r := <-done
		if r.result.Code != protocol.CodeClosedByClient {
			t.Fatal(r)
		}
		assertCancelClosed(t, l, c)
	})
}

func TestForwardCancellationRacesFIN(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		l, c := newCancelLeg(), newCancelConn()
		l.push(protocol.ReadyFrame())
		done := startCancelBridge(ctx, l, c, 100*time.Millisecond)
		<-c.readEntered
		start := make(chan struct{})
		joined := make(chan struct{})
		go func() { <-start; cancel(); close(joined) }()
		close(start)
		l.push(protocol.FinFrame())
		c.in <- nil
		r := <-done
		<-joined
		if r.err != nil || (r.result.Code != protocol.CodeCompleted && r.result.Code != protocol.CodeClosedByClient) {
			t.Fatalf("competing FIN/cancel invented terminal result: %+v", r)
		}
		assertCancelClosed(t, l, c)
	})
}

func TestForwardCancellationLocalWriteErrorWaitsDrain(t *testing.T) {
	for _, name := range []string{"cancel_first", "local_write_error_first"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				l, c := newCancelLeg(), newCancelConn()
				var releaseWrite, releaseClose chan struct{}
				if name == "cancel_first" {
					c.blockWrite = true
				} else {
					releaseWrite, releaseClose = make(chan struct{}), make(chan struct{})
					c.writeGate, c.closeGate, c.writeErr = releaseWrite, releaseClose, io.ErrClosedPipe
				}
				l.push(protocol.ReadyFrame())
				l.push(protocol.DataFrame([]byte("blocked response")))
				done := startCancelBridge(ctx, l, c, 100*time.Millisecond)
				<-c.writeEntered
				want := protocol.CodeClosedByClient
				if releaseWrite != nil {
					close(releaseWrite)
					<-c.closeEntered
					want = protocol.CodeChannelInterrupted
				}
				cancel()
				if releaseClose != nil {
					close(releaseClose)
				}
				synctest.Wait()
				select {
				case <-c.closed:
				default:
					t.Fatal("local side has not been reclaimed")
				}
				select {
				case r := <-done:
					t.Fatalf("returned before peer close: %+v", r)
				default:
				}
				select {
				case <-l.closed:
					t.Fatal("ABORT grace was cut short")
				default:
				}
				select {
				case raw := <-l.out:
					f, err := protocol.DecodeFrame(raw, 32<<10)
					if err != nil || f.Type != protocol.FrameAbort || f.Reason != want {
						t.Fatalf("wrong first ABORT: %+v %v", f, err)
					}
				default:
					t.Fatal("no ABORT reached the leg")
				}
				_ = l.CloseNow() // Peer closes after receiving the first terminal reason.
				r := <-done
				if r.err != nil || r.result.Code != want || r.result.Remote {
					t.Fatalf("first terminal result changed: %+v", r)
				}
				assertCancelClosed(t, l, c)
			})
		})
	}
}

func TestForwardProtocolFailureKeepsAbortGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l, c := newCancelLeg(), newCancelConn()
		l.push(protocol.DataFrame([]byte("before READY")))
		start := time.Now()
		done := startCancelBridge(context.Background(), l, c, 100*time.Millisecond)
		r := <-done
		if protocol.CodeOf(r.err) != protocol.CodeProtocolError || r.result.Code != protocol.CodeProtocolError {
			t.Fatal(r)
		}
		select {
		case <-l.closed:
			t.Fatal("normal protocol failure cut short ABORT grace")
		default:
		}
		select {
		case raw := <-l.out:
			f, e := protocol.DecodeFrame(raw, 32<<10)
			if e != nil || f.Type != protocol.FrameAbort || f.Reason != protocol.CodeProtocolError {
				t.Fatal(f, e)
			}
		default:
			t.Fatal("missing protocol ABORT")
		}
		<-l.closed
		if time.Since(start) != 100*time.Millisecond {
			t.Fatal("original closing grace changed", time.Since(start))
		}
		assertCancelClosed(t, l, c)
	})
}

func TestListenCancelActive(t *testing.T) {
	w := newWorld(t)
	defer w.c.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addresses := make(chan net.Addr, 1)
	done := make(chan error, 1)
	results := make(chan tunnel.Result, 1)
	go func() {
		done <- w.c.Listen(ctx, "127.0.0.1:0", "lab-a/http", func(a net.Addr) { addresses <- a }, func(r tunnel.Result, _ error) { results <- r })
	}()
	var addr net.Addr
	select {
	case addr = <-addresses:
	case <-time.After(3 * time.Second):
		t.Fatal("listener not ready")
	}
	c, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("cancel-active-marker")); err != nil {
		t.Fatal(err)
	}
	if !w.t1.Wait(3*time.Second, func(rs []fixture.Record) bool {
		return len(rs) == 1 && string(rs[0].Data) == "cancel-active-marker" && !rs[0].EOF
	}) {
		t.Fatal("target did not observe active payload")
	}
	start := time.Now()
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(w.c.closeGrace + time.Second):
		t.Fatal("Listen waited for a peer FIN after cancellation")
	}
	if r := <-results; r.Code != protocol.CodeClosedByClient || r.Remote {
		t.Fatalf("cancel returned %+v", r)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = c.Read(make([]byte, 1)); err == nil || err == io.EOF {
		t.Fatalf("cancel did not reset local TCP: %v", err)
	} else if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		t.Fatal("local TCP still open")
	}
	if !w.t1.Wait(time.Second, func(rs []fixture.Record) bool { return len(rs) == 1 && rs[0].Done && rs[0].Reset && !rs[0].EOF }) {
		t.Fatal("target not reset/reclaimed")
	}
	t.Logf("active cancellation returned ClosedByClient and reset both TCP endpoints in %v", time.Since(start))
}
