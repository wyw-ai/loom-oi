// Package wsio provides bounded storage for sequential WebSocket messages.
package wsio

import (
	"context"
	"io"

	"github.com/coder/websocket"
)

// Source starts one message. Its message reader must retain the connection's
// existing read limit and control-frame handling, as websocket.Conn.Reader does.
type Source interface {
	Reader(context.Context) (websocket.MessageType, io.Reader, error)
}

// Reader owns one reusable message buffer. Read calls must be serialized, and
// the returned bytes are valid only until the next Read. It never prefetches a
// second message. max includes one byte to detect a message over the read limit.
type Reader struct {
	buf []byte
	max int
}

func NewReader(initialCapacity, readLimit int) *Reader {
	if initialCapacity <= 0 || readLimit < 0 || readLimit == int(^uint(0)>>1) {
		panic("invalid WebSocket message buffer bounds")
	}
	maximum := readLimit + 1
	return &Reader{buf: make([]byte, 0, min(initialCapacity, maximum)), max: maximum}
}

// Read retains the Source's message type and errors. Only a message body's EOF
// means a successfully finished message; failure to start a message is returned.
func (r *Reader) Read(ctx context.Context, source Source) (websocket.MessageType, []byte, error) {
	typ, body, err := source.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}
	r.buf = r.buf[:0]
	for {
		if len(r.buf) == cap(r.buf) {
			if cap(r.buf) == r.max {
				// Ask the real message reader to detect/close on its read limit,
				// preserving websocket.ErrMessageTooBig and close-frame behavior.
				var probe [1]byte
				_, err = body.Read(probe[:])
				if err == nil || err == io.EOF {
					err = websocket.ErrMessageTooBig
				}
				return typ, r.buf, err
			}
			next := cap(r.buf) + min(cap(r.buf), r.max-cap(r.buf))
			grown := make([]byte, len(r.buf), next)
			copy(grown, r.buf)
			r.buf = grown
		}
		n, readErr := body.Read(r.buf[len(r.buf):cap(r.buf)])
		r.buf = r.buf[:len(r.buf)+n]
		if readErr != nil {
			if readErr == io.EOF {
				if len(r.buf) == r.max {
					readErr = websocket.ErrMessageTooBig
				} else {
					readErr = nil
				}
			}
			return typ, r.buf, readErr
		}
	}
}
