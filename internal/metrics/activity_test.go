package metrics

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestExporterActivitySnapshot(t *testing.T) {
	var absent *Handler
	if absent.Activity() != (ExportActivity{}) {
		t.Fatal("nil exporter is active")
	}
	var r Registry
	h := NewHandler(&r)
	a, ok := h.acquire()
	if !ok {
		t.Fatal("first slot")
	}
	b, ok := h.acquire()
	if !ok {
		t.Fatal("second slot")
	}
	young := time.Now().Add(-40 * time.Millisecond)
	h.activityMu.Lock()
	h.active[a], h.active[b] = young.Add(-time.Second), young
	h.activityMu.Unlock()
	before := time.Now()
	x := h.Activity()
	after := time.Now()
	if x.Active != 2 || x.YoungestActiveAge < before.Sub(young) || x.YoungestActiveAge > after.Sub(young) {
		t.Fatalf("inconsistent minimum: %+v", x)
	}
	if _, ok := h.acquire(); ok {
		t.Fatal("capacity enlarged")
	}
	if NewHandler(&r).Activity() != (ExportActivity{}) {
		t.Fatal("activity aggregated by registry")
	}
	h.release(b)
	if x := h.Activity(); x.Active != 1 || x.YoungestActiveAge < time.Second {
		t.Fatalf("wrong owner cleared: %+v", x)
	}
	h.release(a)
	if h.Activity() != (ExportActivity{}) {
		t.Fatal("empty age retained")
	}
	c, ok := h.acquire()
	if !ok {
		t.Fatal("slot not reusable")
	}
	h.release(c)
}

func TestExporterActivityExcludesRejectedWriter(t *testing.T) {
	var r Registry
	h := NewHandler(&r)
	ws := []*controlledWriter{writer(), writer(), writer()}
	done := make([]chan struct{}, len(ws))
	for i, w := range ws {
		w.flushEntered, w.release = make(chan struct{}), make(chan struct{})
		done[i] = make(chan struct{})
		go func() { defer close(done[i]); h.ServeHTTP(w, request()) }()
		await(t, w.flushEntered)
		want := min(i+1, 2)
		if x := h.Activity(); x.Active != want {
			t.Fatalf("held write activity %+v want %d", x, want)
		}
	}
	if ws[2].status != 429 {
		t.Fatal("third response not rejected")
	}
	close(ws[2].release)
	await(t, done[2])
	if h.Activity().Active != 2 {
		t.Fatal("rejected response released another owner")
	}
	if !r.Add(Samples, "ready") {
		t.Fatal("activity blocks registry")
	}
	close(ws[0].release)
	await(t, done[0])
	if h.Activity().Active != 1 {
		t.Fatal("first owner not released")
	}
	close(ws[1].release)
	await(t, done[1])
	if h.Activity() != (ExportActivity{}) {
		t.Fatal("last owner not released")
	}
}

func TestExporterActivityClearsEveryExit(t *testing.T) {
	for _, mode := range []string{"success", "write_error", "flush_error", "render_error", "oversize", "panic", "method", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			var r Registry
			h := NewHandler(&r)
			w := writer()
			req := request()
			switch mode {
			case "write_error":
				w.writeError = io.ErrClosedPipe
			case "flush_error":
				w.flushError = io.ErrClosedPipe
			case "render_error":
				h.render = func(Snapshot) ([]byte, error) { return nil, ErrTooLarge }
			case "oversize":
				h.render = func(Snapshot) ([]byte, error) { return make([]byte, MaxResponseBytes+1), nil }
			case "panic":
				h.render = func(Snapshot) ([]byte, error) { panic(http.ErrAbortHandler) }
			case "method":
				req.Method = "POST"
			case "unsupported":
				w.deadlineError = http.ErrNotSupported
			}
			if mode == "panic" || mode == "unsupported" {
				aborts(t, func() { h.ServeHTTP(w, req) })
			} else {
				h.ServeHTTP(w, req)
			}
			if h.Activity() != (ExportActivity{}) || len(h.slots) != 0 {
				t.Fatal("ownership leaked")
			}
		})
	}
}
