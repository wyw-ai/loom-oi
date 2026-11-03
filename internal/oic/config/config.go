// Package config loads the OIC configuration: the only authority on
// targets (B2 §4). Consumers and OIS can never supply or rewrite a target
// address; each exposure's revision is a content hash of its target and
// transport settings, so an OIC needs no persistent state.
package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// MaxBytes bounds the configuration file.
const MaxBytes = 1 << 20

// Target is where an exposure's channels are dialed.
type Target struct {
	Address      string   `json:"address"`
	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`

	// Host and Port split Address; Addr is set for IP literal targets.
	Host     string         `json:"-"`
	Port     uint16         `json:"-"`
	Addr     netip.Addr     `json:"-"`
	Prefixes []netip.Prefix `json:"-"`
}

// Exposure is one locally configured exposure.
type Exposure struct {
	Name          string `json:"name"`
	Target        Target `json:"target"`
	Protocol      string `json:"protocol"`
	AdmissionMode string `json:"admission_mode,omitempty"`
	Description   string `json:"description,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`

	// ID is "<access_id>/<name>"; Revision is derived (B2 §4).
	ID       string `json:"-"`
	Revision string `json:"-"`
}

// IsEnabled reports the effective enabled flag (default true).
func (e *Exposure) IsEnabled() bool { return e.Enabled == nil || *e.Enabled }

// OIS locates the access listener and the CA that signed its certificate.
type OIS struct {
	URL        string `json:"url"`
	CAFile     string `json:"ca_file"`
	ServerName string `json:"server_name,omitempty"`
}

// TLS is the OIC client certificate.
type TLS struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

// Config is the OIC configuration file (strict JSON).
type Config struct {
	AccessID   string          `json:"access_id"`
	OIS        OIS             `json:"ois"`
	TLS        TLS             `json:"tls"`
	Exposures  []*Exposure     `json:"exposures"`
	DiagListen string          `json:"diag_listen,omitempty"`
	LogLevel   string          `json:"log_level,omitempty"`
	RawLimits  json.RawMessage `json:"limits,omitempty"`

	Limits limits.Limits `json:"-"`
}

// Load reads and validates a configuration file; relative paths are
// resolved against its directory.
func Load(path string) (*Config, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(data) > MaxBytes {
		return nil, nil, fmt.Errorf("%s: larger than %d bytes", path, MaxBytes)
	}
	c, err := Parse(data, filepath.Dir(path))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", path, err)
	}
	return c, data, nil
}

// Parse decodes and validates a configuration.
func Parse(data []byte, dir string) (*Config, error) {
	var c Config
	if err := protocol.DecodeStrictBytes(data, &c); err != nil {
		return nil, err
	}
	c.Limits = limits.Defaults()
	if len(c.RawLimits) > 0 {
		if err := protocol.DecodeStrictBytes(c.RawLimits, &c.Limits); err != nil {
			return nil, fmt.Errorf("limits: %v", err)
		}
	}
	for _, p := range []*string{&c.OIS.CAFile, &c.TLS.CertFile, &c.TLS.KeyFile} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	if !protocol.ValidAccessID(c.AccessID) {
		return errors.New("access_id is malformed")
	}
	u, err := url.Parse(c.OIS.URL)
	if err != nil || u.Scheme != "wss" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return errors.New("ois.url must be wss://host:port of the OIS access listener")
	}
	if c.OIS.CAFile == "" || c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
		return errors.New("ois.ca_file, tls.cert_file and tls.key_file are required")
	}
	if c.DiagListen != "" {
		if err := loopbackAddr(c.DiagListen); err != nil {
			return fmt.Errorf("diag_listen: %v", err)
		}
	}
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, e := range c.Exposures {
		if e == nil {
			return fmt.Errorf("exposures[%d] is null", i)
		}
		if err := c.validateExposure(e); err != nil {
			return fmt.Errorf("exposure %q: %v", e.Name, err)
		}
		if seen[e.Name] {
			return fmt.Errorf("exposure %q is defined twice", e.Name)
		}
		seen[e.Name] = true
	}
	return nil
}

