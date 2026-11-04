package fixture

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

// HealthRecord 记录真实 HTTP 请求边界；这些时刻不代表 OIC 归约时刻。
type HealthRecord struct {
	ID, Version                                                          uint64
	Status                                                               int
	RequestURI                                                           string
	StartedNS, HeaderStartedNS, HeaderWrittenNS, CancelledNS, FinishedNS int64
	WriteFailed                                                          bool
}
type HealthSnapshot struct {
	PID      int
	Version  uint64
	Workers  int
	Overflow bool
	Records  []HealthRecord
}

// Health 是测试专用、有界、无正文的 HTTP 状态端点。
type Health struct {
	mu               sync.Mutex
	ln               net.Listener
	server           *http.Server
	now              func() int64
	version          uint64
	status           int
	hold             bool
	gate             chan struct{}
	records          []HealthRecord
	workers          int
	overflow, closed bool
	changed          chan struct{}
	done             chan struct{}
}

const healthMaxRecords = 4096

func NewHealth(t testing.TB, clock ...func() int64) *Health {
	t.Helper()
	origin := time.Now()
	now := func() int64 { return time.Since(origin).Nanoseconds() }
	if len(clock) > 0 {
		now = clock[0]
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &Health{ln: ln, now: now, version: 1, status: 200, gate: make(chan struct{}), changed: make(chan struct{}), done: make(chan struct{})}
	h.server = &http.Server{Handler: http.HandlerFunc(h.serve), ReadHeaderTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 8 << 10}
	go func() { defer close(h.done); _ = h.server.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Errorf("health 未回收: %v", err)
		}
	})
	return h
}
func (h *Health) URL() string { return "http://" + h.ln.Addr().String() }
func (h *Health) notify()     { close(h.changed); h.changed = make(chan struct{}) }
func (h *Health) Set(status int, hold bool) uint64 {
	if status < 200 || status > 599 {
		panic("测试 health status 必须为 200..599")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		panic("health 已关闭")
	}
	close(h.gate)
	h.gate = make(chan struct{})
	h.version++
	h.status = status
	h.hold = hold
	h.notify()
	return h.version
}
func (h *Health) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hold && !h.closed {
		h.hold = false
		close(h.gate)
		h.gate = make(chan struct{})
		h.notify()
	}
}
func (h *Health) Snapshot() HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HealthSnapshot{PID: os.Getpid(), Version: h.version, Workers: h.workers, Overflow: h.overflow, Records: append([]HealthRecord(nil), h.records...)}
}

// WaitStarted 只等待实际 handler 已收到的请求，不合成探针开始事件。
func (h *Health) WaitStarted(ctx context.Context, after uint64) (HealthRecord, error) {
	for {
		h.mu.Lock()
		for _, r := range h.records {
			if r.ID > after {
				h.mu.Unlock()
				return r, nil
			}
		}
		changed, closed := h.changed, h.closed
		h.mu.Unlock()
		if closed {
			return HealthRecord{}, errors.New("health 已关闭")
		}
		select {
		case <-ctx.Done():
			return HealthRecord{}, ctx.Err()
		case <-changed:
		}
	}
}
func (h *Health) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.closed || len(h.records) >= healthMaxRecords {
		if !h.closed {
			h.overflow = true
		}
		h.mu.Unlock()
		w.Header().Set("Connection", "close")
		w.WriteHeader(503)
		return
	}
	i := len(h.records)
	rec := HealthRecord{ID: uint64(i + 1), Version: h.version, Status: h.status, StartedNS: h.now(), RequestURI: r.URL.RequestURI()}
	h.records = append(h.records, rec)
	h.workers++
	gate, hold := h.gate, h.hold
	h.notify()
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.records[i].FinishedNS = h.now(); h.workers--; h.notify(); h.mu.Unlock() }()
	if hold {
		select {
		case <-gate:
		case <-r.Context().Done():
			h.mu.Lock()
			h.records[i].CancelledNS = h.now()
			h.notify()
			h.mu.Unlock()
			return
		}
	}
	if r.Context().Err() != nil {
		h.mu.Lock()
		h.records[i].CancelledNS = h.now()
		h.notify()
		h.mu.Unlock()
		return
	}
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Connection", "close")
	h.mu.Lock()
	h.records[i].HeaderStartedNS = h.now()
	h.notify()
	h.mu.Unlock()
	w.WriteHeader(rec.Status)
	err := http.NewResponseController(w).Flush()
	writtenNS := h.now()
	h.mu.Lock()
	if err == nil {
		h.records[i].HeaderWrittenNS = writtenNS
	} else {
		h.records[i].WriteFailed = true
	}
	h.notify()
	h.mu.Unlock()
}
func (h *Health) Close(ctx context.Context) error {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		close(h.gate)
		h.notify()
	}
	h.mu.Unlock()
	_ = h.server.Close()
	select {
	case <-h.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		h.mu.Lock()
		n, changed := h.workers, h.changed
		h.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
