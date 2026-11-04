package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
)

// This instrument observes the real Handler. It never gates a server write,
// changes a socket's send buffer, or replaces the export slot state machine.
// It is opt-in diagnostic evidence, not a new formal G2 vector.
const g2TraceCapacity = 65536

type g2TraceEvent struct {
	Index   uint64 `json:"index"`
	AtNS    int64  `json:"at_ns"`
	Conn    uint64 `json:"conn"`
	Request uint64 `json:"request"`
	Action  string `json:"action"`
	Status  int    `json:"status"`
	Bytes   int    `json:"bytes"`
	Error   string `json:"error"`
	Remote  string `json:"remote,omitempty"`
	Local   string `json:"local,omitempty"`
}

type g2TraceEntry struct {
	event g2TraceEvent
	ready atomic.Bool
}

type g2HTTPTrace struct {
	next    atomic.Uint64
	conn    atomic.Uint64
	entries []g2TraceEntry
}

func g2NewHTTPTrace() *g2HTTPTrace {
	return &g2HTTPTrace{entries: make([]g2TraceEntry, g2TraceCapacity)}
}

func (t *g2HTTPTrace) add(e g2TraceEvent) {
	i := t.next.Add(1) - 1
	if i >= uint64(len(t.entries)) {
		return
	}
	e.Index, e.AtNS = i+1, resourceNS()
	t.entries[i].event = e
	t.entries[i].ready.Store(true)
}

func (t *g2HTTPTrace) snapshot() ([]g2TraceEvent, error) {
	n := t.next.Load()
	if n > uint64(len(t.entries)) {
		return nil, fmt.Errorf("trace capacity exceeded: %d", n)
	}
	result := make([]g2TraceEvent, 0, n)
	for i := uint64(0); i < n; i++ {
		if !t.entries[i].ready.Load() {
			return result, fmt.Errorf("trace event %d not published", i+1)
		}
		result = append(result, t.entries[i].event)
	}
	return result, nil
}

type g2TraceConn struct {
	id       uint64
	requests atomic.Uint64
}
type g2TraceContextKey struct{}

func (t *g2HTTPTrace) connContext(ctx context.Context, c net.Conn) context.Context {
	state := &g2TraceConn{id: t.conn.Add(1)}
	t.add(g2TraceEvent{Action: "connection", Conn: state.id, Remote: c.RemoteAddr().String(), Local: c.LocalAddr().String()})
	return context.WithValue(ctx, g2TraceContextKey{}, state)
}

func g2TraceError(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	if errors.Is(err, io.ErrShortWrite) {
		return "short_write"
	}
	if errors.Is(err, http.ErrNotSupported) {
		return "unsupported"
	}
	return "write_error"
}

type g2TraceWriter struct {
	inner   http.ResponseWriter
	trace   *g2HTTPTrace
	conn    uint64
	request uint64
	status  int
}

func (w *g2TraceWriter) event(action string, n int, err error) {
	w.trace.add(g2TraceEvent{Action: action, Conn: w.conn, Request: w.request, Status: w.status, Bytes: n, Error: g2TraceError(err)})
}
func (w *g2TraceWriter) Header() http.Header         { return w.inner.Header() }
func (w *g2TraceWriter) Unwrap() http.ResponseWriter { return w.inner }
func (w *g2TraceWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.event("header", 0, nil)
	}
	w.inner.WriteHeader(code)
}
func (w *g2TraceWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.event("write_enter", len(p), nil)
	n, err := w.inner.Write(p)
	w.event("write_exit", n, err)
	return n, err
}
func (w *g2TraceWriter) FlushError() error {
	w.event("flush_enter", 0, nil)
	err := http.NewResponseController(w.inner).Flush()
	w.event("flush_exit", 0, err)
	return err
}

func (t *g2HTTPTrace) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, ok := r.Context().Value(g2TraceContextKey{}).(*g2TraceConn)
		if !ok {
			t.add(g2TraceEvent{Action: "missing_connection"})
			next.ServeHTTP(w, r)
			return
		}
		writer := &g2TraceWriter{inner: w, trace: t, conn: state.id, request: state.requests.Add(1)}
		writer.event("handler_enter", 0, nil)
		defer func() {
			if value := recover(); value != nil {
				writer.event("handler_exit", 0, errors.New("handler panic"))
				panic(value)
			}
			writer.event("handler_exit", 0, nil)
		}()
		next.ServeHTTP(writer, r)
	})
}

