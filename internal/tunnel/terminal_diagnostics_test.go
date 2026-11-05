package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type terminalGateLeg struct {
	*fakeLeg
	entered, release chan struct{}
	once             sync.Once
}

func (l *terminalGateLeg) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	if len(p) > 0 && p[0] == protocol.FrameAbort {
		l.once.Do(func() { close(l.entered) })
		select {
		case <-l.release:
		case <-ctx.Done():
			return ctx.Err()
		case <-l.closed:
			return errors.New("closed during terminal cleanup")
		}
	}
	return l.fakeLeg.Write(ctx, typ, p)
}

// Winning publication precedes blocking ABORT cleanup; another real terminal
// contender must not overwrite it, even when diagnostics are sampled meanwhile.
func TestBridgeTerminalWinner(t *testing.T) {
	for _, operation := range []string{"external_abort", "target_read"} {
		t.Run(operation, func(t *testing.T) {
			c, _ := pair(t)
			l := &terminalGateLeg{fakeLeg: newLeg(), entered: make(chan struct{}), release: make(chan struct{})}
			b := New(context.Background(), l, c, 32768, grace, nil)
			if b.Diagnostics().Terminal != nil {
				t.Fatal("unmade terminal decision")
			}
			b.up.Store(17)
			b.down.Store(29)
			b.finSend.Store(finWriting)
			b.finRecv.Store(true)
			first, second := make(chan struct{}), make(chan struct{})
			secondStarted := false
			var release sync.Once
			go func() {
				defer close(first)
				if operation == "external_abort" {
					b.Abort(protocol.CodeChannelInterrupted)
				} else {
					b.finish(protocol.CodeChannelInterrupted, false, true, operation, errors.New("private payload token must not appear"))
				}
			}()
			t.Cleanup(func() {
				release.Do(func() { close(l.release) })
				_ = l.CloseNow()
				waitTunnelSignal(t, first, "first terminal worker joined")
				if secondStarted {
					waitTunnelSignal(t, second, "second terminal worker joined")
				}
			})
			waitTunnelSignal(t, l.entered, "terminal ABORT write")
			snapshot := b.Diagnostics()
			before := snapshot.Terminal
			if before != nil && before.AtNS > snapshot.SampledNS {
				t.Fatal("terminal is later than its snapshot")
			}
			if before == nil || before.Operation != operation || before.Code != protocol.CodeChannelInterrupted || before.Remote || before.AtNS <= 0 || before.ContextState != "active" || before.FINSendState != "writing" || !before.FINReceived || before.BytesUp != 17 || before.BytesDown != 29 {
				t.Fatalf("wrong winning observation: %+v", before)
			}
			wantClass := "none"
			if operation == "target_read" {
				wantClass = "other"
			}
			if before.ErrorClass != wantClass {
				t.Fatal(before.ErrorClass)
			}
			select {
			case <-b.Done():
				t.Fatal("cleanup has not returned")
			default:
			}
			go func() {
				defer close(second)
				b.finish(protocol.CodeLicenseExpired, false, false, "leg_read", context.DeadlineExceeded)
			}()
			secondStarted = true
			// A caller cannot mutate the published object through a snapshot.
			before.Operation = "caller mutation"
			for range 100 {
				snapshot := b.Diagnostics()
				if got := snapshot.Terminal; got.Operation != operation || got.AtNS != before.AtNS || got.AtNS > snapshot.SampledNS {
					t.Fatalf("changed or future decision: %+v", got)
				}
			}
			release.Do(func() { close(l.release) })
			waitTunnelSignal(t, first, "first terminal return")
			waitTunnelSignal(t, second, "second terminal return")
			got := b.Diagnostics().Terminal
			if got.Operation != operation || got.Code != b.Result().Code || got.AtNS != before.AtNS {
				t.Fatalf("loser overwrote decision: %+v", got)
			}
			encoded, err := json.Marshal(b.Diagnostics())
			if err != nil || strings.Contains(string(encoded), "private payload") {
				t.Fatalf("unsafe diagnostic: %s %v", encoded, err)
			}
		})
	}
}

// Decode failures are protocol decisions with no I/O error. Cover both the
// first READY boundary and post-READY DATA validation through their real paths.
func TestBridgeTerminalProtocolDecision(t *testing.T) {
	for _, stage := range []string{"wait_ready", "running"} {
		t.Run(stage, func(t *testing.T) {
			c, peer := pair(t)
			l := newLeg()
			defer l.CloseNow()
			b := New(context.Background(), l, c, 32768, grace, nil)
			operation := "wait_ready_frame"
			if stage == "wait_ready" {
				l.push([]byte{protocol.FrameReady, 1})
				if err := b.WaitReady(); protocol.CodeOf(err) != protocol.CodeProtocolError {
					t.Fatal(err)
				}
			} else {
				operation = "leg_frame"
				l.push(protocol.ReadyFrame())
				if err := b.WaitReady(); err != nil {
					t.Fatal(err)
				}
				done := run(b)
				l.push([]byte{protocol.FrameData})
				if got := wait(t, done); got.Code != protocol.CodeProtocolError {
					t.Fatal(got)
				}
			}
			l.expectAbort(t, protocol.CodeProtocolError)
			snapshot := b.Diagnostics()
			d := snapshot.Terminal
			if d == nil || d.Code != protocol.CodeProtocolError || d.Operation != operation || d.ErrorClass != "none" || d.Remote || d.AtNS <= 0 || d.AtNS > snapshot.SampledNS {
				t.Fatalf("protocol decision mislabeled as transport failure: %+v", d)
			}
			expectReset(t, peer)
		})
	}
}

func TestBridgeTerminalFINWriteFailure(t *testing.T) {
	c, peer := pair(t)
	l := newLeg()
	err := errors.New("private FIN transport error")
	l.writeErr.Store(&err)
	b := New(context.Background(), l, c, 32768, grace, nil)
	done := run(b)
	t.Cleanup(func() { _ = l.CloseNow(); _ = c.Close() })
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, done); got.Code != protocol.CodeChannelInterrupted {
		t.Fatal(got)
	}
	d := b.Diagnostics().Terminal
	if d == nil || d.Operation != "fin_write" || d.FINSendState != "failed" || d.ErrorClass != "other" || d.Remote {
		t.Fatalf("FIN failure: %+v", d)
	}
}

func TestBridgeTerminalCompletionRechecksLicense(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "completed"
		if denied {
			name = "license_wins"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := pair(t)
			l := newLeg()
			defer l.CloseNow()
			var calls atomic.Int64
			b := New(context.Background(), l, c, 32768, grace, func() protocol.Code {
				calls.Add(1)
				if denied {
					return protocol.CodeLicenseExpired
				}
				return ""
			})
			close(b.drained)
			b.finSend.Store(finWritten)
			b.finRecv.Store(true)
			b.finish(protocol.CodeCompleted, false, false, "fin_complete", nil)
			wantCode, wantOperation := protocol.CodeCompleted, "fin_complete"
			if denied {
				wantCode, wantOperation = protocol.CodeLicenseExpired, "license_check"
			}
			got := b.Diagnostics().Terminal
			if got == nil || got.Code != wantCode || got.Operation != wantOperation || got.FINSendState != "written" || got.ErrorClass != "none" || b.Result().Code != wantCode || calls.Load() != 1 {
				t.Fatalf("final license priority changed: %+v calls=%d", got, calls.Load())
			}
		})
	}
}
