package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type g2ExportSlotObservation struct {
	StartedNS   int64                `json:"started_ns"`
	CompletedNS int64                `json:"completed_ns"`
	HTTPStatus  int                  `json:"http_status"`
	Process     resourceProcess[any] `json:"process"`
}

type g2ExportSlotInput struct {
	Schema            string                    `json:"schema"`
	OriginNS          int64                     `json:"origin_ns"`
	DeadlineNS        int64                     `json:"deadline_ns"`
	Zero              g2ExportSlotObservation   `json:"zero"`
	WritersReleasedNS int64                     `json:"writers_released_ns"`
	Samples           []g2ExportSlotObservation `json:"samples"`
}

func g2ReadExportSlots(ctx context.Context, g *g2Env, hc *http.Client, role string) (x g2ExportSlotObservation, d *protocol.ObservationRuntimeStats, err error) {
	x.StartedNS = resourceNS()
	p, addr, client := g.ois, "https://"+g.api+"/v1/admin/stats", g.stats
	if role == "oic" {
		p, addr, client = g.oics["lab-a"].proc, "http://"+g.oics["lab-a"].diag+"/stats", hc
	} else if role != "ois" {
		return x, nil, fmt.Errorf("unknown exporter role")
	}
	if err = processAlive(p); err != nil {
		return
	}
	x.Process.PID = p.cmd.Process.Pid
	defer func() { x.CompletedNS = resourceNS() }()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if e != nil {
		return x, nil, e
	}
	if role == "ois" {
		req.Header.Set("Authorization", "Bearer "+g.adminTok)
	}
	res, e := client.Do(req)
	if e != nil {
		return x, nil, e
	}
	defer res.Body.Close()
	x.HTTPStatus = res.StatusCode
	raw, e := io.ReadAll(io.LimitReader(res.Body, 65537))
	if e != nil {
		return x, nil, e
	}
	if res.StatusCode != 200 || len(raw) > 65536 {
		return x, nil, fmt.Errorf("export activity stats status/size invalid")
	}
	var fields struct {
		Observation map[string]json.RawMessage `json:"observation"`
	}
	if e = json.Unmarshal(raw, &fields); e != nil {
		return x, nil, e
	}
	if e = requiredFields(fields.Observation, "exporter_active", "exporter_youngest_active_age_ns"); e != nil {
		return x, nil, e
	}
	if role == "ois" {
		var stats protocol.OISStats
		if e = validateStatsJSON(raw, &stats); e != nil {
			return x, nil, e
		}
		x.Process.Stats = stats
		d = stats.Observation
	} else {
		var stats protocol.OICStats
		if e = validateStatsJSON(raw, &stats); e != nil {
			return x, nil, e
		}
		if stats.AccessID != "lab-a" {
			return x, nil, fmt.Errorf("activity access mismatch")
		}
		x.Process.Stats = stats
		d = stats.Observation
	}
	x.Process.SampledNS = resourceNS()
	if d == nil || d.ExporterActive < 0 || d.ExporterActive > 2 || d.ExporterYoungestActiveAgeNS < 0 || (d.ExporterActive == 0 && d.ExporterYoungestActiveAgeNS != 0) {
		return x, d, fmt.Errorf("invalid exporter activity")
	}
	err = processAlive(p)
	return
}