// Checks complete call pairs after cleanup. Global atomic append order is not
// interpreted as a total temporal order between independent server goroutines.
func g2ValidateHTTPTrace(events []g2TraceEvent) error {
	connections := map[uint64]bool{}
	type requestState struct {
		at                                int64
		entered, ended, writing, flushing bool
		status                            int
	}
	states := map[[2]uint64]*requestState{}
	lastRequest := map[uint64]uint64{}
	for i, e := range events {
		if e.Index != uint64(i+1) || e.AtNS <= 0 || e.Conn == 0 {
			return fmt.Errorf("invalid event identity/time at %d", i)
		}
		if e.Action == "connection" {
			if connections[e.Conn] || e.Request != 0 || e.Remote == "" || e.Local == "" {
				return fmt.Errorf("invalid connection %d", e.Conn)
			}
			connections[e.Conn] = true
			continue
		}
		if !connections[e.Conn] || e.Request == 0 {
			return fmt.Errorf("unbound request at %d", i)
		}
		key := [2]uint64{e.Conn, e.Request}
		s := states[key]
		if s == nil {
			s = &requestState{}
			states[key] = s
		}
		if e.AtNS < s.at || s.ended {
			return fmt.Errorf("request time/order at %d", i)
		}
		s.at = e.AtNS
		switch e.Action {
		case "handler_enter":
			if s.entered || e.Request != lastRequest[e.Conn]+1 {
				return fmt.Errorf("request ordinal at %d", i)
			}
			if previous := states[[2]uint64{e.Conn, e.Request - 1}]; previous != nil && !previous.ended {
				return fmt.Errorf("HTTP/1 handler overlap on one connection")
			}
			s.entered = true
			lastRequest[e.Conn] = e.Request
		case "header":
			if !s.entered || s.status != 0 || e.Status < 100 || e.Status > 599 {
				return fmt.Errorf("header order at %d", i)
			}
			s.status = e.Status
		case "write_enter":
			if !s.entered || s.writing || s.flushing || s.status == 0 || e.Status != s.status || e.Bytes < 0 {
				return fmt.Errorf("write order at %d", i)
			}
			s.writing = true
		case "write_exit":
			if !s.writing || e.Status != s.status || e.Bytes < 0 {
				return fmt.Errorf("write return at %d", i)
			}
			s.writing = false
		case "flush_enter":
			if !s.entered || s.writing || s.flushing || e.Status != s.status {
				return fmt.Errorf("flush order at %d", i)
			}
			s.flushing = true
		case "flush_exit":
			if !s.flushing || e.Status != s.status {
				return fmt.Errorf("flush return at %d", i)
			}
			s.flushing = false
		case "handler_exit":
			if !s.entered || s.writing || s.flushing || e.Status != s.status {
				return fmt.Errorf("incomplete handler at %d", i)
			}
			s.ended = true
		default:
			return fmt.Errorf("unknown action %s", e.Action)
		}
	}
	if len(states) == 0 {
		return fmt.Errorf("no real handler events")
	}
	for _, s := range states {
		if !s.ended {
			return fmt.Errorf("unfinished handler")
		}
	}
	return nil
}

type g2TraceSnapshot struct {
	StartedNS   int64  `json:"started_ns"`
	CompletedNS int64  `json:"completed_ns"`
	Body        string `json:"body"`
	data        metrics.Snapshot
}

func g2TakeTraceSnapshot(r *metrics.Registry) g2TraceSnapshot {
	s := g2TraceSnapshot{StartedNS: resourceNS()}
	s.data = r.Snapshot()
	s.CompletedNS = resourceNS()
	return s
}

type g2TraceTuple struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	AtNS   int64  `json:"at_ns"`
	Reused bool   `json:"reused"`
}

func g2TraceGet(ctx context.Context, hc *http.Client, addr string) (g2Exposition, []g2TraceTuple, error) {
	var connections []g2TraceTuple
	overflow := false
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		if len(connections) >= 4 {
			overflow = true
			return
		}
		connections = append(connections, g2TraceTuple{Local: info.Conn.LocalAddr().String(), Remote: info.Conn.RemoteAddr().String(), AtNS: resourceNS(), Reused: info.Reused})
	}})
	x, err := g2ReadExposition(ctx, hc, addr)
	if overflow {
		err = errors.Join(err, fmt.Errorf("observer connection history overflow"))
	}
	return x, connections, err
}

