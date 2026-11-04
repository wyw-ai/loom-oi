package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func bufferedStreamPeer(t *testing.T, typ websocket.MessageType, frames [][]byte, end bool) *Stream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		if c.Write(ctx, websocket.MessageBinary, protocol.ReadyFrame()) != nil {
			return
		}
		for _, frame := range frames {
			if c.Write(ctx, typ, frame) != nil {
				return
			}
		}
		if !end {
			for {
				if _, _, err = c.Read(ctx); err != nil {
					return
				}
			}
		}
	}))
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow(); cancel(); srv.Close() })
	c.SetReadLimit((1 << 20) + 3 + protocol.MaxAbortDetail)
	if err = waitReady(ctx, c, 32<<10); err != nil {
		t.Fatal(err)
	}
	return &Stream{leg: c, dataMax: 32 << 10, closeGrace: time.Second}
}

func TestStreamMessageBufferPreservesPendingAndPeerLimit(t *testing.T) {
	const f = 32 << 10
	blocks := [][]byte{bytes.Repeat([]byte{0x31}, f), bytes.Repeat([]byte{0xd6}, f), bytes.Repeat([]byte{0x7b}, 1<<20)}
	frames := [][]byte{protocol.DataFrame(blocks[0]), protocol.DataFrame(blocks[1]), protocol.DataFrame(blocks[2]), protocol.FinFrame()}
	s := bufferedStreamPeer(t, websocket.MessageBinary, frames, false)
	var pendingAddress *byte
	for i, want := range blocks {
		var first [1]byte
		if n, err := s.Read(first[:]); n != 1 || err != nil || first[0] != want[0] {
			t.Fatal(i, n, err)
		}
		if len(s.pending) != len(want)-1 || !bytes.Equal(s.pending, want[1:]) {
			t.Fatal("next frame overwrote unconsumed payload", i, len(s.pending))
		}
		if i == 0 {
			pendingAddress = &s.pending[0]
		} else if i == 1 && pendingAddress != &s.pending[0] {
			t.Fatal("normal maximum frame did not reuse Stream's buffer")
		}
		rest := make([]byte, len(want)-1)
		if _, err := io.ReadFull(s, rest); err != nil || !bytes.Equal(rest, want[1:]) {
			t.Fatal("partial reads changed payload", i, err)
		}
	}
	if n, err := s.Read(make([]byte, 7)); n != 0 || err != io.EOF {
		t.Fatal("FIN changed", n, err)
	}
}

func TestStreamMessageBufferKeepsTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  websocket.MessageType
		data []byte
		end  bool
		code protocol.Code
	}{
		{"abort", websocket.MessageBinary, protocol.AbortFrame(protocol.CodeRevoked, "revoked"), false, protocol.CodeRevoked},
		{"text", websocket.MessageText, []byte("not a tunnel frame"), false, protocol.CodeProtocolError},
		{"malformed_fin", websocket.MessageBinary, []byte{protocol.FrameFin, 1}, false, protocol.CodeProtocolError},
		{"empty", websocket.MessageBinary, nil, false, protocol.CodeProtocolError},
		{"data_over_peer_limit", websocket.MessageBinary, protocol.DataFrame(make([]byte, (1<<20)+1)), false, protocol.CodeProtocolError},
		{"message_over_read_limit", websocket.MessageBinary, protocol.DataFrame(make([]byte, (1<<20)+132)), false, protocol.CodeChannelInterrupted},
		{"eof_without_fin", websocket.MessageBinary, nil, true, protocol.CodeChannelInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := [][]byte{tc.data}
			if tc.end {
				frames = nil
			}
			s := bufferedStreamPeer(t, tc.typ, frames, tc.end)
			_, err := io.Copy(io.Discard, s)
			var ae *AbortError
			if !errors.As(err, &ae) || ae.Code != tc.code {
				t.Fatal("terminal classification changed", err, tc.code)
			}
			if _, again := s.Read(make([]byte, 1)); again != err {
				t.Fatal("terminal error was not sticky", again, err)
			}
		})
	}
}
