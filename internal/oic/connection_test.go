package oic

import (
	"context"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestChannelBindingRegistrationHeartbeatInvocationAndReconnect(t *testing.T) {
	for _, tc := range []struct {
		scheme string
		token  bool
	}{
		{"http", false}, {"ws", false}, {"https", true}, {"wss", false},
	} {
		t.Run(tc.scheme, func(t *testing.T) { testChannelLifecycle(t, tc.scheme, tc.token) })
	}
}

func testChannelLifecycle(t *testing.T, scheme string, withToken bool) {
	t.Helper()
	t.Setenv("OIC_TEST_TOKEN", "channel-fixture-token")
	expectedAuthorization := ""
	if withToken {
		expectedAuthorization = "Bearer channel-fixture-token"
	}
	var attempts atomic.Int32
	completed := make(chan int, 4)
	failures := make(chan error, 4)
	upgrader := websocket.Upgrader{}
	handler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if _, present := r.Header["Authorization"]; !withToken && present {
			failures <- fmt.Errorf("anonymous request unexpectedly included Authorization")
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v1/oic/connect" || r.Header.Get("Authorization") != expectedAuthorization {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		ws, err := upgrader.Upgrade(rw, r, nil)
		if err != nil {
			failures <- err
			return
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		read := func() (protocol.Frame, error) { var f protocol.Frame; err := ws.ReadJSON(&f); return f, err }
		f, err := read()
		if err != nil || f.Type != "bind" || f.HostID != "fixture-host" || f.ChannelID != "fixture-channel" {
			failures <- fmt.Errorf("bad bind: %+v %v", f, err)
			return
		}
		_ = ws.WriteJSON(protocol.Frame{Type: "bound"})
		f, err = read()
		if err != nil || f.Type != "register" || len(f.Apps) != 1 || !f.Apps[0].Ready || f.Host == nil || f.Host.ID != "fixture-host" {
			failures <- fmt.Errorf("bad registration: %+v %v", f, err)
			return
		}
		_ = ws.WriteJSON(protocol.Frame{Type: "registered"})
		_ = ws.WriteJSON(protocol.Frame{Type: "ping"})
		f, err = read()
		if err != nil || f.Type != "pong" {
			failures <- fmt.Errorf("bad heartbeat: %+v %v", f, err)
			return
		}
		n := int(attempts.Add(1))
		invoke := fixtureFrame(fmt.Sprintf("wire-%d", n), "echo")
		_ = ws.WriteJSON(invoke)
		f, err = read()
		if err != nil || f.Type != "result" || f.CallID != invoke.CallID {
			failures <- fmt.Errorf("bad invoke response: %+v %v", f, err)
			return
		}
		completed <- n
		if n == 1 {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	var server *httptest.Server
	if scheme == "https" || scheme == "wss" {
		server = httptest.NewTLSServer(handler)
	} else {
		server = httptest.NewServer(handler)
	}
	defer server.Close()
	h, path, _ := newFixture(t, nil)
	if scheme == "https" || scheme == "wss" {
		caPath := filepath.Join(filepath.Dir(path), "ca.pem")
		if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		h.config.OIS.CAFile = caPath
	}
	h.config.OIS.URL = server.URL
	if scheme == "ws" || scheme == "wss" {
		h.config.OIS.URL = strings.Replace(server.URL, "http", "ws", 1) + "/api/v1/oic/connect"
	}
	if !withToken {
		h.config.OIS.CredentialEnv = ""
	}
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	defer cancel()
	for want := 1; want <= 2; want++ {
		select {
		case n := <-completed:
			if n != want {
				t.Fatalf("unexpected attempt %d", n)
			}
		case err := <-failures:
			t.Fatal(err)
		case <-time.After(8 * time.Second):
			t.Fatal("channel operation timed out", h.Status())
		}
	}
	eventually(t, func() bool { return h.Status().Connected && h.Apps()[0].Registered })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Host did not stop")
	}
	if h.Status().Connected || h.Status().Running {
		t.Fatal("closed Host still reports active")
	}
}

func TestDialConfigKeepsTLSValidationAndRejectsCAOnHTTP(t *testing.T) {
	h, _, _ := newFixture(t, nil)
	h.config.OIS.URL = "http://127.0.0.1:25100"
	endpoint, tlsConfig, err := h.dialConfig()
	if err != nil || endpoint != "ws://127.0.0.1:25100/api/v1/oic/connect" || tlsConfig != nil {
		t.Fatalf("bad HTTP configuration: %q %#v %v", endpoint, tlsConfig, err)
	}
	h.config.OIS.CAFile = "unnecessary.pem"
	if _, _, err := h.dialConfig(); err == nil || !strings.Contains(err.Error(), "ois.ca_file") {
		t.Fatal("HTTP silently ignored a CA configuration")
	}
	h.config.OIS.CAFile = ""
	h.config.OIS.URL = "https://127.0.0.1:25100"
	endpoint, tlsConfig, err = h.dialConfig()
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "wss://127.0.0.1:25100/api/v1/oic/connect" || tlsConfig.InsecureSkipVerify || tlsConfig.RootCAs != nil {
		t.Fatal("TLS or endpoint defaults incorrect")
	}
	badCA := filepath.Join(h.StateDir(), "bad.pem")
	if err = os.WriteFile(badCA, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	h.config.OIS.CAFile = badCA
	if _, _, err = h.dialConfig(); err == nil {
		t.Fatal("invalid explicit CA accepted")
	}
}

func TestOptionalCredentialConfigurationAndExplicitMissingEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oic.yaml")
	definition := map[string]any{"version": 1, "host": map[string]any{"id": "fixture-host"}, "channel": map[string]any{"id": "fixture-channel"}, "ois": map[string]any{"url": "http://127.0.0.1:25100"}}
	writeYAML(t, path, definition)
	c, _, err := readConfig(path)
	if err != nil || c.OIS.CredentialEnv != "" || c.OIS.CAFile != "" {
		t.Fatalf("optional credentials/CA rejected: %+v %v", c, err)
	}
	definition["ois"].(map[string]any)["ca_file"] = "missing.pem"
	writeYAML(t, path, definition)
	if _, _, err := readConfig(path); err == nil || !strings.Contains(err.Error(), "ois.ca_file") {
		t.Fatal("invalid HTTP/CA combination was not rejected at load")
	}
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS=%t", secure), func(t *testing.T) {
			var requests atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusUnauthorized) })
			var server *httptest.Server
			if secure {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}
			defer server.Close()
			h, appPath, _ := newFixture(t, nil)
			h.config.OIS.URL = server.URL
			if secure {
				ca := filepath.Join(filepath.Dir(appPath), "ca.pem")
				if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
					t.Fatal(err)
				}
				h.config.OIS.CAFile = ca
			}
			for _, value := range []string{"", "  "} {
				t.Setenv("OIC_TEST_TOKEN", value)
				if err := h.connect(); err == nil || !strings.Contains(err.Error(), "credential environment variable") {
					t.Fatalf("missing explicit token was not rejected: %v", err)
				}
			}
			if err := os.Unsetenv("OIC_TEST_TOKEN"); err != nil {
				t.Fatal(err)
			}
			if err := h.connect(); err == nil || !strings.Contains(err.Error(), "credential environment variable") {
				t.Fatalf("unset explicit token was not rejected: %v", err)
			}
			if requests.Load() != 0 {
				t.Fatal("configured missing credentials fell back to an anonymous network request")
			}
		})
	}
}

