package ois

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Listen              string       `yaml:"listen" json:"listen"`
	PublicURL           string       `yaml:"public_url" json:"public_url"`
	TLSCert             string       `yaml:"tls_cert" json:"tls_cert"`
	TLSKey              string       `yaml:"tls_key" json:"tls_key"`
	HTTPS               *HTTPSConfig `yaml:"https,omitempty" json:"https,omitempty"`
	DataFile            string       `yaml:"data_file" json:"data_file"`
	AllowedOrigins      []string     `yaml:"allowed_origins" json:"allowed_origins"`
	RequestTimeoutMS    int          `yaml:"request_timeout_ms" json:"request_timeout_ms"`
	HeartbeatIntervalMS int          `yaml:"heartbeat_interval_ms" json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMS  int          `yaml:"heartbeat_timeout_ms" json:"heartbeat_timeout_ms"`
}

// HTTPS adds a TLS listener alongside the default HTTP listener. The legacy
// tls_cert/tls_key fields instead make the root listener HTTPS-only.
type HTTPSConfig struct {
	Listen    string `yaml:"listen" json:"listen"`
	PublicURL string `yaml:"public_url" json:"public_url"`
	CertFile  string `yaml:"cert_file" json:"cert_file"`
	KeyFile   string `yaml:"key_file" json:"key_file"`
}

type AdminConfig struct {
	TokenEnv string `yaml:"token_env" json:"token_env"`
}

type Config struct {
	Version  int                `yaml:"version" json:"version"`
	StateDir string             `yaml:"state_dir" json:"state_dir"`
	Server   ServerConfig       `yaml:"server" json:"server"`
	Admin    AdminConfig        `yaml:"admin" json:"admin"`
	Channels map[string]Channel `yaml:"channels" json:"channels"`
	Tools    map[string]Tool    `yaml:"tools" json:"tools"`
	VMCPS    map[string]VMCP    `yaml:"vmcps" json:"vmcps"`
}

type Channel struct {
	Description    string `yaml:"description" json:"description"`
	ExpectedHostID string `yaml:"expected_host_id" json:"expected_host_id"`
	CredentialEnv  string `yaml:"credential_env" json:"credential_env"`
}

type Source struct {
	Channel string `yaml:"channel" json:"channel"`
	App     string `yaml:"app" json:"app"`
	Action  string `yaml:"action" json:"action"`
}

type Tool struct {
	Description string         `yaml:"description" json:"description"`
	InputSchema map[string]any `yaml:"input_schema" json:"input_schema"`
	Source      Source         `yaml:"source" json:"source"`
}

type Access struct {
	CredentialEnv string `yaml:"credential_env" json:"credential_env"`
	RequireHTTPS  bool   `yaml:"require_https" json:"require_https"`
}

type VMCP struct {
	Tools  []string `yaml:"tools" json:"tools"`
	Access Access   `yaml:"access" json:"access"`
}

type persisted struct {
	Version  int                `json:"version"`
	Channels map[string]Channel `json:"channels"`
	Tools    map[string]Tool    `json:"tools"`
	VMCPS    map[string]VMCP    `json:"vmcps"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func validID(s string) bool { return identifier.MatchString(s) }

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	d := yaml.NewDecoder(strings.NewReader(string(b)))
	d.KnownFields(true)
	if err := d.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("read OIS configuration: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return cfg, fmt.Errorf("unsupported configuration version %d", cfg.Version)
	}
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":25100"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = ".ois"
	}
	if cfg.Server.DataFile == "" {
		cfg.Server.DataFile = "data/ois-state.json"
	}
	if cfg.Server.RequestTimeoutMS == 0 {
		cfg.Server.RequestTimeoutMS = 60000
	}
	if cfg.Server.HeartbeatIntervalMS == 0 {
		cfg.Server.HeartbeatIntervalMS = 10000
	}
	if cfg.Server.HeartbeatTimeoutMS == 0 {
		cfg.Server.HeartbeatTimeoutMS = 30000
	}
	if cfg.Server.RequestTimeoutMS < 1 || cfg.Server.HeartbeatIntervalMS < 1 || cfg.Server.HeartbeatTimeoutMS <= cfg.Server.HeartbeatIntervalMS {
		return cfg, fmt.Errorf("invalid request or heartbeat timing")
	}
	if cfg.Admin.TokenEnv != "" && os.Getenv(cfg.Admin.TokenEnv) == "" {
		return cfg, fmt.Errorf("admin.token_env must name a nonempty environment variable")
	}
	legacy := cfg.Server.TLSCert != "" || cfg.Server.TLSKey != ""
	if legacy && cfg.Server.HTTPS != nil {
		return cfg, fmt.Errorf("server.https cannot be combined with legacy tls_cert/tls_key")
	}
	if legacy && (cfg.Server.TLSCert == "" || cfg.Server.TLSKey == "") {
		return cfg, fmt.Errorf("legacy TLS requires both tls_cert and tls_key")
	}
	scheme := "http"
	if legacy {
		scheme = "https"
	}
	if err := configurePublicURL(&cfg.Server.PublicURL, scheme, cfg.Server.Listen); err != nil {
		return cfg, fmt.Errorf("server.public_url: %w", err)
	}
	if secure := cfg.Server.HTTPS; secure != nil {
		if secure.Listen == "" {
			secure.Listen = ":25101"
		}
		if secure.CertFile == "" || secure.KeyFile == "" {
			return cfg, fmt.Errorf("server.https requires cert_file and key_file")
		}
		if secure.Listen == cfg.Server.Listen {
			return cfg, fmt.Errorf("HTTP and HTTPS listeners must use different addresses")
		}
		if err := configurePublicURL(&secure.PublicURL, "https", secure.Listen); err != nil {
			return cfg, fmt.Errorf("server.https.public_url: %w", err)
		}
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return cfg, err
	}
	paths := []*string{&cfg.Server.DataFile, &cfg.Server.TLSCert, &cfg.Server.TLSKey}
	if cfg.Server.HTTPS != nil {
		paths = append(paths, &cfg.Server.HTTPS.CertFile, &cfg.Server.HTTPS.KeyFile)
	}
	for _, field := range paths {
		if *field != "" && !filepath.IsAbs(*field) {
			*field = filepath.Join(base, *field)
		}
	}
	return cfg, nil
}

