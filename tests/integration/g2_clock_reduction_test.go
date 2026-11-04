package integration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// These helpers observe a real OIS scrape. No product clock or TTL is changed.
type g2ClockQueryReduction struct {
	QueryID    string         `json:"query_id"`
	DeadlineNS int64          `json:"deadline_ns"`
	Before     g2Exposition   `json:"before"`
	Polls      []g2Exposition `json:"polls"`
}

func g2ClockQueryCount(x g2Exposition) (uint64, error) {
	const name = "oi_observation_query_rtt_seconds"
	family := x.Families[name]
	if x.Status != 200 || family == nil || family.GetType() != dto.MetricType_HISTOGRAM {
		return 0, fmt.Errorf("missing successful OIS query RTT exposition")
	}
	var count uint64
	found := false
	for _, metric := range family.Metric {
		if len(metric.Label) != 1 || metric.Label[0].GetName() != "result" || metric.Label[0].GetValue() != "success" {
			continue
		}
		if found || metric.Histogram == nil || metric.Histogram.SampleCount == nil {
			return 0, fmt.Errorf("invalid/duplicate successful query RTT count")
		}
		count, found = metric.Histogram.GetSampleCount(), true
	}
	if !found {
		return 0, fmt.Errorf("successful query RTT count absent")
	}
	return count, nil
}

func g2ClockAwaitReduction(ctx context.Context, r *g2ClockQueryReduction, read func(context.Context) (g2Exposition, error)) error {
	before, err := g2ClockQueryCount(r.Before)
	if err != nil {
		return err
	}
	if before == math.MaxUint64 {
		return fmt.Errorf("query count cannot advance exactly once")
	}
	for len(r.Polls) < 51 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if resourceNS() >= r.DeadlineNS {
			return context.DeadlineExceeded
		}
		x, err := read(ctx)
		r.Polls = append(r.Polls, x) // retain every raw scrape, including failures
		if err != nil {
			return err
		}
		if x.CompletedNS > r.DeadlineNS {
			return context.DeadlineExceeded
		}
		current, err := g2ClockQueryCount(x)
		if err != nil {
			return err
		}
		if current < before || current > before+1 {
			return fmt.Errorf("query RTT count changed by other than exactly one: %d -> %d", before, current)
		}
		if current == before+1 {
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
	return fmt.Errorf("bounded query reduction scrape count exhausted")
}

func g2ClockAuthorityWindow(start, end, remaining int64) (int64, int64) {
	return start + (remaining-1)*int64(time.Millisecond), end + remaining*int64(time.Millisecond)
}

func clockCountExposition(count uint64) g2Exposition {
	name, key, value, kind := "oi_observation_query_rtt_seconds", "result", "success", dto.MetricType_HISTOGRAM
	return g2Exposition{Status: 200, StartedNS: resourceNS(), CompletedNS: resourceNS(), Families: map[string]*dto.MetricFamily{name: {Name: &name, Type: &kind,
		Metric: []*dto.Metric{{Label: []*dto.LabelPair{{Name: &key, Value: &value}}, Histogram: &dto.Histogram{SampleCount: &count}}}}}}
}

func TestG2ClockQueryReductionRequiresExactCompletedIncrement(t *testing.T) {
	for _, test := range []struct {
		name      string
		values    []uint64
		wantError bool
	}{
		{"unchanged_then_exact", []uint64{5, 6}, false}, {"skip", []uint64{7}, true}, {"reset", []uint64{4}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &g2ClockQueryReduction{Before: clockCountExposition(5), DeadlineNS: resourceNS() + int64(500*time.Millisecond)}
			ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(r.DeadlineNS)))
			defer cancel()
			n := 0
			err := g2ClockAwaitReduction(ctx, r, func(context.Context) (g2Exposition, error) {
				x := clockCountExposition(test.values[n])
				n++
				return x, nil
			})
			if (err != nil) != test.wantError || len(r.Polls) != len(test.values) {
				t.Fatalf("err=%v trace=%d", err, len(r.Polls))
			}
		})
	}
}

func TestG2ClockQueryReductionKeepsDeadlineAndFailedScrape(t *testing.T) {
	for _, mode := range []string{"context", "late_complete", "read_error", "http_error", "missing_series", "duplicate_series"} {
		t.Run(mode, func(t *testing.T) {
			r := &g2ClockQueryReduction{Before: clockCountExposition(5), DeadlineNS: resourceNS() + int64(20*time.Millisecond)}
			ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(r.DeadlineNS)))
			defer cancel()
			err := g2ClockAwaitReduction(ctx, r, func(c context.Context) (g2Exposition, error) {
				x := clockCountExposition(6)
				switch mode {
				case "context":
					<-c.Done()
					x.CompletedNS = resourceNS()
					return x, c.Err()
				case "late_complete":
					x.CompletedNS = r.DeadlineNS + 1
				case "read_error":
					return x, errors.New("scrape failed")
				case "http_error":
					x.Status = 429
				case "missing_series":
					x.Families = nil
				case "duplicate_series":
					f := x.Families["oi_observation_query_rtt_seconds"]
					f.Metric = append(f.Metric, f.Metric[0])
				}
				return x, nil
			})
			if err == nil || len(r.Polls) != 1 {
				t.Fatalf("err=%v trace=%d", err, len(r.Polls))
			}
		})
	}
}

func TestG2ClockAuthorityWindowFlooredZero(t *testing.T) {
	const unit = int64(time.Millisecond)
	// Exhaust sub-millisecond representation positions and query brackets.
	for _, remaining := range []int64{1, 2, 200, 950} {
		for fraction := int64(0); fraction < unit; fraction += 137 {
			at := int64(10)*unit + fraction
			deadline := at + remaining*unit + fraction
			endpoint := deadline - unit
			lower, upper := g2ClockAuthorityWindow(at-321, at+654, remaining)
			if endpoint < lower || endpoint >= upper {
				t.Fatalf("endpoint=%d outside [%d,%d)", endpoint, lower, upper)
			}
			// directoryView returns Unknown as soon as duration/ms is zero.
			if (deadline-(endpoint+1))/unit != 0 || (deadline-(endpoint-1))/unit < 1 {
				t.Fatal("floor boundary moved")
			}
		}
	}
}
