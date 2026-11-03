package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (w *world) oi(token string, stdin string, args ...string) (int, string, string) {
	full := append([]string{"--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", token}, args...)
	var out, errb bytes.Buffer
	code := Main(context.Background(), full, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestCLIConsumer(t *testing.T) {
	w := newWorld(t)
	code, out, errOut := w.oi(w.token, "", "discover")
	var list []protocol.ExposureView
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 2 {
		t.Fatal(code, out, errOut)
	}
	code, out, errOut = w.oi(w.token, "ping", "connect", "lab-a/http", "--stdio")
	if code != 0 || out != "T1:ping" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	// An aborted channel exits 1 with the ABORT code.
	code, _, errOut = w.oi(w.token, "q", "connect", "--stdio", "lab-a/reset")
	if code != 1 || !strings.Contains(errOut, "ChannelInterrupted") {
		t.Fatalf("%d %q", code, errOut)
	}
	o, err := w.c.OpenLatest(context.Background(), "lab-a/http", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if code, out, _ := w.oi(w.token, "", "channel", o.ChannelID); code != 0 || !strings.Contains(out, `"preparing"`) {
		t.Fatal(code, out)
	}
	if code, out, _ := w.oi(w.token, "", "close", o.ChannelID); code != 0 || !strings.Contains(out, "ClosedByClient") {
		t.Fatal(code, out)
	}
	if code, _, errOut := w.oi(w.token, "", "channel", "ch_none"); code != 1 || !strings.Contains(errOut, `"NotFound"`) {
		t.Fatal(code, errOut)
	}
	if code, _, errOut := w.oi(w.token, "", "connect", "lab-a/none", "--stdio"); code != 1 || !strings.Contains(errOut, "NotFound") {
		t.Fatal(code, errOut)
	}
}

func TestCLIListen(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	old := notifyContext
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { notifyContext = old }()
	var out, errb lockedBuf
	done := make(chan int, 1)
	go func() {
		done <- Main(context.Background(), []string{"--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", w.token,
			"connect", "lab-a/http", "--listen", "127.0.0.1:0"}, strings.NewReader(""), &out, &errb)
	}()
	var addr string
	deadline := time.Now().Add(5 * time.Second)
	for addr == "" && time.Now().Before(deadline) {
		var m map[string]string
		if json.Unmarshal([]byte(out.String()), &m) == nil {
			addr = m["listening"]
		}
		time.Sleep(5 * time.Millisecond)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("x"))
	_ = conn.(*net.TCPConn).CloseWrite()
	if b, _ := io.ReadAll(conn); string(b) != "T1:x" {
		t.Fatal(string(b))
	}
	_ = conn.Close()
	for !strings.Contains(errb.String(), "Completed") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// A failing Open is reported per connection.
	cancel()
	if code := <-done; code != 0 {
		t.Fatal(code, errb.String())
	}
	if _, stop := old(context.Background()); stop != nil {
		stop()
	}
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) {
		c, s := context.WithCancel(context.Background())
		return c, s
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) { return ctx2, cancel2 }
	var out2, errb2 lockedBuf
	go func() {
		done <- Main(context.Background(), []string{"--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", w.token,
			"connect", "lab-a/none", "--listen", "127.0.0.1:0"}, strings.NewReader(""), &out2, &errb2)
	}()
	addr = ""
	for addr == "" && time.Now().Before(deadline.Add(5*time.Second)) {
		var m map[string]string
		if json.Unmarshal([]byte(out2.String()), &m) == nil {
			addr = m["listening"]
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c2, err := net.Dial("tcp", addr); err == nil {
		_, _ = io.ReadAll(c2)
		_ = c2.Close()
	}
	for !strings.Contains(errb2.String(), "NotFound") && time.Now().Before(deadline.Add(5*time.Second)) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel2()
	<-done
	if !strings.Contains(errb2.String(), "NotFound") {
		t.Fatal(errb2.String())
	}
	// Ctrl-C during --stdio aborts the channel.
	ctx3, cancel3 := context.WithCancel(context.Background())
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) { return ctx3, cancel3 }
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		var o, e bytes.Buffer
		done <- Main(context.Background(), []string{"--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", w.token,
			"connect", "lab-a/http", "--stdio"}, pr, &o, &e)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel3()
	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("interrupted stdio exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdio did not stop")
	}
}

func TestCLIAdmin(t *testing.T) {
	w := newWorld(t)
	admin := w.tp.AdminToken
	run := func(want int, args ...string) string {
		t.Helper()
		code, out, errOut := w.oi(admin, "", args...)
		if code != want {
			t.Fatalf("%v: %d %s %s", args, code, out, errOut)
		}
		return out + errOut
	}
	if out := run(0, "admin", "config"); !strings.Contains(out, `"config_version"`) {
		t.Fatal(out)
	}
	if out := run(0, "admin", "stats"); !strings.Contains(out, `"channels"`) {
		t.Fatal(out)
	}
	if out := run(0, "admin", "audit", "--after", "1", "--limit", "2"); !strings.Contains(out, `"items"`) {
		t.Fatal(out)
	}
	ca, _ := pki.NewCA("x")
	leaf, _ := ca.Client("lab-b")
	certFile, _, _ := leaf.Write(t.TempDir(), "lab-b")
	run(0, "admin", "access", "create", "lab-b", "--pin-cert", certFile, "--max-exposures", "4", "--label", "site=b", "--disabled")
	out := run(0, "admin", "access", "get", "lab-b")
	if !strings.Contains(out, identity.SPKIPin(leaf.Cert)) || !strings.Contains(out, `"enabled":false`) || !strings.Contains(out, `"site":"b"`) {
		t.Fatal(out)
	}
	run(0, "admin", "access", "update", "lab-b", "--enable", "--clear-pins", "--max-exposures", "2", "--label", "site=c")
	out = run(0, "admin", "access", "list")
	if !strings.Contains(out, `"lab-b"`) || !strings.Contains(out, `"spki_pins":[]`) {
		t.Fatal(out)
	}
	run(0, "admin", "access", "update", "lab-b", "--disable", "--pin", strings.Repeat("a", 64))
	run(1, "admin", "access", "update", "lab-b", "--if-match", "0", "--enable")
	run(0, "admin", "access", "delete", "lab-b")
	out = run(0, "admin", "token", "create", "--subject", "bob", "--ttl-seconds", "60")
	var tok protocol.TokenCreated
	if err := json.Unmarshal([]byte(out), &tok); err != nil || tok.Scope != "consumer" {
		t.Fatal(out)
	}
	if out := run(0, "admin", "token", "list"); !strings.Contains(out, tok.TokenID) {
		t.Fatal(out)
	}
	run(0, "admin", "token", "revoke", tok.TokenID)
	run(0, "admin", "policy", "create", "p2", "--subject", "bob", "--selector", "lab-a/http", "--actions", "discover", "--disabled")
	run(0, "admin", "policy", "update", "p2", "--actions", "discover,open", "--enable")
	run(0, "admin", "policy", "update", "p2", "--disable", "--subject", "carol", "--selector", "lab-a/*")
	if out := run(0, "admin", "policy", "get", "p2"); !strings.Contains(out, `"carol"`) || !strings.Contains(out, `"enabled":false`) {
		t.Fatal(out)
	}
	if out := run(0, "admin", "policy", "list"); !strings.Contains(out, `"p2"`) {
		t.Fatal(out)
	}
	run(0, "admin", "policy", "delete", "p2")
	run(1, "admin", "policy", "get", "p2")
	// A consumer token cannot administer.
	if code, _, errOut := w.oi(w.token, "", "admin", "config"); code != 1 || !strings.Contains(errOut, "Denied") {
		t.Fatal(code, errOut)
	}
	if code, _, _ := w.oi(w.token, "", "admin", "access", "create", "x"); code != 1 {
		t.Fatal(code)
	}
	if code, _, _ := w.oi(admin, "", "admin", "access", "create", "x", "--pin-cert", filepath.Join(t.TempDir(), "none.pem")); code != 1 {
		t.Fatal(code)
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	_ = os.WriteFile(empty, []byte("nothing"), 0o600)
	if code, _, _ := w.oi(admin, "", "admin", "access", "create", "x", "--pin-cert", empty); code != 1 {
		t.Fatal(code)
	}
	bogus := filepath.Join(t.TempDir(), "bogus.pem")
	_ = os.WriteFile(bogus, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o600)
	if code, _, _ := w.oi(admin, "", "admin", "access", "create", "x", "--pin-cert", bogus); code != 1 {
		t.Fatal(code)
	}
}

func TestCLIUsage(t *testing.T) {
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := Main(context.Background(), args, strings.NewReader(""), &out, &errb)
		return code, out.String(), errb.String()
	}
	if code, _, errOut := run(); code != 2 || !strings.Contains(errOut, "usage") {
		t.Fatal(code)
	}
	if code, out, _ := run("version"); code != 0 || !strings.HasPrefix(out, "oi dev") {
		t.Fatal(out)
	}
	if code, out, _ := run("help"); code != 0 || !strings.Contains(out, "connect") {
		t.Fatal(out)
	}
	base := []string{"--ois", "https://127.0.0.1:1", "--token", "t"}
	cases := [][]string{
		{"frob"},
		{"discover", "extra"},
		{"channel"},
		{"connect", "a/b"},
		{"connect", "a/b", "--stdio", "--listen", "x"},
		{"connect", "--bogus"},
		{"admin"},
		{"admin", "config", "x"},
		{"admin", "audit", "x"},
		{"admin", "nope"},
		{"admin", "access"},
		{"admin", "access", "frob", "x"},
		{"admin", "access", "update", "x", "--enable", "--disable"},
		{"admin", "access", "create", "x", "--label", "novalue"},
		{"admin", "token"},
		{"admin", "token", "create"},
		{"admin", "token", "get", "x"},
		{"admin", "policy"},
		{"admin", "policy", "create"},
		{"admin", "policy", "update", "x", "--enable", "--disable"},
		{"admin", "access", "--bogus"},
		{"admin", "token", "--bogus"},
		{"admin", "policy", "--bogus"},
	}
	for _, c := range cases {
		if code, _, _ := run(append(base, c...)...); code != 2 {
			t.Errorf("%v: %d", c, code)
		}
	}
	if code, _, _ := run("--ois", "http://x", "--token", "t", "discover"); code != 2 {
		t.Fatal("bad URL")
	}
	if code, _, errOut := run(append(base, "discover")...); code != 1 || !strings.Contains(errOut, "oi:") {
		t.Fatal(code, errOut)
	}
}
