package relay

import (
	"context"
	"errors"
)

// Conn is the part of a WebSocket connection the hub uses. The server
// adapts github.com/coder/websocket; unit tests use in-memory fakes.
type Conn interface {
	// Read returns the next message and whether it is a text message. It
	// returns ErrTooBig when a message exceeds the connection's read limit.
	Read(ctx context.Context) (text bool, data []byte, err error)
	Write(ctx context.Context, text bool, data []byte) error
	// Ping sends a ping and waits for the pong.
	Ping(ctx context.Context) error
	// Close performs the closing handshake with a normal status.
	Close(reason string) error
	// CloseNow drops the connection without a handshake.
	CloseNow() error
}

// ErrTooBig reports a message above the connection's read limit.
var ErrTooBig = errors.New("message exceeds the read limit")
