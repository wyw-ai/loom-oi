package wsio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestReadPhasePinnedWrappers(t *testing.T) {
	const private = "private-token-payload-address"
	wrap := func(label string, err error) error { return fmt.Errorf("%s%w", label, err) }
	handle := func(opcode string, err error) error {
		return wrap("failed to handle control frame "+opcode+": ", err)
	}
	pong := func(err error) error { return wrap("failed to write control frame opPong: ", err) }
	lock := wrap("failed to acquire lock: ", context.DeadlineExceeded)
	frame := wrap("failed to write frame: ", errors.New(private))
	payload := wrap("failed to read frame payload: ", io.ErrUnexpectedEOF)
	type example struct {
		name string
		err  error
		want string
	}
	cases := []example{
		{"pong_lock", handle("opPing", pong(lock)), "pong_write_lock"},
		{"pong_frame", handle("opPing", pong(frame)), "pong_control_write"},
		{"pong_frame_flush", handle("opPing", pong(wrap("failed to write frame: ", wrap("failed to flush: ", errors.New(private))))), "pong_control_write"},
		{"pong_frame_header", handle("opPing", pong(wrap("failed to write frame: ", wrap("failed to write frame header: ", errors.New(private))))), "pong_control_write"},
		{"frame_header", wrap("failed to read frame header: ", io.EOF), "frame_header"},
		{"frame_payload", payload, "frame_payload"},
		{"opaque_inner", errors.New("failed to handle control frame opPing: failed to write control frame opPong: failed to acquire lock: " + private), "unknown"},
		{"wrong_control_opcode", handle("opBinary", pong(lock)), "unknown"},
		{"wrong_response_opcode", handle("opPing", wrap("failed to write control frame opPing: ", lock)), "unknown"},
		{"response_to_wrong_control", handle("opPong", pong(lock)), "unknown"},
		{"wrong_control_order", pong(handle("opPing", lock)), "unknown"},
		{"extra_before_pong", handle("opPing", wrap("extra: ", pong(lock))), "unknown"},
		{"extra_before_phase", handle("opPing", pong(wrap("extra: ", lock))), "unknown"},
		{"missing_write_control", handle("opPing", frame), "unknown"},
		{"opaque_pong_phase", handle("opPing", pong(errors.New("failed to acquire lock: "+private))), "unknown"},
		{"opaque_control_payload", handle("opPing", errors.New("failed to read frame payload: "+private)), "unknown"},
		{"sentinel_text", handle("opPing", errors.New(context.DeadlineExceeded.Error())), "unknown"},
		{"wrapped_sentinel", handle("opPing", wrap("extra: ", context.DeadlineExceeded)), "unknown"},
		{"bare_deadline", context.DeadlineExceeded, "unknown"},
		{"bare_closed", net.ErrClosed, "unknown"},
		{"closed_after_write_control", handle("opPing", pong(net.ErrClosed)), "unknown"},
		{"changed_opcode_case", handle("opping", pong(lock)), "unknown"},
		{"changed_phase_case", handle("opPing", pong(wrap("Failed to acquire lock: ", context.DeadlineExceeded))), "unknown"},
		{"nested_read_outer", wrap("failed to get reader: ", handle("opPing", pong(lock))), "unknown"},
		{"joined_control", errors.Join(handle("opPing", pong(lock)), errors.New(private)), "unknown"},
		{"joined_pong", handle("opPing", errors.Join(pong(lock), errors.New(private))), "unknown"},
		{"peer_close_reason", websocket.CloseError{Code: websocket.StatusPolicyViolation, Reason: "failed to read frame header: " + private}, "unknown"},
	}
	for _, op := range []string{"opPing", "opPong", "opClose"} {
		cases = append(cases,
			example{op + "_payload", handle(op, payload), "control_payload"},
			example{op + "_deadline", handle(op, context.DeadlineExceeded), "control_payload"},
			example{op + "_canceled", handle(op, context.Canceled), "control_payload"},
			example{op + "_closed", handle(op, net.ErrClosed), "control_payload"})
	}
	check := func(t *testing.T, err error, want string) {
		t.Helper()
		class := ErrorClass(err)
		got := ReadPhase(err)
		if got != want || ErrorClass(err) != class {
			t.Fatalf("read phase=%q want=%q class=%q", got, want, ErrorClass(err))
		}
		encoded, marshalErr := json.Marshal(map[string]string{"read_phase": got, "error_class": class})
		if marshalErr != nil || bytes.Contains(encoded, []byte(private)) {
			t.Fatal("read classification exposed its private leaf", marshalErr)
		}
	}
	for _, outer := range []struct{ name, label string }{
		{"get_reader", "failed to get reader: "}, {"message_body", "failed to read: "},
	} {
		t.Run(outer.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) { check(t, wrap(outer.label, tc.err), tc.want) })
			}
		})
	}
	valid := wrap("failed to get reader: ", handle("opPing", pong(lock)))
	for _, tc := range []example{
		{"nil", nil, "unknown"},
		{"opaque_full", errors.New(valid.Error()), "unknown"},
		{"wrong_outer", wrap("extra: ", valid), "unknown"},
		{"changed_outer_case", wrap("Failed to get reader: ", handle("opPing", pong(lock))), "unknown"},
		{"joined_outer", errors.Join(valid, errors.New(private)), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) { check(t, tc.err, tc.want) })
	}
}

