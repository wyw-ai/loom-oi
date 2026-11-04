package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Receive-time validation and consuming the preflight result are separate
// instructions, even under the same coordinator lock. A local scheduling delay
// must not extend CHECK_MS. This uses only the injected clock, never wall time.
func TestAdmissionCompletionDeadline(t *testing.T) {
	for _, name := range []string{"single_check_deadline", "past_check_deadline", "total_deadline_priority", "valid_budget", "revoked_priority", "cancelled_priority"} {
		t.Run(name, func(t *testing.T) {
			f := newObservedFixture(t, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := asyncOpen(f.h, ctx, f.alice, f.request("completion-deadline"))
			request := expect[*protocol.AdmissionCheck](f.o)
			reply := f.reply(request, protocol.ReadinessUnknown, 0)
			f.h.mu.Lock()
			check := f.h.admissions[request.CheckID]
			received := check.deadline.Add(-2 * time.Millisecond)
			consumed := check.deadline
			want := protocol.CodeReadinessCheckFailed
			switch name {
			case "past_check_deadline":
				consumed = consumed.Add(time.Millisecond)
			case "total_deadline_priority":
				consumed = check.op.deadline
				want = protocol.CodePrepareTimeout
			case "valid_budget":
				consumed = check.deadline.Add(-time.Millisecond)
				want = ""
			case "revoked_priority":
				consumed = check.op.deadline.Add(time.Millisecond)
				token := f.h.tokens[f.alice.TokenID]
				token.RevokedAt = &received
				f.h.tokens[f.alice.TokenID] = token
				want = protocol.CodeUnauthenticated
			case "cancelled_priority":
				consumed = check.op.deadline.Add(time.Millisecond)
				// The waiter cannot acquire Hub.mu yet. createPrepared must see
				// cancellation itself before assigning any Channel or ticket.
				cancel()
			}
			f.clk.Add(consumed.Sub(f.clk.Now()))
			f.h.onAdmissionResult(check.sess, reply, received)
			f.h.mu.Unlock()
			out := opened(t, done)
			switch {
			case name == "cancelled_priority":
				if !errors.Is(out.err, context.Canceled) {
					t.Fatal("last waiter cancellation lost priority", out.err)
				}
			case want == "":
				if out.err != nil || out.result.Response.State != protocol.StatePreparing || out.result.Ticket == "" {
					t.Fatal("unexpired preflight could not prepare", out.result.Response, out.err)
				}
				prepare := f.o.prepareMsg()
				if prepare.ChannelID != out.result.Response.ChannelID || prepare.PrepareTTLMS != ms(check.op.deadline.Sub(consumed)) {
					t.Fatal("valid preparation renewed or lost total D", prepare)
				}
			default:
				if protocol.CodeOf(out.err) != want {
					t.Fatalf("consumed=%s check=%s total=%s: got %v, want %s", consumed, check.deadline, check.op.deadline, out.err, want)
				}
				if want == protocol.CodeReadinessCheckFailed {
					var pe *protocol.Error
					if !errors.As(out.err, &pe) || pe.Reason != "timeout" || pe.Code.HTTPStatus() != http.StatusServiceUnavailable {
						t.Fatal("lost single-check timeout diagnostics", out.err)
					}
				} else if want == protocol.CodePrepareTimeout && want.HTTPStatus() != http.StatusGatewayTimeout {
					t.Fatal("total preparation timeout lost HTTP 504")
				}
			}
			f.h.mu.Lock()
			pending, admissions, operations := len(f.h.pending), len(f.h.admissions), len(f.h.operations)
			channels, quota := len(f.h.channels), f.h.quota.total
			f.h.mu.Unlock()
			if pending != 0 || admissions != 0 || operations != 0 {
				t.Fatalf("finished operation leaked: pending=%d admissions=%d operations=%d", pending, admissions, operations)
			}
			stats := f.h.Stats()
			if want == "" {
				if channels != 1 || quota != 1 || stats.Tickets != 2 {
					t.Fatalf("valid preparation did not acquire exactly once: channels=%d quota=%d tickets=%d", channels, quota, stats.Tickets)
				}
				if _, _, err := f.h.CloseChannel(f.alice, out.result.Response.ChannelID); err != nil {
					t.Fatal(err)
				}
				if stats = f.h.Stats(); stats.Tickets != 0 || stats.Channels.Preparing != 0 {
					t.Fatal("valid preparation did not release after close", stats)
				}
			} else if channels != 0 || quota != 0 || stats.Tickets != 0 || out.result.Response.Attach != nil || out.result.Ticket != "" {
				t.Fatalf("failed/cancelled preflight created resources: channels=%d quota=%d tickets=%d", channels, quota, stats.Tickets)
			}
		})
	}
}

func TestAdmissionV1HasNoCheckDeadline(t *testing.T) {
	f := newFixture(t)
	flow := f.prepare()
	if flow.open.Response.State != protocol.StatePreparing || flow.open.Response.Readiness != protocol.ReadinessUnknown || flow.prep.Binding != nil {
		t.Fatal("legacy best-effort preparation changed", flow.open.Response, flow.prep)
	}
	if stats := f.h.Stats(); stats.Observation.PendingChecks != 0 || stats.Observation.PendingOpens != 0 || stats.Tickets != 2 {
		t.Fatal("legacy path created a check or lost tickets", stats)
	}
}
