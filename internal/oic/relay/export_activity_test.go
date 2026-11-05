package relay

import (
	"encoding/json"
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

func TestOICStatsExporterActivity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			e := newEnv(t, func(c map[string]any) { c["metrics_enabled"] = enabled })
			e.connect(1, 30000, true)
			hc := &http.Client{Timeout: time.Second}
			defer hc.CloseIdleConnections()
			read := func(want int) {
				t.Helper()
				r := must(hc.Get("http://" + e.agent.DiagAddr() + "/stats"))
				defer r.Body.Close()
				var s protocol.OICStats
				if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
					t.Fatal(err)
				}
				if r.StatusCode != 200 || s.Observation == nil || s.Observation.ExporterActive != want || s.Observation.ExporterYoungestActiveAgeNS < 0 || (want == 0 && s.Observation.ExporterYoungestActiveAgeNS != 0) {
					t.Fatalf("stats %+v", s)
				}
			}
			read(0)
			e.agent.mu.Lock()
			h := e.agent.exporter
			e.agent.mu.Unlock()
			if !enabled {
				if h != nil {
					t.Fatal("disabled handler created")
				}
				return
			}
			if h == nil {
				t.Fatal("configured diagnostic handler absent")
			}
			release := holdActivity(t, h)
			read(1)
			release()
			read(0)
		})
	}
}
