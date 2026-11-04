package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestTVG2_10_SaturatedFairness(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for round := range 3 {
			t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
				ids := []string{"lab-a/http", "lab-a/alt"}
				for i := 2; i < 16; i++ {
					ids = append(ids, fmt.Sprintf("lab-a/saturation-%02d", i))
				}
				g := newG2Env(t, be, "G2-10", g2Options{proxy: true, probes: ids})
				hc := g2MetricsClient(g)
				versions := map[string]uint64{}
				for i, id := range ids {
					versions[id] = g.health[id].Set(200, i >= 2)
				}
				started := resourceNS()
				deadline := time.Now().Add(60 * time.Second)
				samples := 0
				saturated := false
				for {
					res, err := hc.Get("http://" + g.oics["lab-a"].diag + "/stats")
					if err != nil {
						t.Fatal(err)
					}
					var st protocol.OICStats
					raw, readErr := io.ReadAll(io.LimitReader(res.Body, 65537))
					res.Body.Close()
					if readErr != nil || res.StatusCode != 200 || len(raw) > 65536 {
						t.Fatalf("bounded diagnostics failed %d %v", res.StatusCode, readErr)
					}
					if err = json.Unmarshal(raw, &st); err != nil {
						t.Fatal(err)
					}
					o := st.Observation
					if o == nil || o.ProbeJobs != 16 || o.ProbeActive > 2 || o.ProbeQueued > 4 || o.MaxProbeActive > 2 || o.MaxProbeQueued > 4 {
						t.Fatalf("probe bounds: %+v", o)
					}
					if o.ProbeMaxWaitNS > int64(2500*time.Millisecond) || o.ProbeOldestDueNS > int64(2500*time.Millisecond) {
						t.Fatalf("due wait exceeded 2500ms: %+v", o)
					}
					if o.ProbeActive == 2 && o.ProbeQueued == 4 {
						saturated = true
					}
					g.event("scheduler_sample", map[string]any{"round": round, "window_start_ns": started, "sample": samples, "observation": o})
					samples++
					if !time.Now().Before(deadline) {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				ended := resourceNS()
				if ended-started < int64(60*time.Second) || !saturated {
					t.Fatalf("load did not cover full window/full P and Q: %d %v", ended-started, saturated)
				}
				g.settled()
				for i, id := range ids {
					hs := g.health[id].Snapshot()
					if hs.Overflow {
						t.Fatal("health history exhausted")
					}
					executions, timeouts := 0, 0
					for _, r := range hs.Records {
						if r.Version == versions[id] && r.StartedNS >= started && r.StartedNS <= ended {
							executions++
							if r.CancelledNS > r.StartedNS && r.HeaderStartedNS == 0 {
								timeouts++
							}
						}
					}
					if executions == 0 || (i >= 2 && timeouts == 0) {
						t.Fatalf("%s missing actual execution/timeout: %d/%d", id, executions, timeouts)
					}
					g.event("saturation_target", map[string]any{"round": round, "exposure_id": id, "version": versions[id], "timeout_target": i >= 2, "executions": executions, "timeouts": timeouts, "window_start_ns": started, "window_end_ns": ended, "health": hs})
				}
				g.event("saturation_complete", map[string]any{"round": round, "window_start_ns": started, "window_end_ns": ended, "samples": samples, "targets": 16, "timeout_targets": 14, "saturated": saturated})
			})
		}
	})
}