func loopbackAddr(s string) error {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err != nil || !ip.IsLoopback() {
		return errors.New("only loopback addresses are allowed")
	}
	return nil
}

func (c *Config) validateExposure(e *Exposure) error {
	switch {
	case !protocol.ValidName(e.Name):
		return errors.New("name is malformed")
	case !protocol.ValidProtocol(e.Protocol):
		return errors.New("protocol is malformed")
	case len(e.Description) > c.Limits.DescMax:
		return fmt.Errorf("description exceeds %d bytes", c.Limits.DescMax)
	}
	if e.AdmissionMode == "" {
		e.AdmissionMode = protocol.AdmissionBestEffort
	}
	if !protocol.ValidAdmission(e.AdmissionMode) {
		return errors.New("admission_mode must be best_effort or require_ready")
	}
	if err := e.Target.normalize(); err != nil {
		return err
	}
	e.ID = c.AccessID + "/" + e.Name
	e.Revision = Revision(e.Target, protocol.TransportTCP, e.Protocol)
	return nil
}

// normalize parses the target and enforces B2 §4: host names need
// allowed_cidrs; IP literals must lie inside any configured network.
func (t *Target) normalize() error {
	host, port, err := net.SplitHostPort(t.Address)
	if err != nil {
		return fmt.Errorf("target.address: %v", err)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return errors.New("target.address needs a port in 1..65535")
	}
	if host == "" {
		return errors.New("target.address needs a host")
	}
	t.Port = uint16(p)
	t.Prefixes = nil
	for _, s := range t.AllowedCIDRs {
		pfx, err := netip.ParsePrefix(s)
		if err != nil {
			return fmt.Errorf("allowed_cidrs: %v", err)
		}
		pfx = pfx.Masked()
		if !slices.Contains(t.Prefixes, pfx) {
			t.Prefixes = append(t.Prefixes, pfx)
		}
	}
	slices.SortFunc(t.Prefixes, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		t.Addr, t.Host = ip, ip.String()
		if len(t.Prefixes) > 0 && !t.Allowed(ip) {
			return errors.New("target IP lies outside allowed_cidrs")
		}
		return nil
	}
	t.Addr, t.Host = netip.Addr{}, strings.ToLower(strings.TrimSuffix(host, "."))
	if len(t.Prefixes) == 0 {
		return errors.New("a host name target needs allowed_cidrs")
	}
	return nil
}

// Allowed reports whether ip lies inside an allowed network; without
// networks only the configured IP literal itself is allowed.
func (t *Target) Allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if len(t.Prefixes) == 0 {
		return t.Addr.IsValid() && ip == t.Addr
	}
	for _, p := range t.Prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Revision is "rv_" followed by the first 26 characters of the lowercase
// base32 SHA-256 of the canonical JSON
// {"protocol":…,"target":{"address":…,"allowed_cidrs":[…]},"transport_profile":…}
// with sorted keys, the normalized host:port and sorted masked networks.
// Description and admission mode do not take part (B2 §4, D2-d).
func Revision(t Target, transport, proto string) string {
	cidrs := make([]string, 0, len(t.Prefixes))
	for _, p := range t.Prefixes {
		cidrs = append(cidrs, p.String())
	}
	canon, _ := json.Marshal(map[string]any{
		"protocol":          proto,
		"target":            map[string]any{"address": net.JoinHostPort(t.Host, strconv.Itoa(int(t.Port))), "allowed_cidrs": cidrs},
		"transport_profile": transport,
	})
	sum := sha256.Sum256(canon)
	return "rv_" + protocol.Base32.EncodeToString(sum[:])[:26]
}

// Find returns the exposure with the given ID.
func (c *Config) Find(id string) *Exposure {
	for _, e := range c.Exposures {
		if e.ID == id {
			return e
		}
	}
	return nil
}
