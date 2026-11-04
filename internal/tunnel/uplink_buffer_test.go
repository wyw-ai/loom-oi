package tunnel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type uplinkPiece struct {
	data []byte
	err  error
}

type uplinkSource struct {
	*boundedBridgeConn
	pieces chan uplinkPiece
}

func (c *uplinkSource) Read(p []byte) (int, error) {
	c.reads.Add(1)
	select {
	case piece := <-c.pieces:
		return copy(p, piece.data), piece.err
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

type uplinkLeaseLeg struct {
	*fakeLeg
	leases  chan []byte
	release chan struct{}
	once    sync.Once
	fail    bool
}

func (l *uplinkLeaseLeg) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	if len(p) > 0 && p[0] == protocol.FrameData {
		l.leases <- p
		l.once.Do(func() {
			select {
			case <-l.release:
			case <-ctx.Done():
			}
		})
		if l.fail {
			return errors.New("write rejected after consuming borrowed frame")
		}
	}
	return l.fakeLeg.Write(ctx, typ, p)
}

func TestUplinkBufferWaitsForWriteBeforeReuse(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "completed"
		if fail {
			name = "write_failed"
		}
		t.Run(name, func(t *testing.T) {
			const f = 32 << 10
			blocks := [][]byte{bytes.Repeat([]byte{0x11}, f), bytes.Repeat([]byte{0xc2}, f), bytes.Repeat([]byte{0x7d}, 17)}
			base := &boundedBridgeConn{closed: make(chan struct{})}
			c := &uplinkSource{boundedBridgeConn: base, pieces: make(chan uplinkPiece, 3)}
			l := &uplinkLeaseLeg{fakeLeg: newLeg(), leases: make(chan []byte, 3), release: make(chan struct{}), fail: fail}
			t.Cleanup(func() { _ = c.Close(); _ = l.CloseNow() })
			t.Cleanup(func() {
				select {
				case <-l.release:
				default:
					close(l.release)
				}
			})
			c.pieces <- uplinkPiece{data: blocks[0]}
			c.pieces <- uplinkPiece{data: blocks[1]}
			c.pieces <- uplinkPiece{data: blocks[2], err: io.EOF}
			b := New(context.Background(), l, c, f, grace, nil)
			l.push(protocol.ReadyFrame())
			if err := b.WaitReady(); err != nil {
				t.Fatal(err)
			}
			l.push(protocol.FinFrame())
			result := run(b)
			var borrowed []byte
			select {
			case borrowed = <-l.leases:
			case <-b.Done():
				t.Fatal("bridge ended before first DATA write")
			case <-time.After(3 * time.Second):
				t.Fatal("first DATA write was never reached")
			}
			if c.reads.Load() != 1 || len(c.pieces) != 2 || !bytes.Equal(borrowed[1:], blocks[0]) || b.Buffered() != f {
				t.Fatal("borrowed frame overwritten or prefetched while Write blocked")
			}
			address := &borrowed[0]
			close(l.release)
			if fail {
				r := wait(t, result)
				if r.Code != protocol.CodeChannelInterrupted || r.Up != 0 || c.reads.Load() != 1 || b.Buffered() != 0 {
					t.Fatal("failed write retried or consumed next input", r, c.reads.Load())
				}
				return
			}
			for i, want := range blocks {
				frame := l.frame(t)
				if frame.Type != protocol.FrameData || !bytes.Equal(frame.Data, want) {
					t.Fatal("frame changed while buffer reused", i, frame.Type, len(frame.Data))
				}
				if i > 0 {
					if next := <-l.leases; &next[0] != address {
						t.Fatal("uplink still allocates a DATA frame per target Read")
					}
				}
			}
			if frame := l.frame(t); frame.Type != protocol.FrameFin {
				t.Fatal("n>0, EOF failed to send DATA before FIN", frame.Type)
			}
			waitBufferLatch(t, b.Done())
			l.in <- msg{err: io.EOF}
			r := wait(t, result)
			if r.Code != protocol.CodeCompleted || r.Up != 2*f+17 || r.Down != 0 || c.reads.Load() != 3 || b.Buffered() != 0 {
				t.Fatal("byte/FIN/cleanup accounting changed", r, c.reads.Load())
			}
		})
	}
}
