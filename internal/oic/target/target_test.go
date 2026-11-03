package target

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type fakeResolver struct {
	addrs []netip.Addr
	err   error
}

func (f fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return f.addrs, f.err
}

func listen(t *testing.T) (*net.TCPListener, uint16) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.(*net.TCPListener), uint16(ln.Addr().(*net.TCPAddr).Port)
}

func TestDialLiteral(t *testing.T) {
	_, port := listen(t)
	tg := config.Target{Address: "x", Addr: netip.MustParseAddr("127.0.0.1"), Host: "127.0.0.1", Port: port}
	c, err := (&Dialer{}).Dial(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	tg.Port = 1
	if _, err := (&Dialer{}).Dial(context.Background(), tg); protocol.CodeOf(err) != protocol.CodeTargetConnectFailed {
		t.Fatalf("closed port: %v", err)
	}
}

// TV-B2-09: a name resolving only outside the allowed networks is never dialed.
func TestDialHostName(t *testing.T) {
	_, port := listen(t)
	allowed := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	tg := config.Target{Address: "svc:1", Host: "svc", Port: port, Prefixes: allowed}
	var dialed []string
	d := &Dialer{
		Resolver: fakeResolver{addrs: []netip.Addr{netip.MustParseAddr("10.1.1.1"), netip.MustParseAddr("::ffff:127.0.0.1")}},
		DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
			dialed = append(dialed, a)
			return (&net.Dialer{}).DialContext(ctx, n, a)
		},
	}
	c, err := d.Dial(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if len(dialed) != 1 || dialed[0] != netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port).String() {
		t.Fatalf("dialed %v", dialed)
	}
	dialed = nil
	d.Resolver = fakeResolver{addrs: []netip.Addr{netip.MustParseAddr("10.1.1.1")}}
	if _, err := d.Dial(context.Background(), tg); protocol.CodeOf(err) != protocol.CodeTargetAddressNotAllowed || len(dialed) != 0 {
		t.Fatalf("%v dialed %v", err, dialed)
	}
	d.Resolver = fakeResolver{err: errors.New("nxdomain")}
	if _, err := d.Dial(context.Background(), tg); protocol.CodeOf(err) != protocol.CodeTargetConnectFailed {
		t.Fatal(err)
	}
	// The default resolver resolves localhost; a network without loopback refuses it.
	tg = config.Target{Address: "localhost:1", Host: "localhost", Port: port, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	if _, err := (&Dialer{}).Dial(context.Background(), tg); protocol.CodeOf(err) != protocol.CodeTargetAddressNotAllowed {
		t.Fatal(err)
	}
}

type notTCP struct{ net.Conn }

func TestDialRejectsNonTCP(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	d := &Dialer{DialContext: func(context.Context, string, string) (net.Conn, error) { return notTCP{a}, nil }}
	tg := config.Target{Address: "x", Addr: netip.MustParseAddr("127.0.0.1"), Host: "127.0.0.1", Port: 1}
	if _, err := d.Dial(context.Background(), tg); protocol.CodeOf(err) != protocol.CodeTargetConnectFailed {
		t.Fatal(err)
	}
}