func configurePublicURL(value *string, scheme, listen string) error {
	*value = strings.TrimRight(*value, "/")
	if *value == "" {
		host, port, err := net.SplitHostPort(listen)
		if err != nil {
			return fmt.Errorf("invalid listen address: %w", err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "localhost"
		}
		*value = scheme + "://" + net.JoinHostPort(host, port)
	}
	u, err := url.Parse(*value)
	if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must be an absolute %s origin without credentials, path, query or fragment", scheme)
	}
	return nil
}

func (c ServerConfig) httpsURL() string {
	if c.HTTPS != nil {
		return c.HTTPS.PublicURL
	}
	if c.TLSCert != "" && c.TLSKey != "" {
		return c.PublicURL
	}
	return ""
}

func (s *Server) vmcpEndpoint(id string, vm VMCP) string {
	base := s.cfg.Server.PublicURL
	if vm.Access.RequireHTTPS {
		base = s.cfg.Server.httpsURL()
	}
	return base + "/mcp/" + id
}

func validateState(st persisted, server ServerConfig) error {
	if st.Version != 1 {
		return fmt.Errorf("unsupported persisted configuration version")
	}
	for id, ch := range st.Channels {
		if !validID(id) || !validID(ch.ExpectedHostID) {
			return fmt.Errorf("channel requires a valid ID and expected_host_id")
		}
	}
	for id, t := range st.Tools {
		if !validID(id) || !validID(t.Source.App) || !validID(t.Source.Action) {
			return fmt.Errorf("tool requires valid tool/app/action identifiers")
		}
		if _, ok := st.Channels[t.Source.Channel]; !ok {
			return fmt.Errorf("tool source references an unknown channel")
		}
		if t.InputSchema == nil {
			return fmt.Errorf("tool input_schema must be an object")
		}
	}
	for id, vm := range st.VMCPS {
		if !validID(id) {
			return fmt.Errorf("vMCP requires a valid ID")
		}
		if vm.Access.RequireHTTPS && server.httpsURL() == "" {
			return fmt.Errorf("vMCP %s requires HTTPS but no HTTPS listener is configured", id)
		}
		seen := make(map[string]bool)
		for _, name := range vm.Tools {
			if _, ok := st.Tools[name]; !ok || seen[name] {
				return fmt.Errorf("vMCP tools must reference existing, distinct tools")
			}
			seen[name] = true
		}
	}
	return nil
}

func normalizeState(st *persisted) {
	if st.Channels == nil {
		st.Channels = make(map[string]Channel)
	}
	if st.Tools == nil {
		st.Tools = make(map[string]Tool)
	}
	if st.VMCPS == nil {
		st.VMCPS = make(map[string]VMCP)
	}
}

func loadState(cfg Config) (persisted, error) {
	b, err := os.ReadFile(cfg.Server.DataFile)
	var st persisted
	if err == nil {
		if err = decodeJSONNumbers(b, &st); err != nil {
			return st, fmt.Errorf("read persistent OIS state: %w", err)
		}
	} else if os.IsNotExist(err) {
		st = persisted{Version: 1, Channels: cfg.Channels, Tools: cfg.Tools, VMCPS: cfg.VMCPS}
	} else {
		return st, err
	}
	normalizeState(&st)
	if err := validateState(st, cfg.Server); err != nil {
		return st, err
	}
	if os.IsNotExist(err) {
		err = writeState(cfg.Server.DataFile, st)
	}
	return st, err
}

func writeState(path string, st persisted) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ois-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}

func cloneState(st persisted) persisted {
	b, _ := json.Marshal(st)
	var next persisted
	_ = decodeJSONNumbers(b, &next)
	normalizeState(&next)
	return next
}

func decodeJSONNumbers(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
