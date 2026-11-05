package wsio

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestManagedDataJoinWaitsForFailureCallback(t *testing.T) {
	for _, name := range []string{"before_deadline", "after_deadline"} {
		t.Run(name, func(t *testing.T) {
			// A renewed grace would outlive the fixture watchdog. Only the
			// earlier, explicitly saved deadline may govern this cleanup.
			p := managedFixtureTLSWithGrace(t, false, time.Hour)
			entered := make(chan error, 1)
			release, returned := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var calls atomic.Int32
			var callbackReturnedAt time.Time
			// This cleanup runs before managedFixture's cleanup. A failed
			// assertion cannot leave the real failure coordinator in our gate.
			t.Cleanup(func() {
				unblock()
				_ = p.conn.CloseNow()
				p.conn.mu.Lock()
				running := p.conn.failureRunning
				p.conn.mu.Unlock()
				if running {
					diagnosticCleanupWait(t, p.ctx, p.conn.failureDone, "failure callback cleanup")
				}
			})
			p.conn.BindControlFailure(func(err error) {
				calls.Add(1)
				entered <- err
				<-release
				callbackReturnedAt = time.Now()
				close(returned)
			})
			read := p.run(func() error { _, _, err := p.conn.Read(p.ctx); return err })
			p.socket.armed.Store(true)
			if err := p.peer.WritePing(p.ctx, []byte("callback-lifetime")); err != nil {
				t.Fatal(err)
			}
			diagnosticWait(t, p.ctx, p.socket.entered, "real PONG socket Write")
			if p.ctx.Err() != nil {
				t.Fatal("PONG failure was not under an active request context")
			}
			deadline := time.Now().Add(time.Second)
			p.conn.SetCloseDeadline(deadline)
			close(p.socket.release)
			if err := diagnosticResult(t, p.ctx, entered, "business failure callback entered"); !errors.Is(err, errManagedSocket) {
				t.Fatal("lost the actual socket error", err)
			}
			diagnosticWait(t, p.ctx, p.socket.closed, "same transport closed before callback")
			diagnosticWait(t, p.ctx, p.conn.control.Done(), "control writer joined before callback")
			p.conn.mu.Lock()
			saved := p.conn.deadline
			p.conn.mu.Unlock()
			if !saved.Equal(deadline) || !time.Now().Before(deadline) {
				t.Fatal("callback input did not retain the original live deadline", deadline)
			}
			original, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			if err := diagnosticResult(t, original, read, "original Reader released before D"); err == nil {
				t.Fatal("Reader succeeded after the actual socket failure")
			}
			select {
			case <-p.conn.failureDone:
				t.Fatal("failure coordinator ended while its callback was blocked")
			default:
			}
			if name == "after_deadline" {
				// Begin this join only after the saved D is already expired,
				// while the callback remains blocked. WithTimeout(grace) here
				// would wait an hour instead of returning the original failure.
				diagnosticWait(t, p.ctx, original.Done(), "saved deadline expires before join")
				if original.Err() != context.DeadlineExceeded {
					t.Fatal("join input did not reach the original deadline", original.Err())
				}
			}
			joinStarted := make(chan struct{})
			var joinedAt time.Time
			joined := p.run(func() error {
				close(joinStarted)
				err := p.conn.JoinCleanup()
				joinedAt = time.Now()
				return err
			})
			if name == "before_deadline" {
				diagnosticWait(t, original, joinStarted, "outer join started under original D")
			} else {
				diagnosticWait(t, p.ctx, joinStarted, "outer join started after saved D")
			}
			var joinErr error
			if name == "before_deadline" {
				// Keep the callback blocked through half of the remaining
				// original budget. This is a test input, not a new deadline.
				remaining := time.Until(deadline)
				if remaining <= 0 {
					t.Fatal("join preparation consumed the original deadline")
				}
				hold := time.NewTimer(remaining / 2)
				defer hold.Stop()
				select {
				case err := <-joined:
					t.Fatal("join returned before callback release", err)
				case <-hold.C:
				case <-original.Done():
					t.Fatal("callback release input missed the original deadline")
				}
				if !time.Now().Before(deadline) {
					t.Fatal("callback release was not before the original deadline")
				}
				unblock()
				joinErr = diagnosticResult(t, original, joined, "callback and outer join complete before D")
				if joinErr != nil || !joinedAt.Before(deadline) {
					t.Fatal("join did not complete under its unchanged deadline", joinErr, joinedAt, deadline)
				}
			} else {
				// The fixture's original 10s watchdog only collects a broken
				// implementation. JoinCleanup itself must use the saved D.
				joinErr = diagnosticResult(t, p.ctx, joined, "blocked callback reaches original D")
				if !errors.Is(joinErr, context.DeadlineExceeded) || joinedAt.Before(deadline) {
					t.Fatal("blocked callback did not report its original deadline", joinErr, joinedAt, deadline)
				}
				select {
				case <-returned:
					t.Fatal("callback returned without its gate being released")
				default:
				}
				unblock()
			}
			diagnosticWait(t, p.ctx, returned, "business callback returned")
			diagnosticWait(t, p.ctx, p.conn.failureDone, "failure coordinator joined")
			if name == "before_deadline" && joinedAt.Before(callbackReturnedAt) {
				t.Fatal("outer join preceded the callback's actual return")
			}
			later := p.conn.JoinCleanup()
			if name == "before_deadline" && later != nil {
				t.Fatal("completed callback acquired a later cleanup failure", later)
			}
			if name == "after_deadline" && !errors.Is(later, context.DeadlineExceeded) {
				t.Fatal("later callback completion erased the missed cleanup deadline", later)
			}
			p.conn.mu.Lock()
			kept := p.conn.deadline
			p.conn.mu.Unlock()
			if !kept.Equal(deadline) || calls.Load() != 1 || !p.conn.ControlSnapshot().WorkerDone || !errors.Is(p.conn.ControlError(), errManagedSocket) {
				t.Fatal("deadline, callback ownership or first error changed")
			}
			t.Logf("original_D=%s join_at=%s callback_returned_at=%s first_join_error=%v later_join_error=%v",
				deadline.Format(time.RFC3339Nano), joinedAt.Format(time.RFC3339Nano), callbackReturnedAt.Format(time.RFC3339Nano), joinErr, later)
		})
	}
}

