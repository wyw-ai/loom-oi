package websocket

import (
	"context"
	"errors"
	"testing"
)

func TestExplicitControlUnsupportedJS(t *testing.T) {
	var c Conn
	for _, tc := range []struct {
		name string
		call func(context.Context, []byte) error
	}{
		{"ping", c.WritePing}, {"pong", c.WritePong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []int{0, 8, 125} {
				if err := tc.call(context.Background(), make([]byte, n)); !errors.Is(err, errors.ErrUnsupported) {
					t.Fatalf("size %d: %v", n, err)
				}
			}
			if err := tc.call(context.Background(), make([]byte, 126)); err == nil || errors.Is(err, errors.ErrUnsupported) {
				t.Fatalf("invalid length did not fail validation first: %v", err)
			}
		})
	}
	t.Run("invalid_opcode", func(t *testing.T) {
		if err := c.writeExplicitControl(context.Background(), opClose, nil); err == nil || errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("invalid opcode: %v", err)
		}
	})
}
