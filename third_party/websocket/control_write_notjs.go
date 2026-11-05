//go:build !js

package websocket

import (
	"context"
	"fmt"
)

func (c *Conn) writeControlPayload(ctx context.Context, op opcode, payload []byte) error {
	_, err := c.writeFrame(ctx, true, false, op, payload)
	if err != nil {
		return fmt.Errorf("failed to write control frame %v: %w", op, err)
	}
	return nil
}
