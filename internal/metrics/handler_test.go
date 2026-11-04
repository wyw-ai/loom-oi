package metrics

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type controlledWriter struct {
	header                                                   http.Header
	status                                                   int
	body                                                     bytes.Buffer
	deadlines, readDeadlines                                 []time.Time
	deadlineError, readDeadlineError, writeError, flushError error
	short                                                    bool
	flushEntered, release                                    chan struct{}
}

func writer() *controlledWriter                    { return &controlledWriter{header: make(http.Header)} }
func (w *controlledWriter) Header() http.Header    { return w.header }
func (w *controlledWriter) WriteHeader(status int) { w.status = status }
func (w *controlledWriter) Write(p []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	if w.short {
		return len(p) - 1, nil
	}
	return w.body.Write(p)
}
func (w *controlledWriter) SetWriteDeadline(d time.Time) error {
	w.deadlines = append(w.deadlines, d)
	return w.deadlineError
}
func (w *controlledWriter) SetReadDeadline(d time.Time) error {
	w.readDeadlines = append(w.readDeadlines, d)
	return w.readDeadlineError
}
func (w *controlledWriter) FlushError() error {
	if w.flushEntered != nil {
		close(w.flushEntered)
		<-w.release
	}
	return w.flushError
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("event did not arrive")
	}
}
func request() *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://localhost/metrics", nil)
}
func count(t *testing.T, r *Registry, key string) uint64 {
	t.Helper()
	v, ok := r.Snapshot().Counter(Exports, key)
	if !ok {
		t.Fatal(key)
	}
	return v
}
func aborts(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Errorf("expected ErrAbortHandler, got %v", p)
		}
	}()
	f()
	t.Error("did not abort")
}

func TestMetricsSlowWriter(t *testing.T) {
	var r Registry
	h := NewHandler(&r)
	writers := []*controlledWriter{writer(), writer()}
	done := []chan struct{}{make(chan struct{}), make(chan struct{})}
	for i, w := range writers {
		w.flushEntered, w.release = make(chan struct{}), make(chan struct{})
		go func(i int, w *controlledWriter) { defer close(done[i]); h.ServeHTTP(w, request()) }(i, w)
		await(t, w.flushEntered)
	}
	// Write has completed, but slots must remain held until Flush returns.
	if len(h.slots) != 2 {
		t.Fatal("slots released before flush")
	}
	third := writer()
	h.ServeHTTP(third, request())
	if third.status != 429 || count(t, &r, "rejected") != 1 {
		t.Fatal("third scrape was not rejected")
	}
	if !r.Add(Samples, "ready") {
		t.Fatal("registry blocked by network write")
	}
	if _, err := Render(r.Snapshot()); err != nil {
		t.Fatal(err)
	}
	writers[0].flushError = os.ErrDeadlineExceeded
	close(writers[0].release)
	await(t, done[0])
	if count(t, &r, "timeout") != 1 || len(h.slots) != 1 {
		t.Fatal("flush timeout was hidden")
	}
	for _, w := range writers {
		if len(w.deadlines) != 1 || w.deadlines[0].IsZero() {
			t.Fatal("write deadline reset/cleared")
		}
	}
	close(writers[1].release)
	await(t, done[1])
	if count(t, &r, "success") != 1 || len(h.slots) != 0 {
		t.Fatal("slots not returned")
	}
	last := writer()
	start := time.Now()
	h.ServeHTTP(last, request())
	if last.status != 200 || last.header.Get("Content-Type") != ContentType || last.body.Len() > MaxResponseBytes {
		t.Fatal("bad successful scrape")
	}
	if d := last.deadlines[0].Sub(start); d < WriteBudget || d > WriteBudget+50*time.Millisecond {
		t.Fatalf("deadline not fixed from entry: %v", d)
	}
}

