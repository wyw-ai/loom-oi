package ois

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type snapshot struct {
	Host protocol.HostInfo
	Apps []protocol.AppInfo
}

type Server struct {
	cfg       Config
	mu        sync.RWMutex
	state     persisted
	live      map[string]*connection
	snapshots map[string]snapshot
	pending   map[string]*pendingCall
	sessions  map[string]*mcpSession
	closed    bool
	mux       *http.ServeMux
}

func New(configPath string) (*Server, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	st, err := loadState(cfg)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, state: st, live: make(map[string]*connection), snapshots: make(map[string]snapshot), pending: make(map[string]*pendingCall), sessions: make(map[string]*mcpSession)}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/healthz", s.health)
	s.mux.HandleFunc("/api/v1/oic/connect", s.connect)
	s.mux.HandleFunc("/api/v1/exec", s.restExec)
	s.mux.HandleFunc("/api/v1/", s.admin)
	s.mux.HandleFunc("/mcp/", s.mcp)
	return s, nil
}

// Config returns startup settings and credential references, never resolved tokens.
func (s *Server) Config() Config {
	b, _ := json.Marshal(s.cfg)
	var cfg Config
	_ = decodeJSONNumbers(b, &cfg)
	return cfg
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if !s.validOrigin(r) {
			writeError(w, http.StatusForbidden, "", protocol.Err("FORBIDDEN", "origin is not permitted"))
			return
		}
		s.mu.RLock()
		closed := s.closed
		s.mu.RUnlock()
		if closed {
			writeError(w, http.StatusServiceUnavailable, "", protocol.Err("UNAVAILABLE", "server is closing"))
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	conns := make([]*connection, 0, len(s.live))
	for _, c := range s.live {
		conns = append(conns, c)
	}
	for _, p := range s.pending {
		p.complete(outcome{err: protocol.Err("CALL_RESULT_UNKNOWN", "server closed after accepting the call")})
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
	return nil
}

func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	canonical := u.Scheme + "://" + u.Host
	for _, allowed := range s.cfg.Server.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), canonical) {
			return true
		}
	}
	if secure := s.cfg.Server.httpsURL(); secure != "" && strings.EqualFold(secure, canonical) {
		return true
	}
	if s.cfg.Server.PublicURL != "" {
		return strings.EqualFold(s.cfg.Server.PublicURL, canonical)
	}
	// Without a configured public origin, only an exact same-origin request is accepted.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(canonical, scheme+"://"+r.Host)
}

func token(r *http.Request) string {
	f := strings.Fields(r.Header.Get("Authorization"))
	if len(f) != 2 || !strings.EqualFold(f[0], "Bearer") {
		return ""
	}
	return f[1]
}

func matchesEnv(t, env string) bool {
	// Empty is the explicit anonymous mode. A configured-but-missing variable
	// remains fail-closed, and arbitrary client headers never create identities.
	if env == "" {
		return true
	}
	want := os.Getenv(env)
	return t != "" && want != "" && subtle.ConstantTimeCompare([]byte(t), []byte(want)) == 1
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.MaxFrameBytes))
	if err != nil {
		return err
	}
	if err := protocol.ValidateJSON(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// Measure the serialized consumer envelope before writing any HTTP headers.
func writeBoundedJSON(w http.ResponseWriter, status int, v any) bool {
	data, err := json.Marshal(v)
	if err != nil || len(data)+1 > protocol.MaxFrameBytes {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, id string, err *protocol.Error) {
	body := struct {
		CallID string          `json:"call_id,omitempty"`
		Error  *protocol.Error `json:"error"`
	}{id, err}
	if !writeBoundedJSON(w, status, body) {
		body.Error = protocol.Err("RESULT_TOO_LARGE", "error response exceeds the HTTP budget")
		writeJSON(w, 422, body)
	}
}

func errorStatus(e *protocol.Error) int {
	switch e.Code {
	case "UNAUTHENTICATED":
		return 401
	case "FORBIDDEN", "HTTPS_REQUIRED":
		return 403
	case "NOT_FOUND":
		return 404
	case "INVALID_REQUEST":
		return 400
	case "RESOURCE_IN_USE", "BINDING_IMMUTABLE", "CHANNEL_CONNECTED", "SESSION_BUSY", "CALL_ID_CONFLICT":
		return 409
	case "CHANNEL_UNAVAILABLE", "APP_UNAVAILABLE", "CAPACITY_EXCEEDED":
		return 503
	case "CALL_RESULT_UNKNOWN":
		return 504
	case "INTERNAL_ERROR":
		return 500
	case "EXECUTION_ERROR":
		return 502
	default:
		return 422
	}
}

func keys[T any](m map[string]T) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

func (s *Server) availableLocked(src Source) bool {
	_, ready := s.readyActionLocked(src)
	return ready
}

func (s *Server) readyActionLocked(src Source) (protocol.ActionInfo, bool) {
	c := s.live[src.Channel]
	if c == nil || !c.registered {
		return protocol.ActionInfo{}, false
	}
	return s.catalogActionLocked(src)
}

func (s *Server) catalogActionLocked(src Source) (protocol.ActionInfo, bool) {
	for _, app := range s.snapshots[src.Channel].Apps {
		if app.App == src.App {
			for _, action := range app.Actions {
				if action.Name == src.Action {
					return action, app.Ready && action.Ready
				}
			}
		}
	}
	return protocol.ActionInfo{}, false
}

func authorizeAccess(id string, access Access, bearer string, secure bool) (string, *protocol.Error) {
	if access.RequireHTTPS && !secure {
		return "", protocol.Err("HTTPS_REQUIRED", "this service requires an HTTPS connection")
	}
	if !matchesEnv(bearer, access.CredentialEnv) {
		return "", protocol.Err("UNAUTHENTICATED", "valid service credentials are required")
	}
	if access.CredentialEnv == "" {
		return "anonymous:" + id, nil
	}
	return "consumer:" + id + ":" + access.CredentialEnv, nil
}

func duration(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }
