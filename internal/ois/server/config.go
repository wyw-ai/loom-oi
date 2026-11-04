// Package server runs OIS deployment A (B2 D2-a, D4-c): one process with
// an api listener (server TLS) for consumers and administrators and an
// access listener (mTLS) for OIC control connections and provider legs.
package server

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"

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
	APIListen            string          `json:"api_listen"`
	AccessListen         string          `json:"access_listen"`
	TLS                  TLSConfig       `json:"tls"`
	Store                store.Config    `json:"store"`
	LogLevel             string          `json:"log_level,omitempty"`
	RawLimits            json.RawMessage `json:"limits,omitempty"`
	RawObservationLimits json.RawMessage `json:"observation_limits,omitempty"`
	DiagListen           string          `json:"diag_listen,omitempty"`
	MetricsEnabled       bool            `json:"metrics_enabled,omitempty"`

	// Limits are the B4 §3 defaults with the "limits" overrides applied.
	Limits            limits.Limits      `json:"-"`
	ObservationLimits limits.Observation `json:"-"`
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
	cfg.ObservationLimits = limits.ObservationDefaults(cfg.Limits)
	if len(cfg.RawObservationLimits) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(cfg.RawObservationLimits, &fields); err != nil || fields == nil {
			return Config{}, errors.New("observation_limits must be an object")
		}
		for _, v := range fields {
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return Config{}, errors.New("observation_limits fields must not be null")
			}
		}
		if err := protocol.DecodeStrictBytes(cfg.RawObservationLimits, &cfg.ObservationLimits); err != nil {
			return Config{}, err
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
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	if err := c.ObservationLimits.Validate(c.Limits); err != nil {
		return err
	}
	return c.validateDiagnostics()
}

func (c Config) validateDiagnostics() error {
	if c.DiagListen == "" {
		if c.MetricsEnabled {
			return errors.New("metrics_enabled requires explicit loopback diag_listen")
		}
		return nil
	}
	host, port, err := net.SplitHostPort(c.DiagListen)
	if err != nil {
		return fmt.Errorf("diag_listen: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" || !ip.IsLoopback() {
		return errors.New("diag_listen requires a literal loopback IP")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return errors.New("diag_listen port is invalid")
	}
	return nil
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

// redact describes the store for logs without credentials. Both DSN forms
// pgx accepts are parsed, so a password in key/value form or in URL query
// parameters never reaches the log.
func redact(c store.Config) string {
	if c.Driver == store.SQLite {
		return c.SQLitePath
	}
	pc, err := pgconn.ParseConfig(c.PostgresDSN)
	if err != nil {
		return "postgres (unparsable DSN)"
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s", pc.User, pc.Host, pc.Port, pc.Database)
}
