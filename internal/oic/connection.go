package oic

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type connection struct {
	ws            *websocket.Conn
	formats       []string
	mu            sync.Mutex
	closed        chan struct{}
	once          sync.Once
	regMu         sync.Mutex
	registrations []map[string]*loadedApp
}

func (p *connection) close() { p.once.Do(func() { close(p.closed); _ = p.ws.Close() }) }
func (p *connection) write(f protocol.Frame) error {
	message, err := protocol.MarshalFrame(f, false)
	if err != nil {
		if f.Type != "result" && f.Type != "error" {
			return err
		}
		message, err = protocol.MarshalFrame(workerError(f.CallID, "RESULT_TOO_LARGE", "result exceeds the channel frame limit"), false)
		if err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.ws.WriteMessage(websocket.TextMessage, message)
}
func (p *connection) register(f protocol.Frame, snapshot map[string]*loadedApp) error {
	message, err := protocol.MarshalFrame(f, false)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.regMu.Lock()
	p.registrations = append(p.registrations, snapshot)
	p.regMu.Unlock()
	_ = p.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.ws.WriteMessage(websocket.TextMessage, message)
}
func (p *connection) registered() map[string]*loadedApp {
	p.regMu.Lock()
	defer p.regMu.Unlock()
	if len(p.registrations) == 0 {
		return nil
	}
	first := p.registrations[0]
	p.registrations = p.registrations[1:]
	return first
}

func (h *Host) Run(ctx context.Context) error {
	h.mu.Lock()
	if h.started || h.closed {
		h.mu.Unlock()
		return fmt.Errorf("Host can only be run once")
	}
	h.started = true
	h.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			h.cancel()
		case <-h.ctx.Done():
		}
	}()
	defer h.Close(context.Background())
	h.restore()
	for h.ctx.Err() == nil {
		err := h.connect()
		if h.ctx.Err() != nil {
			break
		}
		h.mu.Lock()
		if err != nil {
			h.lastError = err.Error()
		}
		h.mu.Unlock()
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-h.ctx.Done():
			timer.Stop()
		}
	}
	return nil
}

func connectionEndpoint(rawURL, caFile string) (string, bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false, err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", false, fmt.Errorf("ois.url must use HTTP, HTTPS, WS or WSS")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false, fmt.Errorf("ois.url must be an absolute endpoint without credentials, query or fragment")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/v1/oic/connect"
	}
	secure := u.Scheme == "wss"
	if !secure && caFile != "" {
		return "", false, fmt.Errorf("ois.ca_file is only valid with HTTPS/WSS; remove it when using HTTP/WS")
	}
	return u.String(), secure, nil
}

func (h *Host) dialConfig() (string, *tls.Config, error) {
	endpoint, secure, err := connectionEndpoint(h.config.OIS.URL, h.config.OIS.CAFile)
	if err != nil {
		return "", nil, err
	}
	if !secure {
		return endpoint, nil, nil
	}
	// A nil RootCAs uses Go's normal system trust roots. An explicit CA adds
	// trust anchors without disabling certificate name or chain verification.
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if h.config.OIS.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		pem, err := os.ReadFile(h.config.OIS.CAFile)
		if err != nil {
			return "", nil, err
		}
		if !roots.AppendCertsFromPEM(pem) {
			return "", nil, fmt.Errorf("ois.ca_file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	return endpoint, tlsConfig, nil
}

func (h *Host) authorizationHeaders() (http.Header, error) {
	headers := make(http.Header)
	if name := h.config.OIS.CredentialEnv; name != "" {
		token := os.Getenv(name)
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("configured OIC credential environment variable %s is not set or empty", name)
		}
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers, nil
}

func (h *Host) connect() error {
	endpoint, tlsConfig, err := h.dialConfig()
	if err != nil {
		return err
	}
	headers, err := h.authorizationHeaders()
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 10 * time.Second, TLSClientConfig: tlsConfig}
	ws, response, err := dialer.DialContext(h.ctx, endpoint, headers)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("OIS connection: %w", err)
	}
	p := &connection{ws: ws, closed: make(chan struct{})}
	defer p.close()
	go func() {
		select {
		case <-h.ctx.Done():
			p.close()
		case <-p.closed:
		}
	}()
	ws.SetReadLimit(protocol.MaxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err = p.write(protocol.Frame{Type: "bind", Protocol: protocol.Version, ResultFormats: []string{protocol.FormatJSON, protocol.FormatContent}, HostID: h.config.Host.ID, ChannelID: h.config.Channel.ID}); err != nil {
		return err
	}
	first, err := readFrame(ws)
	if err != nil {
		return err
	}
	if first.Type != "bound" {
		if first.Error != nil {
			return first.Error
		}
		return fmt.Errorf("OIS did not accept channel binding")
	}
	if first.Protocol != 0 && first.Protocol != protocol.Version {
		return fmt.Errorf("OIS did not negotiate protocol %d", protocol.Version)
	}
	if (first.HostID != "" && first.HostID != h.config.Host.ID) || (first.ChannelID != "" && first.ChannelID != h.config.Channel.ID) {
		return fmt.Errorf("OIS bound a different Host or channel")
	}
	p.formats = protocol.NegotiateFormats(first.ResultFormats)
	if len(p.formats) == 0 {
		return protocol.Err("RESULT_FORMAT_UNSUPPORTED", "no common result format")
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return context.Canceled
	}
	h.peer = p
	h.lastError = ""
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.peer == p {
			h.peer = nil
			for _, a := range h.apps {
				a.registered = false
			}
		}
		h.mu.Unlock()
	}()
	h.publishRegistration()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if p.write(protocol.Frame{Type: "ping"}) != nil {
					p.close()
					return
				}
			case <-p.closed:
				return
			}
		}
	}()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(30 * time.Second))
		f, err := readFrame(ws)
		if err != nil {
			return err
		}
		switch f.Type {
		case "ping":
			if err = p.write(protocol.Frame{Type: "pong"}); err != nil {
				return err
			}
		case "pong":
		case "registered":
			snapshot := p.registered()
			h.mu.Lock()
			if h.peer == p {
				for code, a := range snapshot {
					if h.apps[code] == a {
						a.registered = true
					}
				}
			}
			h.mu.Unlock()
		case "invoke":
			// Allocate the call record before reading the next frame, so an
			// immediately following cancel cannot race ahead of its invocation.
			started := make(chan struct{})
			go func(f protocol.Frame) {
				result := h.invoke(h.ctx, f, started, p.formats)
				if p.write(result) != nil {
					p.close()
				}
			}(f)
			<-started
		case "cancel":
			h.Cancel(f.CallID)
		case "error":
			if f.Error != nil {
				return f.Error
			}
			return fmt.Errorf("OIS reported an error")
		default:
			return fmt.Errorf("unexpected OIS frame %q", f.Type)
		}
	}
}

func readFrame(ws *websocket.Conn) (protocol.Frame, error) {
	kind, b, err := ws.ReadMessage()
	if err != nil {
		return protocol.Frame{}, err
	}
	var f protocol.Frame
	if kind != websocket.TextMessage {
		return f, fmt.Errorf("expected a JSON text frame")
	}
	if err := protocol.ValidateJSON(b); err != nil {
		return f, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err = dec.Decode(&f); err != nil {
		return f, err
	}
	return f, nil
}