// managedCloseReturnGate wraps the very same Dial transport. Only its first
// Close return is held, after the real socket has already been closed. It
// exposes ownership of the force callback without a production hook.
type managedCloseReturnGate struct {
	net.Conn
	once                         sync.Once
	closed, release, returned    chan struct{}
	closeStarted, socketClosedAt time.Time
	returnedAt                   time.Time
	err                          error
}

func (g *managedCloseReturnGate) Close() error {
	g.once.Do(func() {
		g.closeStarted = time.Now()
		g.err = g.Conn.Close()
		g.socketClosedAt = time.Now()
		close(g.closed)
		<-g.release
		g.returnedAt = time.Now()
		close(g.returned)
	})
	return g.err
}

func TestManagedDataGracefulCloseUsesOriginalForceDeadline(t *testing.T) {
	// Keep the configured grace distinct from the first saved deadline. A
	// timer incorrectly restarted from grace cannot pass the 10s watchdog.
	p := managedFixtureTLSWithGrace(t, false, time.Hour)
	gate := &managedCloseReturnGate{Conn: p.socket, closed: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(gate.release) }) }
	// Release the artificial post-close gate before the existing fixture
	// attempts another CloseNow and joins its owned workers.
	t.Cleanup(unblock)
	p.conn.mu.Lock()
	if p.conn.transport != p.socket {
		p.conn.mu.Unlock()
		t.Fatal("gate does not wrap this Dial's actual transport")
	}
	p.conn.transport = gate
	p.conn.mu.Unlock()
	// There is no peer Reader and no PING. The next actual write therefore
	// belongs to this Close, not a late handshake or a control response.
	p.socket.allWrites.Store(true)
	p.socket.armed.Store(true)
	deadline := time.Now().Add(time.Second)
	p.conn.SetCloseDeadline(deadline)
	original, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var closeReturnedAt time.Time
	closed := p.run(func() error {
		err := p.conn.Close(websocket.StatusNormalClosure, "force original deadline")
		closeReturnedAt = time.Now()
		return err
	})
	diagnosticWait(t, original, p.socket.entered, "Close owns the real blocked socket Write")
	if !time.Now().Before(deadline) || p.conn.ControlSnapshot().Wire != "none" {
		t.Fatal("Close input was not established before its original D")
	}
	select {
	case <-p.socket.closed:
		t.Fatal("transport was closed before the original force deadline")
	case err := <-closed:
		t.Fatal("Close returned while its real Write was still blocked", err)
	default:
	}
	// The product timer must close the real socket at its saved D, rather
	// than waiting for coder's independent 5s close-handshake timeout.
	diagnosticWait(t, p.ctx, gate.closed, "original timer physically closes the socket")
	if gate.closeStarted.Before(deadline) {
		t.Fatal("force closed the transport before the saved original deadline", gate.closeStarted, deadline)
	}
	diagnosticWait(t, p.ctx, p.socket.closed, "blocked Write released by actual socket close")
	diagnosticWait(t, p.ctx, p.conn.control.Done(), "control worker stopped at original close")
	// The physical close has already happened. This artificial hold ONLY
	// checks callback ownership; its later release is not an extension of D
	// and the eventual return must not be described as an in-D close.
	hold := time.NewTimer(50 * time.Millisecond)
	defer hold.Stop()
	select {
	case err := <-closed:
		t.Fatal("Close returned before its running force callback", err)
	case <-hold.C:
	case <-p.ctx.Done():
		t.Fatal("fixture watchdog expired while holding force callback")
	}
	unblock()
	closeErr := diagnosticResult(t, p.ctx, closed, "Close joins the released force callback")
	diagnosticWait(t, p.ctx, gate.returned, "same-transport Close callback returned")
	if closeReturnedAt.Before(gate.returnedAt) || !p.conn.ControlSnapshot().WorkerDone {
		t.Fatal("Close relinquished a live force callback or control worker")
	}
	p.conn.mu.Lock()
	kept := p.conn.deadline
	p.conn.mu.Unlock()
	if !kept.Equal(deadline) {
		t.Fatal("force callback renewed the original deadline", kept, deadline)
	}
	if err := p.conn.JoinCleanup(); err != nil {
		t.Fatal("already completed control owner was not joined", err)
	}
	// A real forced transport close may return nil or a socket-close error.
	// Its result is retained; no nil assertion substitutes for lifecycle proof.
	t.Logf("original_D=%s force_started=%s force_delay_after_D=%s socket_closed=%s artificial_gate_returned=%s Close_returned=%s Close_error=%v",
		deadline.Format(time.RFC3339Nano), gate.closeStarted.Format(time.RFC3339Nano), gate.closeStarted.Sub(deadline), gate.socketClosedAt.Format(time.RFC3339Nano),
		gate.returnedAt.Format(time.RFC3339Nano), closeReturnedAt.Format(time.RFC3339Nano), closeErr)
}
