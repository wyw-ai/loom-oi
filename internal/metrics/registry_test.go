package metrics

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestMetricsExpositionBounded(t *testing.T) {
	var r Registry
	for metric, d := range counters {
		for _, s := range d.series {
			if !r.Add(Counter(metric), s.key) {
				t.Fatal("declared counter rejected")
			}
		}
	}
	for metric, d := range histograms {
		for _, s := range d.series {
			for _, seconds := range []float64{0, .001, .005, .025, .1, .5, 2, 10, 11} {
				if !r.Observe(Histogram(metric), s.key, time.Duration(seconds*float64(time.Second))) {
					t.Fatal("declared histogram rejected")
				}
			}
		}
	}
	data, err := Render(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxResponseBytes || !utf8.Valid(data) || bytes.ContainsRune(data, '\r') || data[len(data)-1] != '\n' {
		t.Fatal("invalid bounded UTF-8/LF text")
	}
	p := expfmt.NewTextParser(model.LegacyValidation)
	families, err := p.TextToMetricFamilies(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != int(counterCount)+int(histogramCount) {
		t.Fatalf("families=%d", len(families))
	}
	for name, family := range families {
		if family.GetHelp() == "" {
			t.Fatal("missing HELP", name)
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				switch label.GetName() {
				case "result", "reason", "protocol", "stage":
				default:
					t.Fatalf("unbounded label %s", label.GetName())
				}
			}
			if h := metric.GetHistogram(); h != nil {
				if !strings.HasSuffix(name, "_seconds") || h.GetSampleCount() != 9 || math.Abs(h.GetSampleSum()-23.631) > 1e-9 {
					t.Fatalf("wrong unit/count for %s: %v", name, h)
				}
				if len(h.Bucket) != len(durationBounds)+1 {
					t.Fatalf("missing +Inf: %v", h)
				}
				for i, b := range h.Bucket {
					want := uint64(i + 2)
					if i == len(durationBounds) {
						want = 9
					}
					if b.GetCumulativeCount() != want {
						t.Fatalf("bucket %d = %d expected %d", i, b.GetCumulativeCount(), want)
					}
				}
			} else if metric.GetCounter().GetValue() != 1 {
				t.Fatalf("counter %s value", name)
			}
		}
	}
	t.Logf("fixed exposition families=%d bytes=%d", len(families), len(data))
	if full, err := render(r.Snapshot(), len(data)); err != nil || !bytes.Equal(full, data) {
		t.Fatal("exact response boundary rejected")
	}
	if partial, err := render(r.Snapshot(), len(data)-1); err != ErrTooLarge || partial != nil {
		t.Fatal("partial successful exposition")
	}
	if _, err := render(r.Snapshot(), -1); err != ErrTooLarge {
		t.Fatal(err)
	}
	// An arbitrary number of entity-like values cannot create series.
	before := r.Snapshot()
	for i := range 10000 {
		key := fmt.Sprintf("exposure/%d", i)
		if r.Add(Samples, key) || r.Observe(ProbeDuration, key, time.Second) {
			t.Fatal("dynamic label accepted")
		}
	}
	if r.Snapshot() != before {
		t.Fatal("rejected labels mutated registry")
	}
	if r.Add(Counter(255), "") || r.Observe(Histogram(255), "", 0) || r.Observe(QueryRTT, "success", -1) {
		t.Fatal("invalid enum/duration accepted")
	}
	if _, ok := before.Counter(Counter(255), ""); ok {
		t.Fatal("invalid counter")
	}
	if _, ok := before.Counter(Samples, "oops"); ok {
		t.Fatal("invalid key")
	}
	if _, _, ok := before.Histogram(Histogram(255), ""); ok {
		t.Fatal("invalid histogram")
	}
	if _, _, ok := before.Histogram(QueryRTT, "oops"); ok {
		t.Fatal("invalid key")
	}
	if count, sum, ok := before.Histogram(QueryRTT, "success"); !ok || count != 9 || math.Abs(sum-23.631) > 1e-9 {
		t.Fatalf("snapshot: %d %g %v", count, sum, ok)
	}
	var fresh Registry
	if n, ok := fresh.Snapshot().Counter(Samples, "ready"); !ok || n != 0 {
		t.Fatal("counters persisted across registries")
	}
	bounds := DurationBuckets()
	bounds[0] = 99
	if DurationBuckets()[0] != .001 {
		t.Fatal("mutable exported buckets")
	}
}

func TestMetricsEscapingAndConcurrentSnapshot(t *testing.T) {
	value := "quote\" slash\\ newline\nvalue"
	text := "# HELP escape " + escapeHelp(value) + "\n# TYPE escape counter\nescape" + labelText(series{labels: [2]string{"reason"}, values: [2]string{value}}, "") + " 1\n"
	p := expfmt.NewTextParser(model.LegacyValidation)
	parsed, err := p.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if parsed["escape"].GetHelp() != value || parsed["escape"].Metric[0].Label[0].GetValue() != value {
		t.Fatal("escaped value changed")
	}
	var r Registry
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				r.Add(Samples, "ready")
				r.Observe(ProbeQueueWait, "", time.Millisecond)
				_ = r.Snapshot()
			}
		})
	}
	wg.Wait()
	s := r.Snapshot()
	if n, _ := s.Counter(Samples, "ready"); n != 800 {
		t.Fatal(n)
	}
	if n, sum, _ := s.Histogram(ProbeQueueWait, ""); n != 800 || math.Abs(sum-.8) > 1e-10 {
		t.Fatal(n, sum)
	}
}
