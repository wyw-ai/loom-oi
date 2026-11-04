package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func resourceTestStats() (protocol.OISStats, protocol.OICStats) {
	p := protocol.ProcessStats{Goroutines: 40, OpenFDs: 20, RSSBytes: 1 << 20, HeapBytes: 1 << 19, CPUMillis: 0}
	return protocol.OISStats{Epoch: 1, AccessesOnline: 1, StoreHealthy: true, Process: p},
		protocol.OICStats{AccessID: "lab-a", Connected: true, Registered: true, Epoch: 1, Generation: 1,
			LicenseRemainingMS: 3000, Closed: map[string]int64{}, Process: p}
}

// Missing zero-valued counters must be distinguishable from measured zero.
func TestResourceStatsRequireCompleteJSON(t *testing.T) {
	ois, oic := resourceTestStats()
	for _, role := range []struct {
		name  string
		value any
		out   func() any
	}{
		{"ois", ois, func() any { return &protocol.OISStats{} }},
		{"oic", oic, func() any { return &protocol.OICStats{} }},
	} {
		t.Run(role.name, func(t *testing.T) {
			data, err := json.Marshal(role.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateStatsJSON(data, role.out()); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			for name := range fields {
				t.Run("missing_"+name, func(t *testing.T) {
					var missing map[string]json.RawMessage
					_ = json.Unmarshal(data, &missing)
					delete(missing, name)
					body, _ := json.Marshal(missing)
					if err := validateStatsJSON(body, role.out()); err == nil {
						t.Fatalf("missing %s accepted", name)
					}
				})
				t.Run("null_"+name, func(t *testing.T) {
					var missing map[string]json.RawMessage
					_ = json.Unmarshal(data, &missing)
					missing[name] = json.RawMessage("null")
					body, _ := json.Marshal(missing)
					if err := validateStatsJSON(body, role.out()); err == nil {
						t.Fatalf("null %s accepted", name)
					}
				})
			}
			for _, parent := range []string{"process", "channels"} {
				if parent == "channels" && role.name == "oic" {
					continue
				}
				var nested map[string]json.RawMessage
				if err := json.Unmarshal(fields[parent], &nested); err != nil {
					t.Fatal(err)
				}
				for name := range nested {
					t.Run("missing_"+parent+"_"+name, func(t *testing.T) {
						var outer, inner map[string]json.RawMessage
						_ = json.Unmarshal(data, &outer)
						_ = json.Unmarshal(outer[parent], &inner)
						delete(inner, name)
						outer[parent], _ = json.Marshal(inner)
						body, _ := json.Marshal(outer)
						if err := validateStatsJSON(body, role.out()); err == nil {
							t.Fatalf("missing %s.%s accepted", parent, name)
						}
					})
				}
			}
			for _, body := range []string{"{}", "null", "[]", "{", string(data) + "{}", strings.TrimSuffix(string(data), "}") + `,"unexpected":0}`} {
				if err := validateStatsJSON([]byte(body), role.out()); err == nil {
					t.Errorf("invalid JSON accepted: %s", body)
				}
			}
		})
	}
}

func TestResourceStatsRejectInvalidCounters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*protocol.OISStats, *protocol.OICStats)
	}{
		{"zero_goroutines", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.Goroutines = 0; c.Process.Goroutines = 0 }},
		{"zero_heap", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.HeapBytes = 0; c.Process.HeapBytes = 0 }},
		{"zero_fd", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.OpenFDs = 0; c.Process.OpenFDs = 0 }},
		{"zero_rss", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.RSSBytes = 0; c.Process.RSSBytes = 0 }},
		{"negative_fd", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.OpenFDs = -2; c.Process.OpenFDs = -2 }},
		{"negative_cpu", func(o *protocol.OISStats, c *protocol.OICStats) { o.Process.CPUMillis = -2; c.Process.CPUMillis = -2 }},
		{"negative_channels", func(o *protocol.OISStats, c *protocol.OICStats) { o.Channels.Preparing = -1; c.Channels = -1 }},
		{"negative_relay", func(o *protocol.OISStats, c *protocol.OICStats) { o.RelayBytes = -1; c.RelayBytes = -1 }},
		{"negative_buffer", func(o *protocol.OISStats, c *protocol.OICStats) { o.MaxBufferedBytes = -1; c.MaxBufferedBytes = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ois, oic := resourceTestStats()
			tc.mutate(&ois, &oic)
			for _, value := range []any{ois, oic} {
				data, _ := json.Marshal(value)
				var out any = &protocol.OISStats{}
				if _, ok := value.(protocol.OICStats); ok {
					out = &protocol.OICStats{}
				}
				if err := validateStatsJSON(data, out); err == nil {
					t.Fatalf("invalid counters accepted: %s", data)
				}
			}
		})
	}
	for _, field := range []string{"fd", "rss", "cpu"} {
		t.Run("unavailable_"+field, func(t *testing.T) {
			ois, _ := resourceTestStats()
			switch field {
			case "fd":
				ois.Process.OpenFDs = -1
			case "rss":
				ois.Process.RSSBytes = -1
			case "cpu":
				ois.Process.CPUMillis = -1
			}
			data, _ := json.Marshal(ois)
			err := validateStatsJSON(data, &protocol.OISStats{})
			if (err != nil) != (runtime.GOOS == "linux") {
				t.Fatalf("%s: platform %s, error %v", field, runtime.GOOS, err)
			}
		})
	}
}