func g2ValidateTraceTuples(events []g2TraceEvent, slow []g2TraceTuple, observers [][]g2TraceTuple) error {
	known := map[[2]string]uint64{}
	for _, e := range events {
		if e.Action != "connection" {
			continue
		}
		key := [2]string{e.Remote, e.Local}
		if known[key] != 0 {
			return fmt.Errorf("duplicate accepted TCP tuple")
		}
		known[key] = e.Conn
	}
	if len(slow) != 2 || slow[0].Local == slow[1].Local {
		return fmt.Errorf("two distinct slow tuples required")
	}
	roles := map[uint64]string{}
	for _, c := range slow {
		id := known[[2]string{c.Local, c.Remote}]
		if id == 0 {
			return fmt.Errorf("slow tuple not accepted")
		}
		roles[id] = "slow"
	}
	for _, attempts := range observers {
		if len(attempts) == 0 {
			return fmt.Errorf("observer has no actual connection")
		}
		for _, c := range attempts {
			id := known[[2]string{c.Local, c.Remote}]
			if id == 0 || roles[id] == "slow" || c.AtNS <= 0 {
				return fmt.Errorf("observer tuple invalid or reused slow connection")
			}
			roles[id] = "observer"
		}
	}
	if len(roles) != len(known) {
		return fmt.Errorf("unclassified accepted connection")
	}
	return nil
}

