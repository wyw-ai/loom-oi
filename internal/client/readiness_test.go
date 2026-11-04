package client

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestReadinessAbortReachesConsumer(t *testing.T) {
	for _, code := range []protocol.Code{protocol.CodeReadinessRequired, protocol.CodeReadinessCheckFailed} {
		for _, ready := range []bool{false, true} {
			t.Run(string(code)+map[bool]string{false: "/before_READY", true: "/after_READY"}[ready], func(t *testing.T) {
				done := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(done)
					c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.TunnelSubprotocol}})
					if err != nil {
						return
					}
					defer c.CloseNow()
					if ready {
						_ = c.Write(r.Context(), websocket.MessageBinary, protocol.ReadyFrame())
					}
					_ = c.Write(r.Context(), websocket.MessageBinary, protocol.AbortFrame(code, "observation_changed"))
					_, _, _ = c.Read(r.Context())
				}))
				defer srv.Close()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				leg, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http:", "ws:", 1), &websocket.DialOptions{Subprotocols: []string{protocol.TunnelSubprotocol}})
				if err != nil {
					t.Fatal(err)
				}
				defer leg.CloseNow()
				err = waitReady(ctx, leg, 32768)
				if ready {
					if err != nil {
						t.Fatal(err)
					}
					s := &Stream{leg: leg, dataMax: 32768, closeGrace: time.Millisecond}
					buf := make([]byte, 1)
					n, rerr := s.Read(buf)
					if n != 0 {
						t.Fatal("payload on rejection")
					}
					err = rerr
				}
				var ae *AbortError
				if !errors.As(err, &ae) || ae.Code != code || ae.Detail != "observation_changed" {
					t.Fatalf("want %s, got %v", code, err)
				}
				_ = leg.CloseNow()
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("peer did not exit")
				}
			})
		}
	}
}

func TestReadinessHTTPErrorReason(t *testing.T) {
	err := apiError(503, []byte(`{"code":"ReadinessCheckFailed","reason":"timeout","message":"deadline"}`))
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeReadinessCheckFailed || pe.Reason != "timeout" {
		t.Fatal(err)
	}
	// The CLI emits the same bounded reason for a real HTTPS API rejection.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(protocol.ErrorBody{Code: protocol.CodeReadinessCheckFailed, Reason: "unavailable"})
	}))
	defer srv.Close()
	// Trust only this test server's self-signed certificate.
	caFile := t.TempDir() + "/server.pem"
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	t.Setenv("OI_TOKEN", "test")
	status := Main(context.Background(), []string{"--ois", srv.URL, "--ca", caFile, "discover"}, strings.NewReader(""), &out, &stderr)
	var body protocol.ErrorBody
	if status != 1 || json.Unmarshal(stderr.Bytes(), &body) != nil || body.Code != protocol.CodeReadinessCheckFailed || body.Reason != "unavailable" {
		t.Fatalf("status=%d stderr=%q", status, stderr.String())
	}
}