func TestResourceStatsHTTPFailures(t *testing.T) {
	ois, _ := resourceTestStats()
	data, _ := json.Marshal(ois)
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"valid", 200, string(data), false},
		{"unauthorized", 401, string(data), true},
		{"unavailable", 503, string(data), true},
		{"empty", 200, "", true},
		{"partial", 200, `{"process":`, true},
		{"oversized", 200, strings.Repeat(" ", 64<<10) + string(data), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			req, _ := http.NewRequest("GET", s.URL, nil)
			var out protocol.OISStats
			err := statsRequest(context.Background(), s.Client(), req, &out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("want error %v, got %v", tc.wantErr, err)
			}
		})
	}
	// The deadline must cancel an in-progress fetch, even if no bytes arrived.
	t.Run("deadline", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer s.Close()
		req, _ := http.NewRequest("GET", s.URL, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := statsRequest(ctx, s.Client(), req, &protocol.OISStats{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want deadline, got %v", err)
		}
	})
	t.Run("connection_refused", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := s.URL
		s.Close()
		req, _ := http.NewRequest("GET", url, nil)
		if err := statsRequest(context.Background(), &http.Client{Timeout: time.Second}, req, &protocol.OISStats{}); err == nil {
			t.Fatal("unreachable statistics endpoint accepted")
		}
	})
}

func TestResourceProcessMustBeAlive(t *testing.T) {
	for _, p := range []*proc{nil, {}, {cmd: &exec.Cmd{}}, {cmd: &exec.Cmd{Process: &os.Process{Pid: 0}}}} {
		if err := processAlive(p); err == nil {
			t.Fatal("missing process accepted")
		}
	}
	p := &proc{name: "exited OIC", cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})}
	if err := processAlive(p); err != nil {
		t.Fatal(err)
	}
	p.err = errors.New("simulated process failure")
	close(p.exited)
	if err := processAlive(p); err == nil {
		t.Fatal("exited process accepted")
	}
}

func resourceTestProfile() (resourceProfile, resourceSnapshot) {
	ois, oic := resourceTestStats()
	base := resourceProfile{ID: "before-failure", Backend: "sqlite", RecordedNS: 20,
		OIS:  resourceProcess[protocol.OISStats]{PID: 1, SampledNS: 10, Stats: ois},
		OICs: map[string]resourceProcess[protocol.OICStats]{"lab-a": {PID: 2, SampledNS: 15, Stats: oic}}}
	sample := resourceSnapshot{StartedNS: 110, EndedNS: 140,
		OIS:  resourceProcess[protocol.OISStats]{PID: 1, SampledNS: 120, Stats: ois},
		OICs: map[string]resourceProcess[protocol.OICStats]{"lab-a": {PID: 2, SampledNS: 130, Stats: oic}}}
	return base, sample
}

