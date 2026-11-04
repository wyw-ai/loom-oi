package observation

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ProbeKind       = "http_status_v1"
	DefaultInterval = 5 * time.Second
	DefaultTimeout  = time.Second
	DefaultValidity = 15 * time.Second
	DefaultJitter   = 500 * time.Millisecond
	HeaderLimit     = 8 << 10
	ReadBufferLimit = 4 << 10
)

const NullDigest = "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b"

// PlanOptions is an internal compiler input, not a second public config
// schema. The config adapter must also build the locally authorized Target
// used by DialFunc. CA bytes come from the same successful candidate read.
type PlanOptions struct {
	URL                                 string
	AllowedCIDRs                        []string
	CA                                  []byte
	ReadyStatuses, NotReadyStatuses     []int
	Interval, Timeout, Validity, Jitter time.Duration
}

// Plan owns immutable copies, including TLS roots and canonical bytes.
type Plan struct {
	url, address, host, digest          string
	interval, timeout, validity, jitter time.Duration
	ready, notReady                     [600]bool
	roots                               *x509.CertPool
	canonical                           []byte
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func NewPlan(o PlanOptions) (*Plan, error) {
	if !durationMS(o.Interval, 100*time.Millisecond, 24*time.Hour) ||
		!durationMS(o.Validity, 100*time.Millisecond, 24*time.Hour) ||
		!durationMS(o.Timeout, 50*time.Millisecond, 30*time.Second) ||
		!durationMS(o.Jitter, 0, o.Interval/2) {
		return nil, errors.New("invalid probe duration")
	}
	u, err := url.Parse(o.URL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(o.URL, "#") || u.Opaque != "" {
		return nil, errors.New("probe URL must be an HTTP(S) host/path without query, userinfo or fragment")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("probe URL has no host")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return nil, errors.New("probe port must be in 1..65535")
	}
	port = strconv.FormatUint(n, 10)
	if len(o.AllowedCIDRs) == 0 {
		return nil, errors.New("probe needs allowed networks")
	}
	var networks []netip.Prefix
	for _, text := range o.AllowedCIDRs {
		p, err := netip.ParsePrefix(text)
		if err != nil {
			return nil, errors.New("invalid probe network")
		}
		p = p.Masked()
		if !slices.Contains(networks, p) {
			networks = append(networks, p)
		}
	}
	cidrs := make([]string, len(networks))
	for i, p := range networks {
		cidrs[i] = p.String()
	}
	slices.Sort(cidrs)
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		allowed := false
		for _, p := range networks {
			allowed = allowed || p.Contains(ip)
		}
		if !allowed {
			return nil, errors.New("probe IP is outside allowed networks")
		}
		host = ip.String()
	} else {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if host == "" {
			return nil, errors.New("probe URL has no normalized host")
		}
	}
	u.Host = net.JoinHostPort(host, port)
	if u.Path == "" {
		u.Path = "/"
	}
	p := &Plan{url: u.String(), address: u.Host, host: host,
		interval: o.Interval, timeout: o.Timeout, validity: o.Validity, jitter: o.Jitter}
	ready, err := statuses(o.ReadyStatuses, &p.ready)
	if err != nil {
		return nil, err
	}
	notReady, err := statuses(o.NotReadyStatuses, &p.notReady)
	if err != nil {
		return nil, err
	}
	for code := 200; code < len(p.ready); code++ {
		if p.ready[code] && p.notReady[code] {
			return nil, errors.New("probe status sets overlap")
		}
	}
	trust := ""
	if u.Scheme == "https" {
		p.roots = x509.NewCertPool()
		if len(o.CA) == 0 || !p.roots.AppendCertsFromPEM(o.CA) {
			return nil, errors.New("HTTPS probe requires valid explicit CA content")
		}
		trust = digest(o.CA)
	} else if len(o.CA) != 0 {
		// A supplied trust input is still part of the normalized contract;
		// HTTP itself never uses a TLS pool or transmits a client certificate.
		trust = digest(o.CA)
	}
	p.canonical, err = json.Marshal(map[string]any{
		"kind": ProbeKind, "url": p.url, "allowed_cidrs": cidrs, "tls_ca_sha256": trust,
		"ready_statuses": ready, "not_ready_statuses": notReady,
		"interval_ms": o.Interval.Milliseconds(), "timeout_ms": o.Timeout.Milliseconds(),
		"validity_ms": o.Validity.Milliseconds(), "jitter_ms": o.Jitter.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("canonical probe: %w", err)
	}
	p.digest = digest(p.canonical)
	return p, nil
}

func durationMS(v, minimum, maximum time.Duration) bool {
	return v >= minimum && v <= maximum && v%time.Millisecond == 0
}

func statuses(input []int, table *[600]bool) ([]int, error) {
	if len(input) == 0 {
		return nil, errors.New("probe status set must not be empty")
	}
	result := make([]int, 0, len(input))
	for _, code := range input {
		if code < 200 || code > 599 {
			return nil, errors.New("probe status outside 200..599")
		}
		if !table[code] {
			table[code] = true
			result = append(result, code)
		}
	}
	slices.Sort(result)
	return result, nil
}

func (p *Plan) Digest() string {
	if p == nil {
		return NullDigest
	}
	return p.digest
}
func (p *Plan) Canonical() []byte {
	if p == nil {
		return []byte("null")
	}
	return append([]byte(nil), p.canonical...)
}
func (p *Plan) URL() string             { return p.url }
func (p *Plan) Address() string         { return p.address }
func (p *Plan) Interval() time.Duration { return p.interval }
func (p *Plan) Timeout() time.Duration  { return p.timeout }
func (p *Plan) Validity() time.Duration { return p.validity }
func (p *Plan) Jitter() time.Duration   { return p.jitter }
