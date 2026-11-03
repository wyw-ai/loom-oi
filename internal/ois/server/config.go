// Package server runs OIS deployment A (B2 D2-a, D4-c): one process with
// an api listener (server TLS) for consumers and administrators and an
// access listener (mTLS) for OIC control connections and provider legs.
package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/logx"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// maxConfigBytes bounds the configuration file.
const maxConfigBytes = 1 << 20

// TLSConfig names the PEM files of the server certificate and of the CA
// that issues OIC client certificates.
type TLSConfig struct {
	CertFile     string `json:"cert_file"`
	KeyFile      string `json:"key_file"`
	ClientCAFile string `json:"client_ca_file"`
}

// Config is the OIS configuration file (strict JSON, B4 §2).
type Config struct {
	APIListen    string          `json:"api_listen"`
	AccessListen string          `json:"access_listen"`
	TLS          TLSConfig       `json:"tls"`
	Store        store.Config    `json:"store"`
	LogLevel     string          `json:"log_level,omitempty"`
	RawLimits    json.RawMessage `json:"limits,omitempty"`

	// Limits are the B4 §3 defaults with the "limits" overrides applied.
	Limits limits.Limits `json:"-"`
}

// LoadConfig reads, strictly decodes and validates a configuration file.
// Relative file paths are resolved against the file's directory.
func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var cfg Config
	if err := protocol.DecodeStrict(f, maxConfigBytes, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %v", path, err)
	}
	cfg.Limits = limits.Defaults()
	if len(cfg.RawLimits) > 0 {
		if err := protocol.DecodeStrictBytes(cfg.RawLimits, &cfg.Limits); err != nil {
			return Config{}, fmt.Errorf("%s: limits: %v", path, err)
		}
	}
	dir := filepath.Dir(path)
	for _, p := range []*string{&cfg.TLS.CertFile, &cfg.TLS.KeyFile, &cfg.TLS.ClientCAFile, &cfg.Store.SQLitePath} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %v", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch {
	case c.APIListen == "" || c.AccessListen == "":
		return errors.New("api_listen and access_listen are required")
	case c.TLS.CertFile == "" || c.TLS.KeyFile == "" || c.TLS.ClientCAFile == "":
		return errors.New("tls.cert_file, tls.key_file and tls.client_ca_file are required")
	}
	if _, err := logx.ParseLevel(c.LogLevel); err != nil {
		return err
	}
	if err := c.Store.Validate(); err != nil {
		return err
	}
	return c.Limits.Validate()
}

// NewLogger returns the JSON logger of the configured level.
func NewLogger(w io.Writer, level string) *slog.Logger { return logx.New(w, level) }

// loadTLS reads the server key pair and the client CA pool.
func loadTLS(c TLSConfig) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("server certificate: %w", err)
	}
	ca, err := os.ReadFile(c.ClientCAFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return tls.Certificate{}, nil, fmt.Errorf("client CA: %s holds no PEM certificate", c.ClientCAFile)
	}
	return cert, pool, nil
}

// redact hides a PostgreSQL password in log output.
func redact(c store.Config) string {
	if c.Driver == store.SQLite {
		return c.SQLitePath
	}
	if i := strings.Index(c.PostgresDSN, "@"); i >= 0 {
		if j := strings.Index(c.PostgresDSN, "://"); j >= 0 && j < i {
			return c.PostgresDSN[:j+3] + "…" + c.PostgresDSN[i:]
		}
	}
	return c.PostgresDSN
}
