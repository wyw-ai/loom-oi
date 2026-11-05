package websocket

import (
	"context"
	"errors"
)

// WritePing writes a single ping frame without waiting for a pong.
// The payload must be at most 125 bytes and remain unchanged until return.
// The context bounds the write, with no additional five-second timeout.
// On JavaScript platforms it returns errors.ErrUnsupported.
func (c *Conn) WritePing(ctx context.Context, payload []byte) error {
	return c.writeExplicitControl(ctx, opPing, payload)
}

// WritePong writes a single pong frame using the caller's context.
// The payload must be at most 125 bytes and remain unchanged until return.
// It does not install a timeout or retain the payload after return.
// On JavaScript platforms it returns errors.ErrUnsupported.
func (c *Conn) WritePong(ctx context.Context, payload []byte) error {
	return c.writeExplicitControl(ctx, opPong, payload)
}

func (c *Conn) writeExplicitControl(ctx context.Context, op opcode, payload []byte) error {
	if op != opPing && op != opPong {
		return errors.New("explicit control write requires ping or pong")
	}
	if len(payload) > 125 {
		return errors.New("explicit control payload exceeds 125 bytes")
	}
	return c.writeControlPayload(ctx, op, payload)
}
