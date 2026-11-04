package fixture

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestHealthVersionedHeaderGate(t *testing.T) {
	origin := time.Now()
	h := NewHealth(t, func() int64 { return time.Since(origin).Nanoseconds() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version := h.Set(503, true)
	responses := make(chan int, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.URL(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			responses <- 0
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		responses <- resp.StatusCode
	}()
	started, err := h.WaitStarted(ctx, 0)
	if err != nil || started.Version != version || started.Status != 503 || started.HeaderStartedNS != 0 {
		t.Fatal(started, err)
	}
	h.Set(204, false) // 释放旧请求，但不能改写它已固定的原版本和响应状态。
	select {
	case status := <-responses:
		if status != 503 {
			t.Fatal(status)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resp, err := http.Get(h.URL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode)
	}
	if err = h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h.Release() // 关闭后仍可安全取消门闩。
	s := h.Snapshot()
	if s.Workers != 0 || s.Overflow || s.PID <= 0 || len(s.Records) != 2 {
		t.Fatal(s)
	}
	for _, r := range s.Records {
		if r.HeaderStartedNS < r.StartedNS || r.HeaderWrittenNS < r.HeaderStartedNS || r.FinishedNS < r.HeaderWrittenNS || r.WriteFailed || r.CancelledNS != 0 {
			t.Fatal(r)
		}
	}
	s.Records[0].Status = 200
	if h.Snapshot().Records[0].Status != 503 {
		t.Fatal("快照可修改原记录")
	}
}
func TestHealthCancelledAndClosedHoldJoin(t *testing.T) {
	h := NewHealth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h.Set(200, true)
	requestCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, h.URL(), nil)
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	if _, err := h.WaitStarted(ctx, 0); err != nil {
		t.Fatal(err)
	}
	stop()
	if err := <-done; err == nil {
		t.Fatal("取消仍成功")
	}
	// handler 收到真实取消后，等待完成信号再断言无头被伪写。
	for {
		h.mu.Lock()
		workers, changed := h.workers, h.changed
		h.mu.Unlock()
		if workers == 0 {
			break
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	r := h.Snapshot().Records[0]
	if r.CancelledNS < r.StartedNS || r.HeaderStartedNS != 0 || r.HeaderWrittenNS != 0 {
		t.Fatal(r)
	}
	h.Release()
	h.Release()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestHealthCapacityIsExplicitFailure(t *testing.T) {
	h := NewHealth(t)
	h.mu.Lock()
	h.records = make([]HealthRecord, healthMaxRecords)
	h.mu.Unlock()
	resp, err := http.Get(h.URL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 || !h.Snapshot().Overflow || len(h.Snapshot().Records) != healthMaxRecords {
		t.Fatal("容量耗尽被掩盖")
	}
}
