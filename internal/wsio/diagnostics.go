package wsio

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/coder/websocket"
)

// ErrorClass is a bounded diagnostic label. It never includes a peer's close
// reason, addresses, payload, or the text of an arbitrary error.
func ErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, net.ErrClosed):
		return "closed"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	if websocket.CloseStatus(err) != -1 {
		return "websocket_close"
	}
	return "other"
}

// ContextState must be sampled before the caller cancels an operation's
// context for cleanup. It is an observation, never an admission decision.
func ContextState(ctx context.Context) string {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return "deadline"
	case context.Canceled:
		return "canceled"
	default:
		return "active"
	}
}

// unwrapLabel matches one exact outer wrapping layer from the pinned
// websocket v1.8.15. Do not search a peer-controlled leaf error for keywords.
func unwrapLabel(err error, label string) error {
	if err == nil {
		return nil
	}
	inner := errors.Unwrap(err)
	if inner != nil && err.Error() == label+inner.Error() {
		return inner
	}
	return nil
}

// PingPhase identifies only the pinned library's reported operation stage.
// control_write does not prove a socket Write started or that Ping closed it.
// Unknown wrapping remains unknown; the result cannot affect connection state.
func PingPhase(err error) string {
	inner := unwrapLabel(err, "failed to ping: ")
	if inner == nil {
		return "unknown"
	}
	if unwrapLabel(inner, "failed to wait for pong: ") != nil {
		return "pong_wait"
	}
	// This is the error returned directly by Ping's closed-channel select.
	if inner == net.ErrClosed {
		return "connection_closed"
	}
	// opcode's String representation is unexported in the pinned dependency.
	const prefix = "failed to write control frame opPing: "
	write := unwrapLabel(inner, prefix)
	if write == nil {
		return "unknown"
	}
	if unwrapLabel(write, "failed to acquire lock: ") != nil {
		return "write_lock"
	}
	if write == net.ErrClosed {
		return "connection_closed"
	}
	if unwrapLabel(write, "failed to write frame: ") != nil {
		return "control_write"
	}
	// A malformed wrapper is deliberately not treated as a known stage.
	return "unknown"
}
