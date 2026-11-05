package websocket

import (
	"context"
	"errors"
)

func (c *Conn) writeControlPayload(context.Context, opcode, []byte) error {
	return errors.ErrUnsupported
}
