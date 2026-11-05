package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type pendingLogSink struct {
	mu               sync.Mutex
	buf              bytes.Buffer
	entered, release chan struct{}
	once             sync.Once
}

func TestAdminPendingIdentityDisabledDoesNotSample(t *testing.T) {
	for _, mode := range []string{"info_valid", "debug_absent", "debug_invalid", "debug_valid"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := relay.New(relay.Config{Limits: limits.Defaults(), Epoch: 1, RunID: "run_" + strings.Repeat("a", 26),
				Now: func() time.Time { calls++; return time.Unix(100, 0) }})
			defer h.Shutdown(context.Background())
			level := slog.LevelDebug
			if mode == "info_valid" {
				level = slog.LevelInfo
			}
			s := &Server{hub: h, log: slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: level}))}
			r := httptest.NewRequest(http.MethodGet, "/v1/admin/stats", nil)
			if mode != "debug_absent" {
				id := "0123456789abcdef0123456789abcdef"
				if mode == "debug_invalid" {
					id = "invalid"
				}
				r.Header.Set(relay.PendingIdentityHeader, id)
			}
			w := httptest.NewRecorder()
			if err := s.adminStats(w, r, relay.Principal{}); err != nil || w.Code != http.StatusOK {
				t.Fatalf("Stats changed: %v, status %d", err, w.Code)
			}
			want := 0
			if mode == "debug_valid" {
				want = 1
			}
			if calls != want {
				t.Fatalf("diagnostic sample clock calls=%d, want %d", calls, want)
			}
		})
	}
}

func (s *pendingLogSink) Write(p []byte) (int, error) {
	if s.entered != nil && bytes.Contains(p, []byte(relay.PendingIdentityMessage)) {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}
func (s *pendingLogSink) records(t *testing.T) []relay.PendingIdentitySnapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []relay.PendingIdentitySnapshot
	for _, line := range bytes.Split(s.buf.Bytes(), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var m struct {
			Msg     string                        `json:"msg"`
			Pending relay.PendingIdentitySnapshot `json:"pending_identity"`
		}
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		if m.Msg == relay.PendingIdentityMessage {
			out = append(out, m.Pending)
		}
	}
	return out
}

func TestAdminPendingIdentityGates(t *testing.T) {
	ts := newTestServer(t)
	consumer := ts.consumerToken("observer")
	id := "0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"debug_valid", "info_valid", "debug_absent", "empty", "uppercase", "short", "long", "comma", "multiple", "anonymous", "consumer", "wrong_method", "other_endpoint"} {
		t.Run(mode, func(t *testing.T) {
			sink := &pendingLogSink{}
			level := slog.LevelDebug
			if mode == "info_valid" {
				level = slog.LevelInfo
			}
			ts.srv.log = slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: level}))
			method, path, token := http.MethodGet, "/v1/admin/stats", ts.admin
			values := []string{id}
			wantStatus := 200
			switch mode {
			case "debug_absent":
				values = nil
			case "empty":
				values = []string{""}
			case "uppercase":
				values = []string{strings.ToUpper(id)}
			case "short":
				values = []string{id[:31]}
			case "long":
				values = []string{"secret-not-an-id-" + strings.Repeat("x", 10000)}
			case "comma":
				values = []string{id + "," + id}
			case "multiple":
				values = []string{id, id}
			case "anonymous":
				token = ""
				wantStatus = 401
			case "consumer":
				token = consumer
				wantStatus = 403
			case "wrong_method":
				method = http.MethodPost
				wantStatus = 405
			case "other_endpoint":
				path = "/v1/admin/config"
			}
			req := must(http.NewRequest(method, ts.url(path), nil))
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			for _, v := range values {
				req.Header.Add(relay.PendingIdentityHeader, v)
			}
			res := must(ts.client.Do(req))
			raw := must(io.ReadAll(res.Body))
			_ = res.Body.Close()
			if res.StatusCode != wantStatus {
				t.Fatalf("status=%d: %s", res.StatusCode, raw)
			}
			records := sink.records(t)
			if mode != "debug_valid" {
				if len(records) != 0 {
					t.Fatalf("unexpected debug identity: %+v", records)
				}
				return
			}
			if len(records) != 1 {
				t.Fatalf("records=%d", len(records))
			}
			var stats protocol.OISStats
			if err := json.Unmarshal(raw, &stats); err != nil {
				t.Fatal(err)
			}
			d := records[0]
			if d.Schema != relay.PendingIdentitySchema || d.SampleID != id || d.PID <= 0 || d.RunID == nil || !d.Complete || d.Epoch != stats.Epoch || d.PendingQueries != stats.Observation.PendingQueries || d.ObservationBacklog != stats.Observation.ObservationBacklog || d.ObservationCapacity != stats.Observation.ObservationCapacity || d.DirtyMarked != stats.Observation.DirtyMarked {
				t.Fatalf("HTTP/debug pairing %+v", d)
			}
			if bytes.Contains(raw, []byte(id)) || bytes.Contains(raw, []byte("pending_identity")) || bytes.Contains(raw, []byte(relay.PendingIdentitySchema)) {
				t.Fatal("diagnostic leaked into HTTP response")
			}
		})
	}
}

func TestAdminPendingIdentityBlockedLog(t *testing.T) {
	ts := newTestServer(t)
	sink := &pendingLogSink{entered: make(chan struct{}), release: make(chan struct{})}
	ts.srv.log = slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(sink.release) }) }
	handlerDone := make(chan struct{})
	api := ts.srv.apiHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(handlerDone); api.ServeHTTP(w, r) }))
	defer func() { release(); srv.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := must(http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/admin/stats", nil))
	req.Header.Set("Authorization", "Bearer "+ts.admin)
	req.Header.Set(relay.PendingIdentityHeader, "0123456789abcdef0123456789abcdef")
	clientDone := make(chan error, 1)
	go func() {
		res, err := srv.Client().Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("diagnostic did not reach sink")
	}
	readDone := make(chan struct{})
	go func() { _ = ts.srv.hub.ConfigView(); close(readDone) }()
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("log sink held Hub mutex")
	}
	select {
	case err := <-clientDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wanted original client deadline: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not finish")
	}
	if len(sink.records(t)) != 0 {
		t.Fatal("blocked log was claimed durable before it was written")
	}
	release()
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("late handler did not join")
	}
	if got := sink.records(t); len(got) != 1 {
		t.Fatalf("late record not preserved: %d", len(got))
	}
	// The client really failed before this log appeared. A log alone is not a
	// successful HTTP request-window, even when the copied snapshot is complete.
}
