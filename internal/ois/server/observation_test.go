package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestObservationConfig(t *testing.T) {
	ca := must(pki.NewCA("g2"))
	dir := t.TempDir()
	for name, extra := range map[string]map[string]any{
		"default":           {},
		"loopback_disabled": {"diag_listen": "127.0.0.1:0"},
		"ipv6":              {"diag_listen": "[::1]:0", "metrics_enabled": true},
		"smaller":           {"observation_limits": map[string]any{"pending_checks": 2, "query_slots": 1, "check_ms": 200}},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := LoadConfig(writeConfig(t, dir, ca, extra))
			if err != nil {
				t.Fatal(err)
			}
			if c.ObservationLimits.MessageMax != 4096 || c.ObservationLimits.WriteMS != 100 {
				t.Fatal(c.ObservationLimits)
			}
		})
	}
	for name, extra := range map[string]map[string]any{
		"no_loopback":   {"metrics_enabled": true},
		"public":        {"diag_listen": "0.0.0.0:8080"},
		"hostname":      {"diag_listen": "localhost:8080"},
		"bad_address":   {"diag_listen": "127.0.0.1"},
		"zone":          {"diag_listen": "[::1%1]:8080"},
		"bad_port":      {"diag_listen": "127.0.0.1:no"},
		"negative_port": {"diag_listen": "127.0.0.1:-1"},
		"large_port":    {"diag_listen": "127.0.0.1:65536"},
		"zero_budget":   {"observation_limits": map[string]any{"check_ms": 0}},
		"null_field":    {"observation_limits": map[string]any{"check_ms": nil}},
		"unknown_field": {"observation_limits": map[string]any{"infinite": true}},
		"bad_type":      {"observation_limits": false},
		"excessive":     {"observation_limits": map[string]any{"query_slots": 65}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, dir, ca, extra)); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	path := writeConfig(t, dir, ca, nil)
	b := must(os.ReadFile(path))
	b = []byte(strings.Replace(string(b), `"api_listen":`, `"observation_limits":null,"api_listen":`, 1))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("null observation accepted")
	}
	if _, err := Start(context.Background(), Config{DiagListen: "0.0.0.0:1"}, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("programmatic public diagnostic listener")
	}
}

func TestMetricsLoopbackServer(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			ca := must(pki.NewCA("g2-metrics"))
			dir := t.TempDir()
			path := writeConfig(t, dir, ca, map[string]any{"diag_listen": "127.0.0.1:0", "metrics_enabled": enabled})
			if code, _, stderr := runMain("init", "--config", path); code != 0 {
				t.Fatal(stderr)
			}
			c := must(LoadConfig(path))
			s := must(Start(context.Background(), c, slog.New(slog.DiscardHandler)))
			client := &http.Client{Timeout: time.Second}
			defer client.CloseIdleConnections()
			stopped := false
			t.Cleanup(func() {
				if stopped {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				s.Shutdown(ctx)
			})
			if s.DiagAddr() == "" {
				t.Fatal("no explicit loopback listener")
			}
			resp := must(client.Get("http://" + s.DiagAddr() + "/metrics"))
			body := must(io.ReadAll(resp.Body))
			resp.Body.Close()
			if !enabled {
				if resp.StatusCode != 404 {
					t.Fatal(resp.StatusCode)
				}
				return
			}
			if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != metrics.ContentType || len(body) > metrics.MaxResponseBytes {
				t.Fatalf("status=%d headers=%v bytes=%d", resp.StatusCode, resp.Header, len(body))
			}
			resp = must(client.Get("http://" + s.DiagAddr() + "/v1/admin/stats"))
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Fatal("private stats exposed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s.Shutdown(ctx)
			stopped = true
			if err := ctx.Err(); err != nil {
				t.Fatal("diagnostic handlers did not finish before shutdown", err)
			}
			// The response can be read before Flush returns to the handler.
			// Graceful shutdown joins handlers before observing their final counter.
			if count, ok := s.metrics.Snapshot().Counter(metrics.Exports, "success"); !ok || count != 1 {
				t.Fatal(count, ok)
			}
			if conn, err := net.DialTimeout("tcp", s.DiagAddr(), time.Second); err == nil {
				conn.Close()
				t.Fatal("diagnostic listener survived shutdown")
			}
		})
	}
	s := newTestServer(t)
	if s.srv.DiagAddr() != "" {
		t.Fatal("diagnostic default enabled")
	}
}

func TestControlVersionNegotiation(t *testing.T) {
	for _, tc := range []struct {
		offer, want string
		code        protocol.Code
	}{
		{protocol.ControlSubprotocol, protocol.ControlSubprotocol, ""},
		{protocol.ControlSubprotocolV2, protocol.ControlSubprotocolV2, ""},
		{protocol.ControlSubprotocol + ", " + protocol.ControlSubprotocolV2, protocol.ControlSubprotocolV2, ""},
		{"oi.control.v0", protocol.ControlSubprotocol, protocol.CodeVersionUnsupported},
		{"", protocol.ControlSubprotocol, protocol.CodeVersionUnsupported},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://loopback/v1/access/control", nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Protocol", tc.offer)
		got, err := controlVersion(r)
		if got != tc.want || (tc.code == "" && err != nil) || (tc.code != "" && protocol.CodeOf(err) != tc.code) {
			t.Fatal(tc, got, err)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "http://loopback/v1/access/control", nil)
	if _, err := controlVersion(r); protocol.CodeOf(err) != protocol.CodeInvalidRequest {
		t.Fatal(err)
	}
}

func TestControlV2Handshake(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := w.dialWS(w.oicClient(w.leaf), "wss://"+w.srv.AccessAddr()+"/v1/access/control", nil, protocol.ControlSubprotocolV2)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != protocol.ControlSubprotocolV2 {
		t.Fatal(c.Subprotocol())
	}
	if err := c.Write(ctx, websocket.MessageText, protocol.EncodeControl(&protocol.Hello{}, 1)); err != nil {
		t.Fatal(err)
	}
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.DecodeControlForVersion(b, protocol.ControlSubprotocolV2)
	if err != nil {
		t.Fatal(err, string(b))
	}
	welcome, ok := m.(*protocol.Welcome)
	if !ok || typ != websocket.MessageText || welcome.Generation != 2 || welcome.ObservationLimits == nil || welcome.ObservationLimits.MessageMax != 4096 {
		t.Fatalf("%+v", m)
	}
}

func TestMetricsListenerFailureClosesProductListeners(t *testing.T) {
	ca := must(pki.NewCA("g2-busy"))
	dir := t.TempDir()
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	defer ln.Close()
	path := writeConfig(t, dir, ca, map[string]any{"diag_listen": ln.Addr().String(), "metrics_enabled": true})
	if code, _, stderr := runMain("init", "--config", path); code != 0 {
		t.Fatal(stderr)
	}
	c := must(LoadConfig(path))
	if _, err := Start(context.Background(), c, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("busy diag accepted")
	}
	// The failed start also released its store lock and hub workers.
	c.DiagListen = "127.0.0.1:0"
	s := must(Start(context.Background(), c, slog.New(slog.DiscardHandler)))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.Shutdown(ctx)
}

func TestReadinessErrorReasonHTTP(t *testing.T) {
	w := httptest.NewRecorder()
	s := &Server{log: slog.New(slog.DiscardHandler)}
	s.writeError(w, &protocol.Error{Code: protocol.CodeReadinessCheckFailed, Reason: "timeout", Message: "check expired"})
	var body protocol.ErrorBody
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Reason != "timeout" || body.Code != protocol.CodeReadinessCheckFailed || w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
}