func TestMetricsHTTPFailurePaths(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*controlledWriter)
		key       string
	}{
		{"write_error", func(w *controlledWriter) { w.writeError = io.ErrClosedPipe }, "write_error"},
		{"short_write", func(w *controlledWriter) { w.short = true }, "write_error"},
		{"flush_error", func(w *controlledWriter) { w.flushError = io.ErrClosedPipe }, "write_error"},
		{"timeout", func(w *controlledWriter) { w.writeError = os.ErrDeadlineExceeded }, "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r Registry
			h := NewHandler(&r)
			w := writer()
			tc.configure(w)
			h.ServeHTTP(w, request())
			if count(t, &r, tc.key) != 1 || count(t, &r, "success") != 0 || len(h.slots) != 0 {
				t.Fatal("failure lost or slot leaked")
			}
		})
	}
	t.Run("unsupported_writer", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := httptest.NewRecorder()
		aborts(t, func() { h.ServeHTTP(w, request()) })
		if w.Body.Len() != 0 || count(t, &r, "unsupported_writer") != 1 {
			t.Fatal("unbounded fallback response")
		}
	})
	t.Run("deadline_failed", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := writer()
		w.deadlineError = io.ErrClosedPipe
		aborts(t, func() { h.ServeHTTP(w, request()) })
		if count(t, &r, "write_error") != 1 || w.status != 0 {
			t.Fatal("closed connection misclassified")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		for _, mode := range []bool{false, true} {
			var r Registry
			h := NewHandler(&r)
			h.render = func(Snapshot) ([]byte, error) {
				if mode {
					return make([]byte, MaxResponseBytes+1), nil
				}
				return nil, ErrTooLarge
			}
			w := writer()
			h.ServeHTTP(w, request())
			if w.status != 503 || count(t, &r, "too_large") != 1 || bytes.Contains(w.body.Bytes(), []byte("# HELP")) {
				t.Fatal("oversize partial success")
			}
		}
	})
	t.Run("body", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := writer()
		req := httptest.NewRequest("GET", "http://localhost/metrics", strings.NewReader("body"))
		h.ServeHTTP(w, req)
		if w.status != 503 || !req.Close || w.header.Get("Connection") != "close" || len(w.readDeadlines) != 1 || w.readDeadlines[0] != w.deadlines[0] || count(t, &r, "request_rejected") != 1 {
			t.Fatal("body handling unbounded")
		}
	})
	t.Run("body_unsupported_deadline", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := writer()
		w.readDeadlineError = http.ErrNotSupported
		aborts(t, func() {
			h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/metrics", strings.NewReader("body")))
		})
		if w.status != 0 || count(t, &r, "unsupported_writer") != 1 {
			t.Fatal("implicit success")
		}
	})
	t.Run("method", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := writer()
		req := request()
		req.Method = "POST"
		h.ServeHTTP(w, req)
		if w.status != 503 || count(t, &r, "request_rejected") != 1 {
			t.Fatal("unexpected request accepted")
		}
	})
	t.Run("render_exhausts_budget", func(t *testing.T) {
		var r Registry
		h := NewHandler(&r)
		w := writer()
		h.render = func(Snapshot) ([]byte, error) {
			timer := time.NewTimer(time.Until(w.deadlines[0]))
			defer timer.Stop()
			<-timer.C
			return []byte("late"), nil
		}
		aborts(t, func() { h.ServeHTTP(w, request()) })
		if w.status != 0 || len(h.slots) != 0 || count(t, &r, "timeout") != 1 {
			t.Fatal("late response became successful")
		}
	})
	t.Run("nil_registry", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("nil registry accepted")
			}
		}()
		NewHandler(nil)
	})
}

type pipeListener struct {
	conn   net.Conn
	mu     sync.Mutex
	taken  bool
	closed chan struct{}
	once   sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.taken {
		l.taken = true
		l.mu.Unlock()
		return l.conn, nil
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

func TestMetricsRealHTTPDeadline(t *testing.T) {
	for _, mode := range []string{"success", "unread_response", "unfinished_request_body", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			var r Registry
			h := NewHandler(&r)
			serverConn, peer := net.Pipe()
			ln := &pipeListener{conn: serverConn, closed: make(chan struct{})}
			handled := make(chan struct{})
			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { defer close(handled); h.ServeHTTP(w, req) })}
			served := make(chan struct{})
			go func() { defer close(served); _ = srv.Serve(ln) }()
			defer func() { _ = peer.Close(); _ = srv.Close(); await(t, served) }()
			req := "GET /metrics HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n"
			if mode == "unfinished_request_body" {
				req += "Content-Length: 10\r\n"
			}
			req += "\r\n"
			if _, err := io.WriteString(peer, req); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "success":
				_ = peer.SetReadDeadline(time.Now().Add(time.Second))
				resp, err := http.ReadResponse(bufio.NewReader(peer), nil)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				await(t, handled)
				if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != ContentType || len(body) > MaxResponseBytes || count(t, &r, "success") != 1 {
					t.Fatal("bad real HTTP exposition")
				}
			case "unread_response":
				// net.Pipe has no kernel send buffer. This is an actual net/http
				// socket write blocked until its absolute deadline, not a mock Write.
				await(t, handled)
				if count(t, &r, "timeout") != 1 {
					t.Fatal("blocked real HTTP write did not time out")
				}
			case "unfinished_request_body":
				_ = peer.SetReadDeadline(time.Now().Add(time.Second))
				resp, err := http.ReadResponse(bufio.NewReader(peer), nil)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				await(t, handled)
				if resp.StatusCode != 503 || count(t, &r, "request_rejected") != 1 {
					t.Fatal("body was accepted")
				}
			case "disconnected":
				_ = peer.Close()
				await(t, handled)
				if count(t, &r, "write_error")+count(t, &r, "unsupported_writer") != 1 {
					t.Fatal("disconnect not recorded")
				}
			}
			if len(h.slots) != 0 {
				t.Fatal("real HTTP slot leaked")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
