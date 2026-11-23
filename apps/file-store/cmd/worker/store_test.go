package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func fixture(t *testing.T) (*service, *service, string, string) {
	t.Helper()
	mount := t.TempDir()
	handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(mount), LockSystem: webdav.NewMemLS()}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	makeWorker := func() *service {
		s, err := openService(config{Store: "shared", Root: t.TempDir(), WebDAVURL: server.URL + "/dav/"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.root.Close() })
		return s
	}
	return makeWorker(), makeWorker(), mount, namespace("shared", "workbench", "consumer")
}

func TestRaceThenRediscoverAndVerify(t *testing.T) {
	a, b, mount, ns := fixture(t)
	ctx := context.Background()
	for i, s := range []*service{a, b} {
		if err := os.WriteFile(filepath.Join(s.root.Name(), "shot.bin"), []byte{0, 1, 0xff, byte(i)}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	type outcome struct {
		r   receipt
		err error
	}
	var results [2]outcome
	var wg sync.WaitGroup
	for i, s := range []*service{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); results[i].r, results[i].err = s.put(ctx, ns, "shot.bin", "shots/a.bin") }()
	}
	wg.Wait()
	winner := 0
	if results[0].err != nil {
		winner = 1
	}
	if results[winner].err != nil || results[1-winner].err == nil || classify(results[1-winner].err).Code != "ALREADY_EXISTS" {
		t.Fatalf("concurrent puts: %+v", results)
	}
	ref := results[winner].r.Ref
	found, err := b.lookup(ctx, ns, "shots/a.bin")
	if err != nil || found.Ref != ref {
		t.Fatalf("lookup: %+v %v", found, err)
	}
	listing, err := b.list(ctx, ns, "shots", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	entries := listing.(map[string]any)["entries"].([]listEntry)
	if len(entries) != 1 || entries[0].Path != "shots/a.bin" {
		t.Fatal(entries)
	}
	if _, err = b.get(ctx, ns, ref, "inbox/result"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(b.root.Name(), "inbox/result"))
	if err != nil || len(data) != 4 || data[3] != byte(winner) {
		t.Fatal("incorrect bytes")
	}
	if _, err = b.get(ctx, ns, ref, "inbox/result"); err == nil || classify(err).Code != "ALREADY_EXISTS" {
		t.Fatal(err)
	}
	if _, err = b.get(ctx, namespace("shared", "other", "consumer"), ref, "wrong"); err == nil || classify(err).Code != "ACCESS_DENIED" {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(mount, filepath.FromSlash(ns), "files/shots/a.bin"), []byte{0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = b.get(ctx, ns, ref, "inbox/corrupt"); err == nil || classify(err).Code != "INTEGRITY_ERROR" {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(b.root.Name(), "inbox/corrupt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published corrupt destination")
	}
}

func TestDirectoryAndCursor(t *testing.T) {
	a, _, _, ns := fixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(a.root.Name(), "empty"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := a.put(ctx, ns, "empty", name); err != nil {
			t.Fatal(err)
		}
	}
	page, err := a.list(ctx, ns, ".", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	cursor := page.(map[string]any)["next_cursor"].(string)
	next, err := a.list(ctx, ns, ".", 1, cursor)
	if err != nil || next.(map[string]any)["next_cursor"] != nil {
		t.Fatal(next, err)
	}
	if _, err = a.put(ctx, ns, "empty", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.list(ctx, ns, ".", 1, cursor); err == nil || classify(err).Code != "STALE_CURSOR" {
		t.Fatal(err)
	}
}

func TestFailedUploadNeverVisible(t *testing.T) {
	a, _, _, ns := fixture(t)
	a.confirmTimeout = 250 * time.Millisecond
	original := a.dav.client.Transport
	if original == nil {
		original = http.DefaultTransport
	}
	a.dav.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "MOVE" {
			return nil, errors.New("storage interrupted")
		}
		return original.RoundTrip(r)
	})
	if err := os.WriteFile(filepath.Join(a.root.Name(), "source"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.put(context.Background(), ns, "source", "aborted"); err == nil || classify(err).Code != "COMMIT_UNCONFIRMED" || classify(err).Details["store_path"] != "aborted" {
		t.Fatalf("unconfirmed publication must expose the recovery path: %v", err)
	}
	if _, err := a.lookup(context.Background(), ns, "aborted"); !errors.Is(err, httpFailure(404)) {
		t.Fatal(err)
	}
	page, err := a.list(context.Background(), ns, ".", 100, "")
	if err != nil || len(page.(map[string]any)["entries"].([]listEntry)) > 0 {
		t.Fatal(page, err)
	}
	a.dav.client.Transport = original
	if _, err = a.put(context.Background(), ns, "source", "aborted"); err != nil {
		t.Fatal(err)
	}
}

func TestMoveResponseLostAfterCommit(t *testing.T) {
	s, _, _, ns := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.dav.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		res, err := http.DefaultTransport.RoundTrip(r)
		if r.Method == "MOVE" && err == nil {
			res.Body.Close()
			cancel()
			return nil, errors.New("lost acknowledgement")
		}
		return res, err
	})
	if err := os.WriteFile(filepath.Join(s.root.Name(), "empty"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.put(ctx, ns, "empty", "lost.txt")
	if err != nil || result.Ref.Size != 7 {
		t.Fatalf("published receipt not recovered: %+v %v", result, err)
	}
}

func TestAcceptedStageMustBecomeVisible(t *testing.T) {
	s, _, _, ns := fixture(t)
	s.confirmTimeout = 200 * time.Millisecond
	s.dav.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, ".oi-stage-") {
			return &http.Response{StatusCode: 202, Header: make(http.Header), Body: http.NoBody}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if err := os.WriteFile(filepath.Join(s.root.Name(), "src"), []byte("queued"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.put(context.Background(), ns, "src", "queued.bin"); err == nil {
		t.Fatal("202 accepted without committed bytes")
	}
	if _, err := s.lookup(context.Background(), ns, "queued.bin"); !errors.Is(err, httpFailure(404)) {
		t.Fatal(err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (rt roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return rt(r) }

func TestLocalAndSecretBoundaries(t *testing.T) {
	a, _, _, ns := fixture(t)
	cfg := config{Root: a.root.Name(), PasswordFile: filepath.Join(a.root.Name(), "secret"), Username: "user"}
	if err := os.WriteFile(cfg.PasswordFile, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.password(); err == nil {
		t.Fatal("accepted credential inside published root")
	}
	cfg.PasswordFile = filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(cfg.PasswordFile, []byte("example\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if password, err := cfg.password(); err != nil || password != "example" {
		t.Fatal("failed to read outside credential")
	}
	cfg.PasswordEnv = "ALSO_ENV"
	if _, err := cfg.password(); err == nil {
		t.Fatal("ambiguous credentials")
	}
	for _, bad := range []string{"../secret", "/secret", "a\\b", ".oi-stage-foreign", "a/../b", "a:content", "a/./b"} {
		data, _ := json.Marshal(map[string]string{"path": bad})
		_, err := a.call(context.Background(), ns, "put_file", data)
		if err == nil || classify(err).Code != "INVALID_PATH" {
			t.Fatal(bad, err)
		}
	}
	if namespace("s", "a\x00b", "c") == namespace("s", "a", "b\x00c") {
		t.Fatal("scope separator collision")
	}
	_, err := a.call(context.Background(), ns, "put_file", []byte(`{"path":"foo","store_url":"untrusted"}`))
	if err == nil || !strings.Contains(err.Error(), "argument") {
		t.Fatal(err)
	}
}