type diagnosticControlWitness struct {
	ctx         context.Context
	receivedAt  time.Time
	deadline    time.Time
	hasDeadline bool
	state       string
}

func TestReadPhasePinnedLibrary(t *testing.T) {
	for _, phase := range []string{"pong_write_lock", "pong_control_write"} {
		t.Run(phase+"_deadline", func(t *testing.T) {
			outer, cancelOuter := context.WithCancel(context.Background())
			defer cancelOuter()
			witnesses := make(chan diagnosticControlWitness, 1)
			var callbacks atomic.Int32
			onPing := func(ctx context.Context, _ []byte) bool {
				d, ok := ctx.Deadline()
				w := diagnosticControlWitness{ctx: ctx, receivedAt: time.Now(), deadline: d, hasDeadline: ok, state: ContextState(ctx)}
				callbacks.Add(1)
				select {
				case witnesses <- w:
				default:
				}
				return true // Preserve the library's original PONG write and deadline.
			}
			p := newDiagnosticPairOptions(t, true, 10*time.Second, true, outer, onPing)
			p.gate.armed.Store(true) // Handshake is complete; selection occurs at Write entry.
			var write <-chan error
			if phase == "pong_write_lock" {
				write = p.run(func() error {
					return p.client.Write(context.Background(), websocket.MessageBinary, []byte("held-data"))
				})
				diagnosticWait(t, p.ctx, p.gate.entered, "real DATA owns frame write lock")
				if p.gate.opcode.Load() != 2 {
					t.Fatal("DATA gate selected an unrelated Write")
				}
			}
			peerCtx, cancelPeer := context.WithCancel(p.ctx)
			defer cancelPeer()
			peerPing := p.run(func() error { return p.peer.Ping(peerCtx) })
			var witnessed diagnosticControlWitness
			select {
			case witnessed = <-witnesses:
			case <-p.ctx.Done():
				t.Fatal("real incoming Ping was not observed")
			}
			remaining := witnessed.deadline.Sub(witnessed.receivedAt)
			if !witnessed.hasDeadline || witnessed.state != "active" || remaining <= 0 || remaining > 5*time.Second {
				t.Fatal("callback did not witness the original active control deadline")
			}
			if phase == "pong_control_write" {
				diagnosticWait(t, p.ctx, p.gate.entered, "real PONG socket Write entry")
				if p.gate.opcode.Load() != 10 || witnessed.ctx.Err() != nil {
					t.Fatal("PONG Write did not enter before its original deadline")
				}
			}
			diagnosticWait(t, p.ctx, witnessed.ctx.Done(), "original library 5s control deadline")
			readErr := diagnosticResult(t, p.ctx, p.readerResult, "single client Reader return")
			if witnessed.ctx.Err() != context.DeadlineExceeded || ErrorClass(readErr) != "deadline" ||
				ContextState(outer) != "active" || ReadPhase(readErr) != phase || p.readerReturnedAt.Before(witnessed.deadline) || callbacks.Load() != 1 {
				t.Fatalf("unexpected read boundary phase=%q class=%q outer=%q control=%q callbacks=%d",
					ReadPhase(readErr), ErrorClass(readErr), ContextState(outer), ContextState(witnessed.ctx), callbacks.Load())
			}
			if phase == "pong_write_lock" {
				select {
				case <-p.gate.closed:
					t.Fatal("PONG write-lock timeout closed the transport")
				default:
				}
				p.gate.free()
				if err := diagnosticResult(t, p.ctx, write, "held DATA write return"); err != nil {
					t.Fatal("PONG lock timeout damaged the existing write", err)
				}
				diagnosticReadData(t, p, "held-data")
				if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("next-data")); err != nil {
					t.Fatal("DATA after PONG lock timeout", err)
				}
				diagnosticReadData(t, p, "next-data")
				// The first client Reader's result was received above. A new
				// reader now proves the opposite direction, without concurrent
				// readers or using a canceled context from the failed operation.
				returnedData := p.run(func() error {
					typ, data, err := p.client.Read(p.ctx)
					if err != nil {
						return err
					}
					if typ != websocket.MessageBinary || string(data) != "return-data" {
						return errors.New("unexpected reverse DATA after PONG lock timeout")
					}
					return nil
				})
				if err := p.peer.Write(p.ctx, websocket.MessageBinary, []byte("return-data")); err != nil {
					t.Fatal("reverse DATA write after PONG lock timeout", err)
				}
				if err := diagnosticResult(t, p.ctx, returnedData, "reverse DATA read"); err != nil {
					t.Fatal("reverse DATA did not survive PONG lock timeout", err)
				}
			} else {
				diagnosticWait(t, p.ctx, p.gate.closed, "library closed active PONG Write")
				if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("must-not-write")); err == nil {
					t.Fatal("PONG active-write deadline left a writable connection")
				}
			}
			cancelPeer()
			if err := diagnosticResult(t, p.ctx, peerPing, "peer Ping return"); err == nil {
				t.Fatal("peer received a PONG which was never written")
			}
			t.Logf("original control remaining=%s; Read returned %s after deadline; phase=%s outer=active",
				remaining, p.readerReturnedAt.Sub(witnessed.deadline), phase)
		})
	}
}

