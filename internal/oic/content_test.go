package oic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func previewFixture(t *testing.T, handshake string) (*Host, string) {
	t.Helper()
	h, path, _ := newFixture(t, map[string]any{"format_handshake": handshake})
	d := fixtureDefinition(t)
	d.Actions["preview"] = ActionDefinition{
		Description: "preview", Handler: "preview", ResultFormat: protocol.FormatContent,
		InputSchema: map[string]any{"type": "object"},
	}
	writeYAML(t, path, d)
	return h, path
}

func TestMixedFormatWorkerRegistrationAndDispatch(t *testing.T) {
	h, path := previewFixture(t, "accept")
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	h.mu.RLock()
	legacy := h.registrationLocked(nil)
	h.mu.RUnlock()
	if !legacy.Apps[0].Ready || !h.Apps()[0].Ready {
		t.Fatal("peer capability changed App readiness")
	}
	for _, a := range legacy.Apps[0].Actions {
		if a.Ready != (a.Name != "preview") {
			t.Fatalf("unexpected per-action readiness: %+v", a)
		}
	}
	ctx := context.Background()
	frame := fixtureFrame("image", "preview")
	if result := h.Invoke(ctx, frame); result.Type != "result" || result.ResultFormat != protocol.FormatContent {
		t.Fatal("failed to deliver declared format", result)
	}
	for _, id := range []string{"image", "new"} {
		frame.CallID = id
		result := h.invoke(ctx, frame, nil, nil)
		if result.Error == nil || result.Error.Code != "RESULT_FORMAT_UNSUPPORTED" {
			t.Fatal("a JSON-only peer received an image or cached image", result)
		}
	}
	resultMap(t, h.invoke(ctx, fixtureFrame("text", "echo"), nil, nil))
	if result := h.Invoke(ctx, fixtureFrame("image", "preview")); result.ResultFormat != protocol.FormatContent {
		t.Fatal("completed response did not retain format")
	}
}

func TestWorkerRejectsInvalidReadyNegotiation(t *testing.T) {
	for _, handshake := range []string{"downgrade", "overclaim"} {
		t.Run(handshake, func(t *testing.T) {
			h, path := previewFixture(t, handshake)
			if _, err := h.Load(path); err == nil || len(h.Apps()) != 0 {
				t.Fatal("incompatible worker was loaded", err)
			}
		})
	}
}

func TestWorkerReceivesEmptyObjectForParameterlessAction(t *testing.T) {
	h, path, _ := newFixture(t, nil)
	d := fixtureDefinition(t)
	d.Actions["empty"] = ActionDefinition{Description: "No parameters", Handler: "echo", InputSchema: map[string]any{"type": "object"}}
	writeYAML(t, path, d)
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	f := fixtureFrame("empty", "empty")
	f.Input = nil
	result := resultMap(t, h.Invoke(context.Background(), f))
	value, ok := result["input"].(map[string]any)
	if !ok || len(value) != 0 {
		t.Fatalf("worker received %v, expected {}", result["input"])
	}
}

func TestReconnectRenegotiatesBeforeReplayingCachedContent(t *testing.T) {
	h, path := previewFixture(t, "accept")
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OIC_TEST_TOKEN", "fixture")
	var connections atomic.Int32
	results := make(chan error, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			results <- err
			return
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		n := connections.Add(1)
		exchange := func() error {
			var bind protocol.Frame
			if err := ws.ReadJSON(&bind); err != nil {
				return err
			}
			if bind.Type != "bind" || !protocol.SupportsFormat(bind.ResultFormats, protocol.FormatContent) {
				return fmt.Errorf("missing bind capabilities: %+v", bind)
			}
			bound := protocol.Frame{Type: "bound", Protocol: 1}
			if n == 1 {
				bound.ResultFormats = bind.ResultFormats
			}
			if err := ws.WriteJSON(bound); err != nil {
				return err
			}
			var registration protocol.Frame
			if err := ws.ReadJSON(&registration); err != nil {
				return err
			}
			if len(registration.Apps) != 1 || !registration.Apps[0].Ready {
				return fmt.Errorf("invalid registration: %+v", registration)
			}
			for _, action := range registration.Apps[0].Actions {
				if action.Name == "preview" && (action.ResultFormat != protocol.FormatContent || action.Ready != (n == 1)) {
					return fmt.Errorf("invalid image registration on connection %d: %+v", n, action)
				}
			}
			if err := ws.WriteJSON(protocol.Frame{Type: "registered"}); err != nil {
				return err
			}
			for _, name := range []string{"preview", "echo"} {
				if err := ws.WriteJSON(fixtureFrame("same-"+name, name)); err != nil {
					return err
				}
				var result protocol.Frame
				if err := ws.ReadJSON(&result); err != nil {
					return err
				}
				if name == "preview" && n == 2 {
					if result.Error == nil || result.Error.Code != "RESULT_FORMAT_UNSUPPORTED" {
						return fmt.Errorf("old connection received cached preview: %+v", result)
					}
				} else if result.Type != "result" || (name == "preview" && result.ResultFormat != protocol.FormatContent) {
					return fmt.Errorf("invalid result on connection %d: %+v", n, result)
				}
			}
			return nil
		}
		results <- exchange()
	}))
	defer server.Close()
	h.config.OIS.URL = strings.Replace(server.URL, "http", "ws", 1)
	for i := 0; i < 2; i++ {
		_ = h.connect() // test peer closes at the end of each exchange
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