func (x *g2ExportSlotInput) beforeWrite(ctx context.Context, g *g2Env, hc *http.Client, role string, s *g2SlowScrapes) error {
	w := s.Snapshot()
	if len(w) != 2 {
		return fmt.Errorf("missing slow connections")
	}
	x.Schema = "g2-export-slot-input-v1"
	x.OriginNS = min(w[0].ConnectedNS, w[1].ConnectedNS)
	x.DeadlineNS = x.OriginNS + int64(time.Second)
	x.Samples = make([]g2ExportSlotObservation, 0)
	var d *protocol.ObservationRuntimeStats
	var err error
	x.Zero, d, err = g2ReadExportSlots(ctx, g, hc, role)
	if err != nil {
		return err
	}
	if d.ExporterActive != 0 || d.ExporterYoungestActiveAgeNS != 0 {
		return fmt.Errorf("exporter not empty before request writers")
	}
	x.WritersReleasedNS = resourceNS()
	if x.WritersReleasedNS > x.DeadlineNS || ctx.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func (x *g2ExportSlotInput) awaitActive(ctx context.Context, g *g2Env, hc *http.Client, role string) error {
	for len(x.Samples) < 128 {
		s, d, err := g2ReadExportSlots(ctx, g, hc, role)
		if err != nil {
			x.Samples = append(x.Samples, s)
			return err
		}
		ready, err := x.observe(s, d)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("export activity sample capacity exhausted")
}

func (x *g2ExportSlotInput) observe(s g2ExportSlotObservation, d *protocol.ObservationRuntimeStats) (bool, error) {
	if len(x.Samples) >= 128 {
		return false, fmt.Errorf("activity observation capacity exhausted")
	}
	previous := x.WritersReleasedNS
	if len(x.Samples) > 0 {
		previous = x.Samples[len(x.Samples)-1].CompletedNS
	}
	x.Samples = append(x.Samples, s)
	if s.HTTPStatus != 200 || s.Process.PID <= 0 || s.Process.PID != x.Zero.Process.PID || s.StartedNS <= previous || s.CompletedNS < s.StartedNS || s.CompletedNS > x.DeadlineNS || s.Process.SampledNS < s.StartedNS || s.Process.SampledNS > s.CompletedNS {
		return false, fmt.Errorf("activity identity/order/deadline invalid")
	}
	if d == nil || d.ExporterActive < 0 || d.ExporterActive > 2 || d.ExporterYoungestActiveAgeNS < 0 || d.ExporterYoungestActiveAgeNS > s.CompletedNS-x.WritersReleasedNS || (d.ExporterActive == 0 && d.ExporterYoungestActiveAgeNS != 0) {
		return false, fmt.Errorf("activity snapshot invalid")
	}
	return d.ExporterActive == 2 && d.ExporterYoungestActiveAgeNS >= int64(40*time.Millisecond), nil
}

func TestG2CompetitionActiveEligibility(t *testing.T) {
	for _, mode := range []string{"exact_40ms", "too_young", "one_owner", "empty", "wrong_pid", "wrong_status", "overlap", "unfinished", "after_deadline", "outside_capture", "negative_age", "age_before_release", "empty_nonzero_age", "excess_owners", "missing_diagnostics", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			x := g2ExportSlotInput{OriginNS: 1, DeadlineNS: int64(time.Second), WritersReleasedNS: 10, Zero: g2ExportSlotObservation{Process: resourceProcess[any]{PID: 123}}}
			s := g2ExportSlotObservation{StartedNS: int64(50 * time.Millisecond), CompletedNS: int64(51 * time.Millisecond), HTTPStatus: 200, Process: resourceProcess[any]{PID: 123, SampledNS: int64(51 * time.Millisecond)}}
			d := &protocol.ObservationRuntimeStats{ExporterActive: 2, ExporterYoungestActiveAgeNS: int64(40 * time.Millisecond)}
			bad := true
			switch mode {
			case "exact_40ms":
				bad = false
			case "too_young":
				d.ExporterYoungestActiveAgeNS--
				bad = false
			case "one_owner":
				d.ExporterActive = 1
				bad = false
			case "empty":
				d.ExporterActive = 0
				d.ExporterYoungestActiveAgeNS = 0
				bad = false
			case "wrong_pid":
				s.Process.PID++
			case "wrong_status":
				s.HTTPStatus = 429
			case "overlap":
				s.StartedNS = x.WritersReleasedNS
			case "unfinished":
				s.CompletedNS = 0
			case "after_deadline":
				s.CompletedNS = x.DeadlineNS + 1
			case "outside_capture":
				s.Process.SampledNS = s.CompletedNS + 1
			case "negative_age":
				d.ExporterYoungestActiveAgeNS = -1
			case "age_before_release":
				d.ExporterYoungestActiveAgeNS = s.CompletedNS
			case "empty_nonzero_age":
				d.ExporterActive = 0
			case "excess_owners":
				d.ExporterActive = 3
			case "missing_diagnostics":
				d = nil
			case "overflow":
				x.Samples = make([]g2ExportSlotObservation, 128)
			}
			ready, err := x.observe(s, d)
			if (err != nil) != bad || ready != (mode == "exact_40ms") {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
		})
	}
}

// This small instrument exercises the real process endpoints and bounded input.
// It does not run, substitute for, or confer a pass on the 60-second vector.
func TestG2CompetitionActiveInput(t *testing.T) {
	if os.Getenv("OI_G2_ACTIVE_INPUT") != "1" {
		t.Skip("development-only bounded Active input: set OI_G2_ACTIVE_INPUT=1; not the formal 60s vector")
	}
	g := newG2Env(t, "sqlite", "TV-G2-FIXTURE", g2Options{probes: []string{"lab-a/http"}})
	hc := g2MetricsClient(g)
	addresses := g2ExporterAddresses(g)
	for _, role := range []string{"ois", "oic"} {
		t.Run(role, func(t *testing.T) {
			x := g2ExportSlotInput{}
			before := g2ExporterRead(t, hc, addresses[role])
			s, err := g2BeginSlowScrapesBeforeWrite(context.Background(), addresses[role], g2CompetitionDialer(), func(ctx context.Context, s *g2SlowScrapes) error { return x.beforeWrite(ctx, g, hc, role, s) })
			var headers []g2CompetitionResponseHeader
			var last g2Exposition
			gate := g2CompetitionRejectionGate{deadlineNS: x.DeadlineNS}
			defer func() {
				fields := map[string]any{"role": role, "export_slot_input": x, "slow_response_headers": headers, "export_input_observations": gate.observations, "last_scrape": last, "before": before}
				if s != nil {
					fields["slow_writes"] = s.Snapshot()
					fields["close_started_ns"] = resourceNS()
					if closeErr := s.Close(); closeErr != nil {
						t.Error(closeErr)
					}
				}
				fields["kind"], fields["test"] = "G2-active-input", t.Name()
				b, marshalErr := json.Marshal(fields)
				if marshalErr != nil {
					t.Error(marshalErr)
				} else {
					t.Logf("OBS %s", b)
				}
			}()
			if err != nil {
				t.Fatal(err)
			}
			deadline := resourceOrigin.Add(time.Duration(x.DeadlineNS))
			if err = g2CompetitionAwaitWrites(s, deadline); err != nil {
				t.Fatal(err)
			}
			headers, err = g2CompetitionPeekHeaders(s, deadline)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			if err = x.awaitActive(ctx, g, hc, role); err != nil {
				t.Fatal(err)
			}
			for {
				last, err = g2ReadExposition(ctx, hc, addresses[role])
				if err != nil {
					t.Fatal(err)
				}
				ok, e := gate.observe(last)
				if e != nil {
					t.Fatal(e)
				}
				if ok {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			for {
				last, err = g2ReadExposition(ctx, hc, addresses[role])
				if err != nil {
					t.Fatal(err)
				}
				if last.Status == 200 && g2CompetitionExportFailures(last)-g2CompetitionExportFailures(before) >= 2 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
	g.settled()
}
