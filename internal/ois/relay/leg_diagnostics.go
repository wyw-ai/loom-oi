package relay

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// Each operation publishes at most one immutable failure. No payload or error
// text is retained, and these observations do not participate in decisions.
type legOperationDiagnostic struct {
	StartedNS                int64  `json:"started_ns"`
	CompletedNS              int64  `json:"completed_ns"`
	ErrorClass               string `json:"error_class"`
	ContextState             string `json:"context_state"`
	ActivityBefore           int64  `json:"activity_before"`
	ActivityAfter            int64  `json:"activity_after"`
	ForwardingBefore         bool   `json:"forwarding_before"`
	ForwardingAfter          bool   `json:"forwarding_after"`
	MessageType              string `json:"message_type,omitempty"`
	PingPhase                string `json:"ping_phase,omitempty"`
	MissesBefore             int    `json:"misses_before"`
	MissesAfter              int    `json:"misses_after"`
	DecisionActivityPrevious int64  `json:"decision_activity_previous"`
	DecisionActivityCurrent  int64  `json:"decision_activity_current"`
	DecisionForwarding       *bool  `json:"decision_forwarding"`
}

type legCloseDiagnostic struct {
	AtNS   int64  `json:"at_ns"`
	Source string `json:"source"`
}

type legDiagnostics struct {
	started time.Time
	read    atomic.Pointer[legOperationDiagnostic]
	write   atomic.Pointer[legOperationDiagnostic]
	ping    atomic.Pointer[legOperationDiagnostic]
	close   atomic.Pointer[legCloseDiagnostic]
	workers atomic.Int32
}

type legDiagnosticSnapshot struct {
	StartedAt  time.Time               `json:"started_at"`
	SampledNS  int64                   `json:"sampled_ns"`
	Role       string                  `json:"role"`
	ChannelID  string                  `json:"channel_id"`
	Read       *legOperationDiagnostic `json:"read"`
	Write      *legOperationDiagnostic `json:"write"`
	Ping       *legOperationDiagnostic `json:"ping"`
	FirstClose *legCloseDiagnostic     `json:"first_close"`
}

func (l *leg) diagnosticNS() int64 { return max(1, time.Since(l.diagnostics.started).Nanoseconds()) }

func (l *leg) beginOperation() legOperationDiagnostic {
	return legOperationDiagnostic{StartedNS: l.diagnosticNS(), ActivityBefore: l.activity.Load(), ForwardingBefore: l.forwarding.Load()}
}

func (l *leg) endOperation(op *legOperationDiagnostic, ctx context.Context, err error) {
	op.CompletedNS = l.diagnosticNS()
	op.ErrorClass = wsio.ErrorClass(err)
	op.ContextState = wsio.ContextState(ctx)
	op.ActivityAfter = l.activity.Load()
	op.ForwardingAfter = l.forwarding.Load()
}

// Keep successful per-frame observations on the stack. Allocate only when
// publishing the first failure of an operation, never once per DATA frame.
func publishLegOperation(slot *atomic.Pointer[legOperationDiagnostic], op legOperationDiagnostic) {
	if slot.Load() == nil {
		copy := op
		slot.CompareAndSwap(nil, &copy)
	}
}

func diagnosticMessage(frame []byte) string {
	if len(frame) > 0 {
		switch frame[0] {
		case protocol.FrameReady:
			return "READY"
		case protocol.FrameData:
			return "DATA"
		case protocol.FrameFin:
			return "FIN"
		case protocol.FrameAbort:
			return "ABORT"
		}
	}
	return "unknown"
}

func (l *leg) diagnosticSnapshot() legDiagnosticSnapshot {
	out := legDiagnosticSnapshot{StartedAt: l.diagnostics.started, Role: l.role.String(), ChannelID: l.ch.id,
		Read: l.diagnostics.read.Load(), Write: l.diagnostics.write.Load(), Ping: l.diagnostics.ping.Load(), FirstClose: l.diagnostics.close.Load()}
	out.SampledNS = l.diagnosticNS()
	return out
}

// closeNow witnesses only our explicit entry into CloseNow. The WebSocket
// library can have closed the transport earlier; that fact is not inferred.
func (l *leg) closeNow(source string) {
	l.diagnostics.close.CompareAndSwap(nil, &legCloseDiagnostic{AtNS: l.diagnosticNS(), Source: source})
	_ = l.conn.CloseNow()
}

// workerEnded runs before the original conns/loops Done, without Hub.mu.
// The last existing worker emits the one complete per-leg diagnostic record.
func (l *leg) workerEnded() {
	if l.diagnostics.workers.Add(-1) == 0 {
		l.h.log.Info("leg ended", "channel", l.ch.id, "role", l.role.String(), "io_failures", l.diagnosticSnapshot())
	}
}

// Caller holds Hub.mu. The pointers refer to immutable published observations;
// pending operations stay null and are not awaited to complete this snapshot.
func (h *Hub) legDiagnosticRecord(ch *channel) []any {
	var snapshots [2]*legDiagnosticSnapshot
	for role, leg := range ch.legs {
		if leg != nil {
			snapshot := leg.diagnosticSnapshot()
			snapshots[role] = &snapshot
		}
	}
	return []any{"leg_failures", snapshots}
}
