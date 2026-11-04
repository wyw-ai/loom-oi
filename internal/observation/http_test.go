package observation

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestHTTPProbeStatusAndFailures(t *testing.T) {
	for _, code := range []int{200, 503, 302, 418} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || !r.Close || r.Header.Get("Accept-Encoding") != "" || r.Header.Get("Cookie") != "" {
					t.Error("unexpected request capability")
				}
				w.Header().Set("Location", "http://192.0.2.1/must-not-follow")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "business body is irrelevant")
			}))
			defer server.Close()
			o := testOptions()
			o.URL = server.URL
			o.ReadyStatuses = []int{200, 302}
			p, err := NewPlan(o)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HTTP_PROXY", "http://192.0.2.1:9")
			t.Setenv("HTTPS_PROXY", "http://192.0.2.1:9")
			var dials atomic.Int32
			out := Execute(context.Background(), p, func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if address != p.Address() {
					t.Error("probe changed target")
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}, time.Now())
			want := MatchedReady
			if code == 503 {
				want = MatchedNotReady
			}
			if code == 418 {
				want = StatusUnmapped
			}
			if out.Reason != want || calls.Load() != 1 || dials.Load() != 1 {
				t.Fatalf("outcome %+v calls=%d dials=%d", out, calls.Load(), dials.Load())
			}
		})
	}
	for _, tc := range []struct {
		name string
		err  error
		want Reason
	}{
		{"address", ErrAddress, AddressDenied}, {"permission", ErrPermission, PermissionError},
		{"os_permission", os.ErrPermission, PermissionError}, {"connect", errors.New("private endpoint unavailable"), ConnectError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Execute(context.Background(), testPlan(t), func(context.Context, string, string) (net.Conn, error) { return nil, tc.err }, time.Now())
			if out.Readiness != Unknown || out.Reason != tc.want {
				t.Fatalf("failure created sample: %+v", out)
			}
		})
	}
	t.Run("nil_dial", func(t *testing.T) {
		if out := Execute(context.Background(), testPlan(t), nil, time.Now()); out.Reason != PermissionError {
			t.Fatal(out)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		if out := Execute(context.Background(), nil, nil, time.Now()); out.Reason != Unsupported {
			t.Fatal(out)
		}
	})
	t.Run("already_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out := Execute(ctx, testPlan(t), func(context.Context, string, string) (net.Conn, error) { t.Fatal("cancelled dial"); return nil, nil }, time.Now())
		if out.Reason != Timeout {
			t.Fatal(out)
		}
	})
	t.Run("slow_header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		o := testOptions()
		o.URL = server.URL
		o.Timeout = 50 * time.Millisecond
		p, _ := NewPlan(o)
		out := Execute(context.Background(), p, (&net.Dialer{}).DialContext, time.Now())
		if out.Reason != Timeout || out.Readiness != Unknown {
			t.Fatal(out)
		}
	})
}

type readCountConn struct {
	net.Conn
	bytes atomic.Int64
	reads atomic.Int32
}

func (c *readCountConn) Read(b []byte) (int, error) {
	c.reads.Add(1)
	n, err := c.Conn.Read(b)
	c.bytes.Add(int64(n))
	return n, err
}

func runRawProbe(t *testing.T, response string) (Outcome, int64, int32) {
	t.Helper()
	a, b := net.Pipe()
	count := &readCountConn{Conn: a}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer b.Close()
		reader := bufio.NewReader(b)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}
		_, _ = io.WriteString(b, response)
	}()
	out := Execute(context.Background(), testPlan(t), func(context.Context, string, string) (net.Conn, error) { return count, nil }, time.Now())
	_ = a.Close()
	_ = b.Close()
	<-done
	return out, count.bytes.Load(), count.reads.Load()
}

func TestHTTPProbeReadBounds(t *testing.T) {
	for _, bodyHeader := range []string{"Content-Length: 100000\r\n", "Content-Length: 1000000\r\n", "Transfer-Encoding: chunked\r\n"} {
		t.Run(strings.Fields(bodyHeader)[0]+strings.Fields(bodyHeader)[1], func(t *testing.T) {
			header := "HTTP/1.1 200 OK\r\n" + bodyHeader + "\r\n"
			out, n, reads := runRawProbe(t, header+strings.Repeat("x", 300000))
			if out.Readiness != Ready || n-int64(len(header)) > ReadBufferLimit || reads != 1 {
				t.Fatalf("body drain/read-ahead: %+v bytes=%d header=%d reads=%d", out, n, len(header), reads)
			}
		})
	}
	for _, tc := range []struct {
		name, response string
		want           Reason
	}{
		{"oversized_header", "HTTP/1.1 200 OK\r\nX-Large: " + strings.Repeat("x", HeaderLimit) + "\r\n\r\n", Malformed},
		{"informational_total", strings.Repeat("HTTP/1.1 103 Early Hints\r\nX-Hint: "+strings.Repeat("x", 1000)+"\r\n\r\n", 9) + "HTTP/1.1 200 OK\r\n\r\n", Malformed},
		{"bad_status", "NOT HTTP\r\n\r\n", Malformed},
		{"upgrade", "HTTP/1.1 101 Switching Protocols\r\nConnection: upgrade\r\nUpgrade: x\r\n\r\n", StatusUnmapped},
		{"bounded_header", "HTTP/1.1 200 OK\r\nX-Test: " + strings.Repeat("x", HeaderLimit-128) + "\r\nContent-Length: 0\r\n\r\n", MatchedReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, _ := runRawProbe(t, tc.response)
			if out.Reason != tc.want {
				t.Fatalf("got %+v want %s", out, tc.want)
			}
		})
	}
}

func TestHTTPProbeTLSIdentity(t *testing.T) {
	ca, err := pki.NewCA("probe-ca")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.Server("probe")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := leaf.TLS()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) != 0 {
			t.Error("unexpected client certificate")
		}
		w.WriteHeader(200)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequestClientCert, NextProtos: []string{"http/1.1"}}
	server.StartTLS()
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for _, name := range []string{"valid", "wrong_name", "wrong_ca"} {
		t.Run(name, func(t *testing.T) {
			o := testOptions()
			o.URL = "https://localhost:" + port + "/"
			o.CA = ca.PEM
			if name == "wrong_name" {
				o.URL = "https://untrusted.invalid:" + port + "/"
			}
			if name == "wrong_ca" {
				other, _ := pki.NewCA("other")
				o.CA = other.PEM
			}
			p, err := NewPlan(o)
			if err != nil {
				t.Fatal(err)
			}
			out := Execute(context.Background(), p, func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}, time.Now())
			want := MatchedReady
			if name != "valid" {
				want = TLSError
			}
			if out.Reason != want {
				t.Fatalf("TLS result %+v want %s", out, want)
			}
		})
	}
}