func TestHTTPSWithoutCustomCAStillRejectsUntrustedCertificate(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	h, _, _ := newFixture(t, nil)
	h.config.OIS.URL = server.URL
	h.config.OIS.CredentialEnv = ""
	if err := h.connect(); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("untrusted certificate was accepted or failed unexpectedly: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("HTTP request reached server despite untrusted certificate")
	}
}

func TestAnonymousConnectionPreservesBindingRejection(t *testing.T) {
	for _, code := range []string{"HOST_MISMATCH", "CHANNEL_CONNECTED"} {
		t.Run(code, func(t *testing.T) {
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				var f protocol.Frame
				if err = ws.ReadJSON(&f); err != nil {
					return
				}
				if f.Type != "bind" || f.HostID != "fixture-host" || f.ChannelID != "fixture-channel" {
					t.Error("anonymous mode omitted fixed binding identity")
				}
				_ = ws.WriteJSON(protocol.Frame{Type: "error", Error: protocol.Err(code, "binding refused")})
			}))
			defer server.Close()
			h, _, _ := newFixture(t, nil)
			h.config.OIS.URL = server.URL
			h.config.OIS.CredentialEnv = ""
			if err := h.connect(); err == nil || !strings.Contains(err.Error(), code) {
				t.Fatalf("binding rejection was ignored: %v", err)
			}
			if h.Status().Connected {
				t.Fatal("rejected anonymous connection was marked connected")
			}
		})
	}
}
