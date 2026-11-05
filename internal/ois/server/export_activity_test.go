package server

import (
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type activityWriter struct {
	header           http.Header
	entered, release chan struct{}
}

func (w *activityWriter) Header() http.Header              { return w.header }
func (w *activityWriter) WriteHeader(int)                  {}
func (w *activityWriter) Write(p []byte) (int, error)      { return len(p), nil }
func (w *activityWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *activityWriter) FlushError() error                { close(w.entered); <-w.release; return nil }
func holdActivity(t *testing.T, h *metrics.Handler) func() {
	t.Helper()
	w := &activityWriter{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/metrics", nil)) }()
	select {
	case <-w.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not enter flush")
	}
	var once sync.Once
	closeWriter := func() { once.Do(func() { close(w.release); <-done }) }
	t.Cleanup(closeWriter)
	return closeWriter
}

func TestAdminStatsExporterActivity(t *testing.T) {
	ts := newTestServer(t)
	read := func(want int) {
		t.Helper()
		r := ts.do("GET", "/v1/admin/stats", ts.admin, nil)
		if r.status != 200 {
			t.Fatal(r.status)
		}
		var s protocol.OISStats
		r.decode(t, &s)
		if s.Observation == nil || s.Observation.ExporterActive != want || s.Observation.ExporterYoungestActiveAgeNS < 0 || (want == 0 && s.Observation.ExporterYoungestActiveAgeNS != 0) {
			t.Fatalf("activity %+v", s.Observation)
		}
	}
	read(0)
	ts.srv.exporter = metrics.NewHandler(ts.srv.metrics)
	release := holdActivity(t, ts.srv.exporter)
	read(1)
	if r := ts.do("GET", "/v1/admin/stats", "", nil); r.status != 401 {
		t.Fatal("stats became unauthenticated", r.status)
	}
	release()
	read(0)
}