func TestResourceSampleExactBounds(t *testing.T) {
	for _, role := range []string{"ois", "oic"} {
		for _, tc := range []struct {
			name                    string
			goroutineDelta, fdDelta int
			want                    bool
		}{
			{"same", 0, 0, true}, {"lower_boundary", -10, 0, true}, {"upper_boundary", 10, 0, true},
			{"below_boundary", -11, 0, false}, {"above_boundary", 11, 0, false},
			{"fd_below", 0, -1, false}, {"fd_above", 0, 1, false},
		} {
			t.Run(role+"_"+tc.name, func(t *testing.T) {
				base, sample := resourceTestProfile()
				if role == "ois" {
					sample.OIS.Stats.Process.Goroutines += tc.goroutineDelta
					sample.OIS.Stats.Process.OpenFDs += tc.fdDelta
				} else {
					x := sample.OICs["lab-a"]
					x.Stats.Process.Goroutines += tc.goroutineDelta
					x.Stats.Process.OpenFDs += tc.fdDelta
					sample.OICs["lab-a"] = x
				}
				ok, err := resourceSampleOK(base, sample, 100, 200)
				if err != nil || ok != tc.want {
					t.Fatalf("want %v, got %v, %v", tc.want, ok, err)
				}
			})
		}
	}
}

func TestResourceSampleRejectsUnsettledOrInvalid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*resourceProfile, *resourceSnapshot)
		invalid bool
	}{
		{"ois_preparing", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats.Channels.Preparing = 1 }, false},
		{"ois_open", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats.Channels.Open = 1 }, false},
		{"ois_closing", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats.Channels.Closing = 1 }, false},
		{"ticket", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats.Tickets = 1 }, false},
		{"control_missing", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats.AccessesOnline = 0 }, false},
		{"oic_channel", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.Channels = 1
			s.OICs["lab-a"] = x
		}, false},
		{"oic_disconnected", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.Connected = false
			s.OICs["lab-a"] = x
		}, false},
		{"oic_unregistered", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.Registered = false
			s.OICs["lab-a"] = x
		}, false},
		{"zero_epoch", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.Epoch = 0
			s.OICs["lab-a"] = x
		}, false},
		{"zero_generation", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.Generation = 0
			s.OICs["lab-a"] = x
		}, false},
		{"missing_oic", func(_ *resourceProfile, s *resourceSnapshot) { delete(s.OICs, "lab-a") }, true},
		{"wrong_access", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.Stats.AccessID = "lab-b"
			s.OICs["lab-a"] = x
		}, true},
		{"missing_pid", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.PID = 0 }, true},
		{"zero_stats", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.Stats = protocol.OISStats{} }, true},
		{"late_baseline", func(b *resourceProfile, _ *resourceSnapshot) { b.RecordedNS = 100 }, true},
		{"late_ois_baseline", func(b *resourceProfile, _ *resourceSnapshot) { b.OIS.SampledNS = 100 }, true},
		{"late_oic_baseline", func(b *resourceProfile, _ *resourceSnapshot) {
			x := b.OICs["lab-a"]
			x.SampledNS = 100
			b.OICs["lab-a"] = x
		}, true},
		{"pre_cleanup", func(_ *resourceProfile, s *resourceSnapshot) { s.StartedNS = 99 }, true},
		{"deadline_exceeded", func(_ *resourceProfile, s *resourceSnapshot) { s.EndedNS = 201 }, true},
		{"reversed_time", func(_ *resourceProfile, s *resourceSnapshot) { s.EndedNS = 109 }, true},
		{"ois_sample_stale", func(_ *resourceProfile, s *resourceSnapshot) { s.OIS.SampledNS = 109 }, true},
		{"oic_sample_outside", func(_ *resourceProfile, s *resourceSnapshot) {
			x := s.OICs["lab-a"]
			x.SampledNS = 141
			s.OICs["lab-a"] = x
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, sample := resourceTestProfile()
			tc.mutate(&base, &sample)
			ok, err := resourceSampleOK(base, sample, 100, 200)
			if ok || (err != nil) != tc.invalid {
				t.Fatalf("want false, invalid=%v; got %v, %v", tc.invalid, ok, err)
			}
		})
	}
}

func TestResourceSampleAllowsRoleReplacementWithoutRebaseline(t *testing.T) {
	base, sample := resourceTestProfile()
	sample.OIS.PID = 11
	x := sample.OICs["lab-a"]
	x.PID = 12
	sample.OICs["lab-a"] = x
	if ok, err := resourceSampleOK(base, sample, 100, 200); !ok || err != nil {
		t.Fatalf("replacement role: %v, %v", ok, err)
	}
	sample.OIS.Stats.Process.OpenFDs++
	if ok, err := resourceSampleOK(base, sample, 100, 200); ok || err != nil {
		t.Fatalf("replacement absorbed leaked FD: %v, %v", ok, err)
	}
}