func TestDiagnosticCleanupDeadline(t *testing.T) {
	t.Run("expired_absolute_deadline_is_not_renewed", func(t *testing.T) {
		original := time.Now().Add(-time.Second)
		cleanup, cancel := diagnosticCleanupContext(original, true)
		defer cancel()
		deadline, ok := cleanup.Deadline()
		if !ok || !deadline.Equal(original) || cleanup.Err() != context.DeadlineExceeded {
			t.Fatal("expired original watchdog was renewed for cleanup")
		}
	})
	t.Run("operation_cancel_keeps_remaining_cleanup_time", func(t *testing.T) {
		operation, cancelOperation := context.WithTimeout(context.Background(), time.Hour)
		original, _ := operation.Deadline()
		cancelOperation()
		cleanup, cancelCleanup := diagnosticCleanupContext(original, true)
		defer cancelCleanup()
		deadline, ok := cleanup.Deadline()
		if operation.Err() != context.Canceled || !ok || !deadline.Equal(original) || cleanup.Err() != nil {
			t.Fatal("cleanup inherited operation cancellation or changed its deadline")
		}
		cancelCleanup()
		if cleanup.Err() != context.Canceled {
			t.Fatal("cleanup context cannot be canceled independently")
		}
	})
	t.Run("legacy_gets_original_independent_window", func(t *testing.T) {
		before := time.Now()
		cleanup, cancel := diagnosticCleanupContext(before.Add(-time.Second), false)
		defer cancel()
		after := time.Now()
		deadline, ok := cleanup.Deadline()
		if !ok || deadline.Before(before.Add(5*time.Second)) || deadline.After(after.Add(5*time.Second)) || cleanup.Err() != nil {
			t.Fatal("legacy cleanup lost its independent live 5s window")
		}
	})
}
