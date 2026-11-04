package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestTunnelMessageBufferSurvivesAdapterValueCopies(t *testing.T) {
	const f = 32 << 10
	for _, proto := range []string{protocol.TunnelSubprotocol, protocol.ControlSubprotocol} {
		t.Run(proto, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			s := &Server{cfg: Config{Limits: limits.Defaults()}}
			first, second := bytes.Repeat([]byte{0x31}, f+1), bytes.Repeat([]byte{0xb2}, f+1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, err := s.accept(w, req, proto, f+2)
				if err != nil {
					result <- err
					return
				}
				defer c.CloseNow()
				if (c.reader != nil) != (proto == protocol.TunnelSubprotocol) {
					result <- errors.New("reusable reader changed control handling or was not installed")
					return
				}
				text, one, err := c.Read(ctx)
				if err != nil || text || !bytes.Equal(one, first) {
					result <- fmt.Errorf("first message: %v", err)
					return
				}
				address := &one[0]
				copyOfAdapter := c
				text, two, err := copyOfAdapter.Read(ctx)
				if err != nil || !text || !bytes.Equal(two, second) {
					result <- fmt.Errorf("second message/type: %v", err)
					return
				}
				if c.reader != nil && &two[0] != address {
					result <- errors.New("value receiver lost reusable storage")
					return
				}
				_, _, err = c.Read(ctx)
				if !errors.Is(err, relay.ErrTooBig) {
					result <- fmt.Errorf("oversize mapping changed: %v", err)
					return
				}
				result <- nil
			}))
			defer srv.Close()
			c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{proto}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			if err = c.Write(ctx, websocket.MessageBinary, first); err != nil {
				t.Fatal(err)
			}
			if err = c.Write(ctx, websocket.MessageText, second); err != nil {
				t.Fatal(err)
			}
			if err = c.Write(ctx, websocket.MessageBinary, make([]byte, f+3)); err != nil {
				t.Fatal(err)
			}
			_, _, err = c.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
				t.Fatal("expected unchanged 1009 close", err)
			}
			if err = <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}
