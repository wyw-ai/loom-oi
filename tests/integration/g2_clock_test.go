package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

const g2ClockTolerance = int64(100 * time.Millisecond)

type g2ClockSample struct {
	ResourceNS        int64 `json:"resource_ns"`
	RealtimeNS        int64 `json:"realtime_ns"`
	MonotonicNS       int64 `json:"monotonic_ns"`
	MonotonicBeforeNS int64 `json:"monotonic_before_ns"`
	MonotonicAfterNS  int64 `json:"monotonic_after_ns"`
}

type g2Clock struct {
	t             *testing.T
	socket, nonce string
	guardPID      int
	active        int64
	anchor        g2ClockSample
	delta         int64
}

func g2ClockEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("OI_IT_CLOCK") != "1" {
		t.Skip("scope: real CLOCK_REALTIME vectors run only in the dedicated hosted g2-clock job")
	}
	for k, want := range map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted", "GITHUB_JOB": "clock", "GITHUB_REPOSITORY": "wyw-ai/loom-oi"} {
		if os.Getenv(k) != want {
			t.Fatalf("clock workflow guard %s", k)
		}
	}
	g2ClockPlatform(t)
}

func newG2Clock(t *testing.T) *g2Clock {
	t.Helper()
	g2ClockEnabled(t)
	c := &g2Clock{t: t, socket: os.Getenv("OI_CLOCK_SOCKET"), nonce: os.Getenv("OI_CLOCK_NONCE")}
	if c.socket == "" || len(c.nonce) != 64 {
		t.Fatal("missing private clock guard endpoint/nonce")
	}
	r, err := c.call(map[string]any{"action": "witness"})
	if err != nil {
		t.Fatal(err)
	}
	c.guardPID = int(g2ClockInteger(t, r, "guard_pid"))
	if r["active_round"] != nil {
		t.Fatal("clock already shifted at leaf entry")
	}
	t.Cleanup(func() {
		if c.active != 0 {
			r, err := c.call(map[string]any{"action": "restore", "round_id": c.active})
			t.Logf("clock failure cleanup restoration=%v error=%v", r, err)
			if err != nil {
				t.Errorf("clock emergency restore: %v", err)
			}
		}
	})
	return c
}

func (c *g2Clock) call(request map[string]any) (map[string]any, error) {
	request["nonce"] = c.nonce
	conn, err := net.DialTimeout("unix", c.socket, time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, err
	}
	if err = json.NewEncoder(conn).Encode(request); err != nil {
		return nil, err
	}
	line, err := bufio.NewReaderSize(conn, 16384).ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	var response map[string]any
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if err = decoder.Decode(&response); err != nil {
		return nil, err
	}
	if response["ok"] != true {
		return nil, fmt.Errorf("clock guard rejected: %v", response)
	}
	if c.guardPID != 0 && response["guard_pid"] != json.Number(fmt.Sprint(c.guardPID)) {
		return nil, fmt.Errorf("clock guard PID changed")
	}
	if response["peer_pid"] != json.Number(fmt.Sprint(os.Getpid())) {
		return nil, fmt.Errorf("clock guard saw another test process")
	}
	return response, nil
}

func (c *g2Clock) shift(delta time.Duration) map[string]any {
	c.t.Helper()
	before := g2ReadClock(c.t)
	w, err := c.call(map[string]any{"action": "witness"})
	if err != nil {
		c.t.Fatal(err)
	}
	c.active = g2ClockInteger(c.t, w, "completed") + 1
	c.anchor, c.delta = before, int64(delta)
	r, err := c.call(map[string]any{"action": "shift", "round_id": c.active, "delta_ns": int64(delta)})
	if err != nil {
		c.t.Fatal(err)
	}
	after := g2ReadClock(c.t)
	actual := after.RealtimeNS - before.RealtimeNS - after.MonotonicNS + before.MonotonicNS
	if absClock(actual-int64(delta)) > g2ClockTolerance {
		c.t.Fatalf("independent clock step got %dns want %dns", actual, delta)
	}
	return map[string]any{"guard": r, "test_before": before, "test_after": after, "actual_delta_ns": actual, "test_pid": os.Getpid()}
}

func absClock(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func g2ClockInteger(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	n, ok := m[key].(json.Number)
	if !ok {
		t.Fatalf("clock %s is not an integer", key)
	}
	v, err := n.Int64()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (c *g2Clock) witness() map[string]any {
	c.t.Helper()
	s := g2ReadClock(c.t)
	offset := s.RealtimeNS - c.anchor.RealtimeNS - s.MonotonicNS + c.anchor.MonotonicNS
	if absClock(offset-c.delta) > g2ClockTolerance {
		c.t.Fatalf("independent clock offset drift: %d want %d", offset, c.delta)
	}
	r, err := c.call(map[string]any{"action": "witness"})
	if err != nil {
		c.t.Fatal(err)
	}
	return map[string]any{"test": s, "offset_ns": offset, "guard": r}
}

func (c *g2Clock) restore() map[string]any {
	c.t.Helper()
	before := g2ReadClock(c.t)
	r, err := c.call(map[string]any{"action": "restore", "round_id": c.active})
	if err != nil {
		c.t.Fatal(err)
	}
	after := g2ReadClock(c.t)
	offset := after.RealtimeNS - c.anchor.RealtimeNS - after.MonotonicNS + c.anchor.MonotonicNS
	if absClock(offset) > g2ClockTolerance {
		c.t.Fatalf("independent restoration offset %d", offset)
	}
	c.active, c.delta = 0, 0
	return map[string]any{"guard": r, "test_before": before, "test_after": after, "offset_ns": offset}
}
