package relay

import (
	"context"
	"math"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const telemetryMax = 4 << 10
const telemetryWriteBudget = 100 * time.Millisecond
const criticalBurstLimit = 8

// sendChecked reserves bounded critical capacity. Its callback runs with the
// coordinator lock at actual dequeue and may discard cancelled/expired work.
func (s *session) sendChecked(m protocol.Message, before func(time.Time) bool) bool {
	if s.ended {
		return false
	}
	select {
	case s.out <- ctrlOut{message: m, before: before, queued: s.h.now()}:
		return true
	default:
		s.h.endSession(s, causeLost, "", "")
		return false
	}
}

// sendTelemetry never borrows critical capacity and never blocks the state
// coordinator. The caller retains its per-exposure dirty marker on refusal.
func (s *session) sendTelemetry(m protocol.Message, before func(time.Time) bool) bool {
	if s.ended {
		return false
	}
	select {
	case s.telemetry <- ctrlOut{message: m, before: before, queued: s.h.now()}:
		return true
	default:
		return false
	}
}

func (s *session) nextControl(burst int) (ctrlOut, bool, bool) {
	if burst >= criticalBurstLimit {
		select {
		case m := <-s.telemetry:
			return m, true, true
		default:
		}
	}
	select {
	case m := <-s.out:
		return m, false, true
	default:
	}
	select {
	case m := <-s.telemetry:
		return m, true, true
	default:
	}
	select {
	case m := <-s.out:
		return m, false, true
	case m := <-s.telemetry:
		return m, true, true
	case <-s.h.ctx.Done():
		return ctrlOut{}, false, false
	}
}

func (s *session) writeLoop() {
	defer s.h.loops.Done()
	burst := 0
	for {
		m, telemetry, ok := s.nextControl(burst)
		if !ok {
			_ = s.conn.CloseNow()
			return
		}
		if m.close {
			_ = s.conn.Close("")
			return
		}
		// Cancelled critical entries still consumed a scheduling turn. Counting
		// only successful writes lets a stream of obsolete checks starve ready
		// telemetry indefinitely while each callback discards its own message.
		if telemetry {
			burst = 0
		} else {
			burst = min(criticalBurstLimit, burst+1)
		}
		s.h.mu.Lock()
		_, isError := m.message.(*protocol.ControlError)
		if (s.ended && !isError) || (m.before != nil && !m.before(s.h.now())) {
			s.h.mu.Unlock()
			continue
		}
		if s.seq == math.MaxUint64 {
			s.h.endSession(s, causeLost, "", "")
			s.h.mu.Unlock()
			_ = s.conn.CloseNow()
			return
		}
		s.seq++
		if _, heartbeat := m.message.(*protocol.Heartbeat); heartbeat {
			s.hbSeq = s.seq
			s.hbQueued = false
		}
		seq := s.seq
		s.h.mu.Unlock()
		data := protocol.EncodeControl(m.message, seq)
		if telemetry && len(data) > min(telemetryMax, s.h.oblim.MessageMax) {
			s.h.mu.Lock()
			s.h.endSession(s, causeLost, protocol.CodePayloadTooLarge, "observation message exceeds telemetry bound")
			s.h.mu.Unlock()
			continue
		}
		timeout := limits.D(s.h.lim.HeartbeatMS)
		if telemetry {
			timeout = min(telemetryWriteBudget, limits.D(s.h.oblim.WriteMS))
		}
		ctx, cancel := context.WithTimeout(s.h.ctx, timeout)
		err := s.conn.Write(ctx, true, data)
		cancel()
		if err != nil {
			_ = s.conn.CloseNow()
			return
		}
	}
}
