package tunnel

import (
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// controlledLeg is optional so existing alternate legs keep their lifecycle.
type controlledLeg interface {
	SetCloseDeadline(time.Time)
	BindControlFailure(func(error))
	ControlError() error
}

func (b *Bridge) bindControl() {
	if leg, ok := b.leg.(controlledLeg); ok {
		leg.BindControlFailure(b.controlFailed)
	}
}

func (b *Bridge) controlFailed(err error) {
	code, operation := b.allow(), "license_check"
	if code == "" {
		code, operation = protocol.CodeChannelInterrupted, "control_write"
	}
	// The failure coordinator already released WebSocket I/O. finish also
	// releases local TCP reads/writes, and once keeps any earlier first cause.
	b.finish(code, false, false, operation, err)
}

func (b *Bridge) closeDeadline(deadline time.Time) {
	if leg, ok := b.leg.(controlledLeg); ok {
		leg.SetCloseDeadline(deadline)
	}
}

func (b *Bridge) controlError() error {
	if leg, ok := b.leg.(controlledLeg); ok {
		return leg.ControlError()
	}
	return nil
}

// JoinCancelledClose is used after the caller's own cancellation close/drain
// has finished. A bridge force callback may have won that same deadline; it
// must also return before the forwarding call can relinquish the connection.
// Pending callbacks can be stopped here, never on the normal READY-error path
// where the original ABORT closing grace still has to run.
func (b *Bridge) JoinCancelledClose() {
	<-b.done
	if b.forceTimer == nil {
		return
	}
	if b.forceTimer.Stop() {
		b.forceDoneOnce.Do(func() { close(b.forceDone) })
	}
	<-b.forceDone
}
