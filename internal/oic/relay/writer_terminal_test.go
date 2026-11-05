package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestControlWriterTerminalPrecedesHandoff(t *testing.T) {
	for _, scenario := range []string{"write_error", "nil_after_expiry", "valid_handoff"} {
		t.Run(scenario, func(t *testing.T) {
			w, p, gate, free := handoffWriterPair(t, 1)
			builderEntered, builderRelease := make(chan struct{}), make(chan struct{})
			cancelEntered, cancelRelease := make(chan struct{}), make(chan struct{})
			var releaseBuilder, releaseCancel sync.Once
			t.Cleanup(func() {
				releaseBuilder.Do(func() { close(builderRelease) })
				releaseCancel.Do(func() { close(cancelRelease) })
			})
			// Hold selection, then instrument the existing per-request cleanup.
			// The writer's production mutex is never held across either gate.
			critical := make(chan error, 1)
			go func() {
				critical <- w.sendBuilt(context.Background(), func(time.Time) (protocol.Message, error) {
					close(builderEntered)
					<-builderRelease
					return nil, nil
				})
			}()
			select {
			case <-builderEntered:
			case <-time.After(wait):
				t.Fatal("selection gate was not reached")
			}
			if scenario == "write_error" {
				gate.resultErr = errors.New("controlled real-write return error")
			}
			gate.armed.Store(true)
			if _, err := w.offerUntil("query:old", context.Background(), time.Now().Add(500*time.Millisecond), func(time.Time) (protocol.Message, error) {
				return &protocol.HeartbeatAck{AckSeq: 1}, nil
			}); err != nil {
				t.Fatal(err)
			}
			writeResult := make(chan error, 1)
			w.mu.Lock()
			q := w.telemetry["query:old"]
			originalCancel := q.cancel
			q.done = writeResult
			q.cancel = func() {
				originalCancel()
				close(cancelEntered)
				<-cancelRelease
			}
			w.telemetry["query:old"] = q
			w.mu.Unlock()
			releaseBuilder.Do(func() { close(builderRelease) })
			if err := <-critical; err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.written:
			case <-time.After(wait):
				t.Fatal("real socket write did not reach return gate")
			}
			if ack, ok := p.recv().(*protocol.HeartbeatAck); !ok || ack.AckSeq != 1 {
				t.Fatal("peer did not receive the complete original frame", ack)
			}
			w.mu.Lock()
			writeDeadline := q.query.writeDeadline
			w.mu.Unlock()
			if scenario == "nil_after_expiry" {
				// The real websocket context closes the underlying socket. The
				// already-complete net.Conn.Write then returns its original nil.
				select {
				case <-gate.closed:
				case <-time.After(wait):
					t.Fatal("original write deadline did not close the socket")
				}
				if time.Now().Before(writeDeadline) {
					t.Fatal("fixture closed the socket before its actual deadline")
				}
			} else {
				if !time.Now().Before(writeDeadline) {
					t.Fatal("fixture missed the original write budget")
				}
				free()
			}
			select {
			case <-cancelEntered:
			case <-time.After(wait):
				t.Fatal("request cleanup gate was not reached")
			}
			actual := <-writeResult
			built := make(chan struct{}, 1)
			_, replacement := w.offerUntil("query:next", context.Background(), time.Now().Add(500*time.Millisecond), func(time.Time) (protocol.Message, error) {
				built <- struct{}{}
				return nil, nil
			})
			t.Logf("scenario=%s write_result=%v session=%v replacement=%v", scenario, actual, w.ctx.Err(), replacement)
			if scenario == "valid_handoff" {
				if actual != nil || replacement != nil || w.ctx.Err() != nil {
					t.Fatal("valid write lost its healthy handoff", actual, replacement, w.ctx.Err())
				}
			} else {
				if actual == nil || replacement == nil || w.ctx.Err() == nil {
					t.Fatal("terminal write published a reusable query slot", actual, replacement, w.ctx.Err())
				}
				if scenario == "nil_after_expiry" && !errors.Is(actual, context.DeadlineExceeded) {
					t.Fatal("expired nil result lost its original deadline", actual)
				}
			}
			releaseCancel.Do(func() { close(cancelRelease) })
			if scenario == "valid_handoff" {
				select {
				case <-built:
				case <-time.After(wait):
					t.Fatal("healthy replacement did not build")
				}
			} else {
				select {
				case <-w.done:
				case <-time.After(wait):
					t.Fatal("terminal writer did not exit")
				}
				select {
				case <-built:
					t.Fatal("replacement builder ran after terminal write")
				default:
				}
			}
		})
	}
}

func TestControlWriteResultUsesOriginalDeadline(t *testing.T) {
	sentinel := errors.New("original socket error")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		end  time.Time
		in   error
		want error
	}{
		{"socket_error", cancelled, time.Now().Add(-time.Second), sentinel, sentinel},
		{"cancelled_nil", cancelled, time.Now().Add(time.Second), nil, context.Canceled},
		{"expired_context_nil", expired, time.Now().Add(-time.Second), nil, context.DeadlineExceeded},
		// A timer callback may not have executed: absolute expiry alone must
		// reject success, even while the context's Err is still nil.
		{"deadline_before_callback", context.Background(), time.Now().Add(-time.Second), nil, context.DeadlineExceeded},
		{"within_budget", context.Background(), time.Now().Add(time.Second), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := controlWriteResult(tc.ctx, tc.end, tc.in); !errors.Is(got, tc.want) {
				t.Fatalf("result=%v want=%v", got, tc.want)
			}
		})
	}
}
