package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	ca := must(pki.NewCA("ca"))
	path := writeConfig(t, dir, ca, map[string]any{"log_level": "debug"})
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.CertFile != filepath.Join(dir, "server.pem") || cfg.Store.SQLitePath != filepath.Join(dir, "ois.db") ||
		cfg.Limits.PrepareMS != 2000 || cfg.Limits.DataMax != 32<<10 || cfg.LogLevel != "debug" {
		t.Fatalf("%+v", cfg)
	}
	abs := filepath.Join(dir, "abs.db")
	cfg = must(LoadConfig(writeConfig(t, dir, ca, map[string]any{"store": map[string]string{"driver": "sqlite", "sqlite_path": abs}})))
	if cfg.Store.SQLitePath != abs {
		t.Fatal(cfg.Store.SQLitePath)
	}
	bad := map[string]map[string]any{
		"unknown field":   {"bogus": 1},
		"unknown limit":   {"limits": map[string]any{"nope": 1}},
		"invalid limit":   {"limits": map[string]any{"data_max": 0}},
		"missing listen":  {"api_listen": nil},
		"missing tls":     {"tls": map[string]string{"cert_file": "a"}},
		"bad level":       {"log_level": "loud"},
		"bad store":       {"store": map[string]string{"driver": "mysql"}},
		"store extra key": {"store": map[string]string{"driver": "sqlite", "sqlite_path": "x", "user": "y"}},
	}
	for name, extra := range bad {
		if _, err := LoadConfig(writeConfig(t, dir, ca, extra)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := LoadConfig(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestNewLoggerAndRedact(t *testing.T) {
	var b strings.Builder
	NewLogger(&b, "warn").Info("hidden")
	NewLogger(&b, "").Info("shown")
	if strings.Contains(b.String(), "hidden") || !strings.Contains(b.String(), "shown") {
		t.Fatal(b.String())
	}
	pw := "s3cret"
	for in, want := range map[string]string{
		"postgres://u:" + pw + "@db:5432/oi":                       "postgres://u@db:5432/oi",
		"host=db port=5433 user=u password=" + pw + " dbname=oi":   "postgres://u@db:5433/oi",
		"postgres://u@db/oi?password=" + pw + "&sslpassword=" + pw: "postgres://u@db:5432/oi",
		"user@db": "postgres (unparsable DSN)",
	} {
		if got := redact(store.Config{Driver: store.Postgres, PostgresDSN: in}); got != want || strings.Contains(got, pw) {
			t.Errorf("%q: %q", in, got)
		}
	}
	if redact(store.Config{Driver: store.SQLite, SQLitePath: "/x"}) != "/x" {
		t.Fatal("sqlite path")
	}
}

func TestStartFailures(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	ca := must(pki.NewCA("ca"))
	path := writeConfig(t, dir, ca, nil)
	cfg := must(LoadConfig(path))
	// The store has not been initialized.
	if _, err := Start(ctx, cfg, log); !errors.Is(err, store.ErrMigrationPending) {
		t.Fatalf("fresh store: %v", err)
	}
	if code, _, _ := runMain("init", "--config", path); code != 0 {
		t.Fatal("init")
	}
	broken := cfg
	broken.TLS.CertFile = filepath.Join(dir, "missing.pem")
	if _, err := Start(ctx, broken, log); err == nil {
		t.Error("missing certificate accepted")
	}
	broken = cfg
	broken.TLS.ClientCAFile = filepath.Join(dir, "missing.pem")
	if _, err := Start(ctx, broken, log); err == nil {
		t.Error("missing client CA accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.pem"), []byte("nothing"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken.TLS.ClientCAFile = filepath.Join(dir, "empty.pem")
	if _, err := Start(ctx, broken, log); err == nil {
		t.Error("CA without certificates accepted")
	}
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	defer ln.Close()
	for _, mod := range []func(*Config){
		func(c *Config) { c.APIListen = ln.Addr().String() },
		func(c *Config) { c.AccessListen = ln.Addr().String() },
	} {
		c := cfg
		mod(&c)
		if _, err := Start(ctx, c, log); err == nil {
			t.Error("busy address accepted")
		}
	}
	s := must(Start(ctx, cfg, log))
	if _, err := Start(ctx, cfg, log); !errors.Is(err, store.ErrAnotherInstance) {
		t.Errorf("second instance: %v", err)
	}
	// A listener that fails ends Serve with its error.
	_ = s.apiLn.Close()
	select {
	case err := <-s.Err():
		if err == nil {
			t.Fatal("nil listener error")
		}
		s.errc <- err
	case <-time.After(3 * time.Second):
		t.Fatal("listener failure not reported")
	}
	if err := s.run(ctx); err == nil {
		t.Fatal("run ignored the listener failure")
	}
}

func TestMainCommands(t *testing.T) {
	if code, _, errOut := runMain(); code != 2 || !strings.Contains(errOut, "usage") {
		t.Fatal(code, errOut)
	}
	if code, out, _ := runMain("help"); code != 0 || !strings.Contains(out, "serve") {
		t.Fatal(code, out)
	}
	if code, out, _ := runMain("version"); code != 0 || !strings.HasPrefix(out, "ois dev go") {
		t.Fatal(code, out)
	}
	if code, _, _ := runMain("frobnicate"); code != 2 {
		t.Fatal(code)
	}
	for _, args := range [][]string{{"serve"}, {"serve", "--config", "x", "extra"}, {"migrate", "--bogus"}, {"init", "--config", "/no/such/file.json"}} {
		if code, _, _ := runMain(args...); code != 2 {
			t.Errorf("%v: %d", args, code)
		}
	}
	dir := t.TempDir()
	ca := must(pki.NewCA("ca"))
	path := writeConfig(t, dir, ca, nil)
	if code, _, errOut := runMain("migrate", "--config", path); code != 2 || !strings.Contains(errOut, "--yes") {
		t.Fatal(code, errOut)
	}
	if code, out, _ := runMain("migrate", "--config", path, "--yes"); code != 0 || !strings.Contains(out, "applied migrations [1]") {
		t.Fatal(code, out)
	}
	if code, out, _ := runMain("migrate", "--config", path, "--yes"); code != 0 || !strings.Contains(out, "current") {
		t.Fatal(code, out)
	}
	// Init on a migrated database without an admin token issues one.
	code, out, errOut := runMain("init", "--config", path, "--ttl-seconds", "60")
	if code != 0 {
		t.Fatal(code, errOut)
	}
	var tok map[string]any
	if err := json.Unmarshal([]byte(out), &tok); err != nil || tok["scope"] != "admin" || tok["subject"] != "admin" {
		t.Fatal(out, err)
	}
	if code, _, errOut := runMain("init", "--config", path); code != 1 || !strings.Contains(errOut, "already") {
		t.Fatal(code, errOut)
	}
	if code, _, _ := runMain("init", "--config", writeConfig(t, t.TempDir(), ca, nil), "--subject", "bad subject"); code != 1 {
		t.Fatal("invalid subject accepted")
	}
	locked := writeConfig(t, dir, ca, map[string]any{"store": map[string]string{"driver": "sqlite", "sqlite_path": filepath.Join(dir, "no", "such", "dir.db")}})
	for _, cmd := range []string{"init", "migrate"} {
		args := []string{cmd, "--config", locked}
		if cmd == "migrate" {
			args = append(args, "--yes")
		}
		if code, _, _ := runMain(args...); code != 1 {
			t.Errorf("%s on an unusable store: %d", cmd, code)
		}
	}
}

type serveCommandLogState struct {
	text                       string
	started, stopped, overflow bool
}

type serveCommandLog struct {
	mu      sync.Mutex
	state   serveCommandLogState
	pending []byte
	started chan struct{}
}

func (w *serveCommandLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	const limit = 64 << 10
	if len(p) > limit-len(w.state.text) {
		w.state.overflow = true
		return len(p), nil // The test rejects overflow; retain the bounded original prefix.
	}
	w.state.text += string(p)
	w.pending = append(w.pending, p...)
	for {
		line, rest, complete := bytes.Cut(w.pending, []byte{'\n'})
		if !complete {
			break
		}
		w.pending = rest
		var record struct {
			Message string `json:"msg"`
		}
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		switch record.Message {
		case "ois started":
			if !w.state.started {
				w.state.started = true
				close(w.started)
			}
		case "ois stopped":
			w.state.stopped = true
		}
	}
	return len(p), nil
}

func (w *serveCommandLog) snapshot() serveCommandLogState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func TestServeCommand(t *testing.T) {
	dir := t.TempDir()
	ca := must(pki.NewCA("ca"))
	path := writeConfig(t, dir, ca, nil)
	if code, _, _ := runMain("serve", "--config", path); code != 1 {
		t.Fatal("serve on an uninitialized store")
	}
	if code, _, _ := runMain("init", "--config", path); code != 0 {
		t.Fatal("init")
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := notifyContext
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { notifyContext = old }()
	done := make(chan struct{})
	errOut := &serveCommandLog{started: make(chan struct{})}
	var code int
	var completedAt time.Time
	go func() {
		defer close(done)
		code = Main(context.Background(), []string{"serve", "--config", path}, &strings.Builder{}, errOut)
		completedAt = time.Now()
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("serve cleanup did not stop", errOut.snapshot())
		}
	}()
	select {
	case <-errOut.started:
	case <-done:
		t.Fatal("serve returned before cancellation", code, errOut.snapshot())
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not report actual startup", errOut.snapshot())
	}
	cancelRequested := time.Now()
	cancel()
	select {
	case <-done:
		if completedAt.Before(cancelRequested) {
			t.Fatal("serve returned before cancellation", code, errOut.snapshot())
		}
		if code != 0 {
			t.Fatal(code, errOut.snapshot())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
	if state := errOut.snapshot(); !state.started || !state.stopped || state.overflow {
		t.Fatal("missing or incomplete serve lifecycle log", state)
	}
	if _, stop := old(context.Background()); stop != nil {
		stop()
	}
}