func TestG2CompetitionSlowInputTrace(t *testing.T) {
	if os.Getenv("OI_IT") != "1" || os.Getenv("OI_G2_INPUT_TRACE") != "1" {
		t.Skip("diagnostic scope: requires OI_IT=1 and OI_G2_INPUT_TRACE=1; not formal G2 evidence")
	}
	trace := g2NewHTTPTrace()
	registry := &metrics.Registry{}
	server := httptest.NewUnstartedServer(trace.handler(metrics.NewHandler(registry)))
	server.Config.ConnContext = trace.connContext
	server.Start()
	transport := &http.Transport{Proxy: nil}
	hc := &http.Client{Transport: transport}
	addr := server.Listener.Addr().String()
	var slow *g2SlowScrapes
	var headers []g2CompetitionResponseHeader
	var observations []g2Exposition
	var inputBefore, beforeClose, afterClose g2TraceSnapshot
	var tuples []g2TraceTuple
	var observerTuples [][]g2TraceTuple
	var problem string
	var deadline time.Time
	rejected := false
	failed := false
	stage := "baseline"
	defer func() {
		beforeClose = g2TakeTraceSnapshot(registry)
		cleanupStart := resourceNS()
		done := make(chan error, 1)
		go func() {
			var err error
			if slow != nil {
				err = slow.Close()
			}
			transport.CloseIdleConnections()
			server.Close()
			done <- err
		}()
		cleanupErr := ""
		select {
		case err := <-done:
			if err != nil {
				cleanupErr = err.Error()
			}
		case <-time.After(2 * time.Second):
			cleanupErr = "diagnostic cleanup exceeded original 2s"
			server.CloseClientConnections()
		}
		cleanupEnd := resourceNS()
		if cleanupEnd-cleanupStart > int64(2*time.Second) {
			cleanupErr = "diagnostic cleanup exceeded original 2s"
		}
		afterClose = g2TakeTraceSnapshot(registry)
		events, err := trace.snapshot()
		traceErr := ""
		if err == nil {
			err = g2ValidateHTTPTrace(events)
		}
		if err == nil {
			err = g2ValidateTraceTuples(events, tuples, observerTuples)
		}
		if err != nil {
			traceErr = err.Error()
		}
		for _, snap := range []*g2TraceSnapshot{&inputBefore, &beforeClose, &afterClose} {
			body, err := metrics.Render(snap.data)
			if err != nil {
				t.Errorf("snapshot render: %v", err)
			} else {
				snap.Body = string(body)
			}
		}
		writes := []g2SlowWrite{}
		if slow != nil {
			writes = slow.Snapshot()
		}
		artifact := map[string]any{"schema": "g2-input-trace-v1", "diagnostic_only": true, "stage": stage,
			"input_error": problem, "persistent_rejection": rejected, "two_failures_before_close": failed,
			"input_deadline_ns": int64(deadline.Sub(resourceOrigin)), "input_before": inputBefore,
			"before_close": beforeClose, "after_close": afterClose, "slow_tuples": tuples, "observer_tuples": observerTuples, "slow_writes": writes,
			"slow_response_headers": headers, "observations": observations, "events": events,
			"event_capacity": g2TraceCapacity, "trace_error": traceErr, "cleanup_error": cleanupErr,
			"cleanup_started_ns": cleanupStart, "cleanup_completed_ns": cleanupEnd}
		path := filepath.Join(runRoot, "g2-slow-input-trace.json")
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			err = json.NewEncoder(out).Encode(artifact)
			err = errors.Join(err, out.Sync(), out.Close())
		}
		if err != nil {
			t.Errorf("preserve diagnostic: %v", err)
		}
		t.Logf("diagnostic=%s events=%d observers=%d persistent429=%v failures=%v trace_error=%q cleanup_error=%q", path, len(events), len(observations), rejected, failed, traceErr, cleanupErr)
		if traceErr != "" || cleanupErr != "" {
			t.Errorf("invalid diagnostic trace/cleanup")
		}
	}()
	fail := func(err error) { problem = err.Error(); t.Fatal(err) }
	baseCtx, baseCancel := context.WithTimeout(context.Background(), time.Second)
	baseline, baseTuples, baseErr := g2TraceGet(baseCtx, hc, addr)
	baseCancel()
	observerTuples = append(observerTuples, baseTuples)
	if baseErr != nil {
		fail(baseErr)
	}
	if baseline.Status != 200 {
		fail(fmt.Errorf("baseline status %d", baseline.Status))
	}
	inputBefore = g2TakeTraceSnapshot(registry)
	var err error
	slow, err = g2BeginSlowScrapesWithDialer(context.Background(), addr, g2CompetitionDialer())
	if err != nil {
		fail(err)
	}
	for _, c := range slow.conns {
		tuples = append(tuples, g2TraceTuple{Local: c.LocalAddr().String(), Remote: c.RemoteAddr().String(), AtNS: resourceNS()})
	}
	writes := slow.Snapshot()
	deadline = resourceOrigin.Add(time.Duration(min(writes[0].ConnectedNS, writes[1].ConnectedNS)) + time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	stage = "await_socket_writes"
	if err = g2CompetitionAwaitWrites(slow, deadline); err != nil {
		fail(err)
	}
	stage = "await_first_response_headers"
	headers, err = g2CompetitionPeekHeaders(slow, deadline)
	if err != nil {
		fail(err)
	}
	stage = "await_persistent_rejection"
	gate := g2CompetitionRejectionGate{deadlineNS: int64(deadline.Sub(resourceOrigin))}
	for ctx.Err() == nil {
		x, connections, err := g2TraceGet(ctx, hc, addr)
		observations = append(observations, x)
		observerTuples = append(observerTuples, connections)
		if len(observations) > 128 {
			fail(fmt.Errorf("observer capacity exceeded"))
		}
		if err != nil {
			fail(err)
		}
		if !rejected {
			rejected, err = gate.observe(x)
			if err != nil {
				fail(err)
			}
		}
		if x.Status == 200 && g2CompetitionExportFailures(x)-g2CompetitionExportFailures(baseline) >= 2 {
			failed = true
		}
		if x.Status != 200 && x.Status != 429 {
			fail(fmt.Errorf("unexpected observer status %d", x.Status))
		}
		if rejected && failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stage = "input_finished"
	if !rejected || !failed {
		fail(fmt.Errorf("original bounded input not established: persistent429=%v two_failures=%v", rejected, failed))
	}
}

type g2TraceFakeWriter struct {
	header                             http.Header
	status, writes, flushes, deadlines int
	writeErr, flushErr                 error
	deadline                           time.Time
}

func (w *g2TraceFakeWriter) Header() http.Header { return w.header }
func (w *g2TraceFakeWriter) WriteHeader(n int)   { w.status = n }
func (w *g2TraceFakeWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}
func (w *g2TraceFakeWriter) FlushError() error { w.flushes++; return w.flushErr }
func (w *g2TraceFakeWriter) SetWriteDeadline(d time.Time) error {
	w.deadlines++
	w.deadline = d
	return nil
}

func TestG2CompetitionTraceTransparency(t *testing.T) {
	for _, kind := range []string{"success", "write_error", "flush_error"} {
		t.Run(kind, func(t *testing.T) {
			trace := g2NewHTTPTrace()
			trace.add(g2TraceEvent{Action: "connection", Conn: 1, Remote: "client", Local: "server"})
			base := &g2TraceFakeWriter{header: http.Header{}}
			sentinel := errors.New("retained error")
			if kind == "write_error" {
				base.writeErr = sentinel
			}
			if kind == "flush_error" {
				base.flushErr = sentinel
			}
			ctx := context.WithValue(context.Background(), g2TraceContextKey{}, &g2TraceConn{id: 1})
			at := time.Now().Add(200 * time.Millisecond)
			trace.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := http.NewResponseController(w).SetWriteDeadline(at); err != nil {
					t.Fatal(err)
				}
				w.Header().Set("X-Probe", "retained")
				w.WriteHeader(200)
				n, err := w.Write([]byte("body"))
				if !errors.Is(err, base.writeErr) || (err == nil && n != 4) || (err != nil && n != 0) {
					t.Fatal("write changed")
				}
				if err := http.NewResponseController(w).Flush(); !errors.Is(err, base.flushErr) {
					t.Fatal("flush changed")
				}
			})).ServeHTTP(base, httptest.NewRequest("GET", "/metrics", nil).WithContext(ctx))
			if base.deadlines != 1 || !base.deadline.Equal(at) || base.status != 200 || base.writes != 1 || base.flushes != 1 || base.header.Get("X-Probe") != "retained" {
				t.Fatalf("forwarding changed: %+v", base)
			}
			events, err := trace.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if err = g2ValidateHTTPTrace(events); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestG2CompetitionTraceBoundaries(t *testing.T) {
	for _, kind := range []string{"missing_end", "wrong_connection", "duplicate_entry", "unfinished_write", "overflow", "unpublished"} {
		t.Run(kind, func(t *testing.T) {
			trace := g2NewHTTPTrace()
			trace.add(g2TraceEvent{Action: "connection", Conn: 1, Remote: "client", Local: "server"})
			trace.add(g2TraceEvent{Action: "handler_enter", Conn: 1, Request: 1})
			trace.add(g2TraceEvent{Action: "header", Conn: 1, Request: 1, Status: 200})
			if kind == "unfinished_write" {
				trace.add(g2TraceEvent{Action: "write_enter", Conn: 1, Request: 1, Status: 200, Bytes: 4})
			}
			if kind != "missing_end" {
				trace.add(g2TraceEvent{Action: "handler_exit", Conn: 1, Request: 1, Status: 200})
			}
			if kind == "overflow" {
				trace.next.Store(g2TraceCapacity + 1)
			}
			if kind == "unpublished" {
				trace.next.Add(1)
			}
			events, err := trace.snapshot()
			if kind == "wrong_connection" {
				events[1].Conn = 2
			}
			if kind == "duplicate_entry" {
				events[1].Index = 1
			}
			if err == nil {
				err = g2ValidateHTTPTrace(events)
			}
			if err == nil {
				t.Fatal("invalid trace accepted")
			}
		})
	}
}

func TestG2CompetitionTraceTuples(t *testing.T) {
	for _, kind := range []string{"exact", "unknown_slow", "same_slow", "borrow_slow", "missing_observer", "unclassified"} {
		t.Run(kind, func(t *testing.T) {
			events := []g2TraceEvent{{Action: "connection", Conn: 1, Remote: "a", Local: "server"}, {Action: "connection", Conn: 2, Remote: "b", Local: "server"}, {Action: "connection", Conn: 3, Remote: "c", Local: "server"}}
			slow := []g2TraceTuple{{Local: "a", Remote: "server", AtNS: 1}, {Local: "b", Remote: "server", AtNS: 1}}
			observers := [][]g2TraceTuple{{{Local: "c", Remote: "server", AtNS: 1}}}
			switch kind {
			case "unknown_slow":
				slow[0].Local = "unknown"
			case "same_slow":
				slow[1] = slow[0]
			case "borrow_slow":
				observers[0][0] = slow[0]
			case "missing_observer":
				observers[0] = nil
			case "unclassified":
				events = append(events, g2TraceEvent{Action: "connection", Conn: 4, Remote: "d", Local: "server"})
			}
			err := g2ValidateTraceTuples(events, slow, observers)
			if (err == nil) != (kind == "exact") {
				t.Fatalf("tuple qualification %s: %v", kind, err)
			}
		})
	}
}
