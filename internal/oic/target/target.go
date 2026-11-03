// Package target dials an exposure's local target under the B2 §4 rules:
// a host name is resolved within the dial deadline, only addresses inside
// the allowed networks are kept, and those IPs are dialed directly so the
// name is never resolved twice (no DNS rebinding window).
package target

import (
	"context"
	"net"
	"net/netip"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Resolver resolves host names; *net.Resolver implements it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer opens target connections.
type Dialer struct {
	Resolver Resolver
	// DialContext defaults to a net.Dialer.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Dial connects to t before ctx ends. Failures carry the B1 §7 codes
// TargetAddressNotAllowed (no allowed address; nothing is dialed) and
// TargetConnectFailed.
func (d *Dialer) Dial(ctx context.Context, t config.Target) (*net.TCPConn, error) {
	candidates := []netip.Addr{t.Addr}
	if !t.Addr.IsValid() {
		r := d.Resolver
		if r == nil {
			r = net.DefaultResolver
		}
		addrs, err := r.LookupNetIP(ctx, "ip", t.Host)
		if err != nil {
			return nil, protocol.Errorf(protocol.CodeTargetConnectFailed, "resolve %s: %v", t.Host, err)
		}
		candidates = candidates[:0]
		for _, a := range addrs {
			if t.Allowed(a) {
				candidates = append(candidates, a.Unmap())
			}
		}
		if len(candidates) == 0 {
			return nil, protocol.Errorf(protocol.CodeTargetAddressNotAllowed, "%s resolves only outside allowed_cidrs", t.Host)
		}
	}
	dial := d.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	var last error
	for _, a := range candidates {
		c, err := dial(ctx, "tcp", netip.AddrPortFrom(a, t.Port).String())
		if err == nil {
			if tc, ok := c.(*net.TCPConn); ok {
				return tc, nil
			}
			_ = c.Close()
			err = protocol.Errorf(protocol.CodeTargetConnectFailed, "not a TCP connection")
		}
		last = err
	}
	return nil, protocol.Errorf(protocol.CodeTargetConnectFailed, "dial %s: %v", t.Address, last)
}
