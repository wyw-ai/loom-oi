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

// ReadPhase reports only the pinned library's preserved read-operation wrappers.
// It cannot prove that a socket operation began or caused a transport closure.
// In particular, finishRead can replace a header/payload error with ctx.Err;
// without the control-frame wrapper that loss of information stays unknown.
func ReadPhase(err error) string {
	inner := unwrapLabel(err, "failed to get reader: ")
	if inner == nil {
		inner = unwrapLabel(err, "failed to read: ")
	}
	if inner == nil {
		return "unknown"
	}
	for _, opcode := range [...]string{"opPing", "opPong", "opClose"} {
		control := unwrapLabel(inner, "failed to handle control frame "+opcode+": ")
		if control == nil {
			continue
		}
		if opcode == "opPing" {
			pong := unwrapLabel(control, "failed to write control frame opPong: ")
			if pong != nil {
				if unwrapLabel(pong, "failed to acquire lock: ") != nil {
					return "pong_write_lock"
				}
				if unwrapLabel(pong, "failed to write frame: ") != nil {
					return "pong_control_write"
				}
				return "unknown"
			}
		}
		if unwrapLabel(control, "failed to read frame payload: ") != nil ||
			control == context.DeadlineExceeded || control == context.Canceled || control == net.ErrClosed {
			return "control_payload"
		}
		return "unknown"
	}
	if unwrapLabel(inner, "failed to read frame header: ") != nil {
		return "frame_header"
	}
	if unwrapLabel(inner, "failed to read frame payload: ") != nil {
		return "frame_payload"
	}
	return "unknown"
}
