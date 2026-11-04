package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

// This reader deliberately does not import the product's metrics package or
// its definitions. expfmt is the independent exposition implementation; the
// shape checks below come from the adopted export contract, not Render().
type g2Exposition struct {
	Status      int                          `json:"status"`
	Body        string                       `json:"body"`
	ContentType string                       `json:"content_type"`
	StartedNS   int64                        `json:"started_ns"`
	CompletedNS int64                        `json:"completed_ns"`
	Families    map[string]*dto.MetricFamily `json:"-"`
	SeriesKeys  []string                     `json:"series_keys"`
	LabelValues map[string][]string          `json:"label_values"`
}

func g2ReadExposition(ctx context.Context, c *http.Client, addr string) (g2Exposition, error) {
	x := g2Exposition{StartedNS: resourceNS()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		return x, err
	}
	res, err := c.Do(req)
	if err != nil {
		return x, err
	}
	defer res.Body.Close()
	x.Status, x.ContentType = res.StatusCode, res.Header.Get("Content-Type")
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	x.CompletedNS, x.Body = resourceNS(), string(raw)
	if err != nil {
		return x, err
	}
	if len(raw) > 65536 {
		return x, fmt.Errorf("exposition exceeds 64 KiB: %d", len(raw))
	}
	if x.Status != http.StatusOK {
		return x, nil
	}
	media, params, err := mime.ParseMediaType(x.ContentType)
	if err != nil || media != "text/plain" || params["version"] != "0.0.4" || !strings.EqualFold(params["charset"], "utf-8") {
		return x, fmt.Errorf("invalid exposition content type %q", x.ContentType)
	}
	if !utf8.Valid(raw) || bytes.ContainsRune(raw, '\r') || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return x, fmt.Errorf("exposition must be complete UTF-8/LF")
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	x.Families, err = parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return x, fmt.Errorf("independent exposition parser: %w", err)
	}
	if len(x.Families) != 13 {
		return x, fmt.Errorf("metric families=%d, want 13", len(x.Families))
	}
	values := map[string]map[string]bool{}
	seen := map[string]bool{}
	counters, histograms := 0, 0
	add := func(name, labels string) error {
		key := name + "{" + labels + "}"
		if seen[key] {
			return fmt.Errorf("duplicate time series %s", key)
		}
		seen[key] = true
		x.SeriesKeys = append(x.SeriesKeys, key)
		return nil
	}
	for name, family := range x.Families {
		if !strings.HasPrefix(name, "oi_") || family.GetName() != name || family.Type == nil || family.GetHelp() == "" || len(family.Metric) == 0 {
			return x, fmt.Errorf("missing bounded family metadata %q", name)
		}
		switch family.GetType() {
		case dto.MetricType_COUNTER:
			counters++
			if !strings.HasSuffix(name, "_total") {
				return x, fmt.Errorf("counter unit %s", name)
			}
		case dto.MetricType_HISTOGRAM:
			histograms++
			if !strings.HasSuffix(name, "_seconds") {
				return x, fmt.Errorf("duration unit %s", name)
			}
		default:
			return x, fmt.Errorf("unexpected metric type %s", name)
		}
		for _, m := range family.Metric {
			labels := []string{}
			labelSeen := map[string]bool{}
			for _, l := range m.Label {
				key, val := l.GetName(), l.GetValue()
				if labelSeen[key] || !g2FiniteExportLabel(key, val) {
					return x, fmt.Errorf("nonfinite/duplicate label %s=%q", key, val)
				}
				labelSeen[key] = true
				if values[key] == nil {
					values[key] = map[string]bool{}
				}
				values[key][val] = true
				labels = append(labels, key+"="+strconv.Quote(val))
			}
			sort.Strings(labels)
			label := strings.Join(labels, ",")
			if m.TimestampMs != nil {
				return x, fmt.Errorf("unexpected wall-clock sample timestamp")
			}
			if family.GetType() == dto.MetricType_COUNTER {
				if m.Counter == nil || m.Counter.Value == nil || !g2Nonnegative(m.Counter.GetValue()) {
					return x, fmt.Errorf("invalid counter %s", name)
				}
				if err := add(name, label); err != nil {
					return x, err
				}
				continue
			}
			h := m.Histogram
			if h == nil || h.SampleCount == nil || h.SampleSum == nil || !g2Nonnegative(h.GetSampleSum()) || len(h.Bucket) != 8 {
				return x, fmt.Errorf("invalid bounded histogram %s", name)
			}
			lastBound, lastCount := math.Inf(-1), uint64(0)
			for i, b := range h.Bucket {
				bound, count := b.GetUpperBound(), b.GetCumulativeCount()
				if b.UpperBound == nil || b.CumulativeCount == nil || math.IsNaN(bound) || bound < 0 || bound <= lastBound || count < lastCount || count > h.GetSampleCount() || (i == 7 && (!math.IsInf(bound, 1) || count != h.GetSampleCount())) {
					return x, fmt.Errorf("invalid cumulative histogram %s", name)
				}
				bucketLabel := label
				if bucketLabel != "" {
					bucketLabel += ","
				}
				bucketLabel += "le=" + strconv.Quote(strconv.FormatFloat(bound, 'g', -1, 64))
				if err := add(name+"_bucket", bucketLabel); err != nil {
					return x, err
				}
				lastBound, lastCount = bound, count
			}
			if err := add(name+"_sum", label); err != nil {
				return x, err
			}
			if err := add(name+"_count", label); err != nil {
				return x, err
			}
		}
	}
	if counters != 7 || histograms != 6 {
		return x, fmt.Errorf("family types counters=%d histograms=%d", counters, histograms)
	}
	sort.Strings(x.SeriesKeys)
	x.LabelValues = map[string][]string{}
	for key, vals := range values {
		// These are instrument bounds, not additional public enum values.
		// Every later scrape must preserve the exact initial values as well.
		if len(vals) > 32 {
			return x, fmt.Errorf("unbounded label vocabulary %s", key)
		}
		for val := range vals {
			x.LabelValues[key] = append(x.LabelValues[key], val)
		}
		sort.Strings(x.LabelValues[key])
	}
	return x, nil
}

func g2Nonnegative(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 }

// The adopted contract permits only these semantic keys. The initial scrape's
// small, finite snake_case vocabulary is frozen and compared through all real
// entity changes; it is not populated from the product registry's enum table.
func g2FiniteExportLabel(key, val string) bool {
	switch key {
	case "result", "reason", "protocol", "stage":
	default:
		return false
	}
	if len(val) == 0 || len(val) > 32 {
		return false
	}
	for _, r := range val {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func g2ExpositionCounter(x g2Exposition, name string, labels map[string]string) float64 {
	family := x.Families[name]
	if family == nil || family.GetType() != dto.MetricType_COUNTER {
		return math.NaN()
	}
	for _, m := range family.Metric {
		if len(m.Label) != len(labels) {
			continue
		}
		match := true
		for _, l := range m.Label {
			if labels[l.GetName()] != l.GetValue() {
				match = false
			}
		}
		if match {
			return m.Counter.GetValue()
		}
	}
	return math.NaN()
}

type g2SlowWrite struct {
	Socket       int    `json:"socket"`
	ConnectedNS  int64  `json:"connected_ns"`
	StartedNS    int64  `json:"started_ns"`
	CompletedNS  int64  `json:"completed_ns"`
	PlannedBytes int    `json:"planned_bytes"`
	WrittenBytes int    `json:"written_bytes"`
	Error        string `json:"error"`
}

type g2SlowScrapes struct {
	mu     sync.Mutex
	conns  []*net.TCPConn
	writes []g2SlowWrite
	done   chan struct{}
	once   sync.Once
}

// Two actual HTTP streams, not idle sockets. Writes have a shared one-second
// absolute deadline, each stream contains at most 2048 requests, and no response
// bytes are consumed. An observed 429 or export failure is still required by
// the caller: these inputs alone do not prove either exporter slot was blocked.
func g2BeginSlowScrapes(ctx context.Context, addr string) (*g2SlowScrapes, error) {
	s := &g2SlowScrapes{done: make(chan struct{})}
	deadline := time.Now().Add(time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for i := 0; i < 2; i++ {
		c, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
		if err != nil {
			for _, old := range s.conns {
				_ = old.Close()
			}
			return nil, err
		}
		tcp := c.(*net.TCPConn)
		if err := tcp.SetReadBuffer(1024); err != nil {
			_ = tcp.Close()
			for _, old := range s.conns {
				_ = old.Close()
			}
			return nil, err
		}
		if err := tcp.SetWriteDeadline(deadline); err != nil {
			_ = tcp.Close()
			for _, old := range s.conns {
				_ = old.Close()
			}
			return nil, err
		}
		s.conns = append(s.conns, tcp)
		s.writes = append(s.writes, g2SlowWrite{Socket: i, ConnectedNS: resourceNS()})
	}
	payload := strings.Repeat("GET /metrics HTTP/1.1\r\nHost: "+addr+"\r\nConnection: keep-alive\r\n\r\n", 2048)
	var wg sync.WaitGroup
	for i, c := range s.conns {
		wg.Add(1)
		go func(i int, c *net.TCPConn) {
			defer wg.Done()
			s.mu.Lock()
			s.writes[i].StartedNS = resourceNS()
			s.writes[i].PlannedBytes = len(payload)
			s.mu.Unlock()
			n, err := io.Copy(c, strings.NewReader(payload))
			s.mu.Lock()
			defer s.mu.Unlock()
			s.writes[i].CompletedNS = resourceNS()
			s.writes[i].WrittenBytes = int(n)
			if err != nil {
				s.writes[i].Error = err.Error()
			}
		}(i, c)
	}
	go func() { wg.Wait(); close(s.done) }()
	return s, nil
}

func (s *g2SlowScrapes) Snapshot() []g2SlowWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]g2SlowWrite(nil), s.writes...)
}

func (s *g2SlowScrapes) Close() error {
	s.once.Do(func() {
		for _, c := range s.conns {
			_ = c.Close()
		}
	})
	select {
	case <-s.done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("slow scrape writers did not stop within 2s")
	}
}

func g2ExporterAddresses(g *g2Env) map[string]string {
	line, _ := g.ois.waitLog(g.t, time.Second, "ois started", 0)
	addr, ok := line["diag"].(string)
	if !ok || addr == "" {
		g.t.Fatal("OIS diagnostic endpoint absent")
	}
	return map[string]string{"ois": addr, "oic": g.oics["lab-a"].diag}
}

func g2ExporterRead(t *testing.T, hc *http.Client, addr string) g2Exposition {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	x, err := g2ReadExposition(ctx, hc, addr)
	if err != nil || x.Status != 200 {
		t.Fatalf("real exporter status=%d: %v", x.Status, err)
	}
	return x
}

func TestTVG2_12_ExporterEntityCardinality(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "TV-G2-12", g2Options{})
		hc := g2MetricsClient(g)
		addresses := g2ExporterAddresses(g)
		initial := map[string]g2Exposition{}
		for role, addr := range addresses {
			initial[role] = g2ExporterRead(t, hc, addr)
			g.event("export_initial", map[string]any{"role": role, "exposition": initial[role]})
		}
		original := append([]any(nil), g.oics["lab-a"].cfg["exposures"].([]any)...)
		channels, targets, subjects := map[string]bool{}, map[string]bool{}, map[string]bool{}
		previousRevision := ""
		for i := 0; i < 32; i++ {
			name := fmt.Sprintf("entity-%02d", i)
			target := fixture.New(t, "target-"+name, fixture.Identify)
			subject := "subject-" + name
			// A stable exposure name isolates target/subject/channel cardinality
			// without confounding the separate bounded tombstone/reconnect test.
			id := "lab-a/export"
			g.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "policy-" + name, Subject: subject, ExposureSelector: id, Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
			g.rewriteOIC("lab-a", append(append([]any(nil), original...), exposure("export", target.Addr(), protocol.AdmissionBestEffort, nil)))
			var revision string
			g.eventually(5*time.Second, "actual target registration", func() bool {
				views, err := g.consumer(subject).DiscoverAll(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range views {
					if v.ExposureID == id && v.AccessOnline {
						revision = v.Revision
						return revision != "" && revision != previousRevision
					}
				}
				return false
			})
			previousRevision = revision
			payload := "native-" + name
			got, ch := request(t, g.consumer(subject), id, payload)
			if got != target.Name+":"+payload || channels[ch] || targets[target.Addr()] || subjects[subject] {
				t.Fatalf("entity/native exchange not unique: %q %q", got, ch)
			}
			r := waitTarget(t, target, 0)
			if string(r.Data) != payload || !r.EOF || r.Reset || !r.Done {
				t.Fatalf("native target outcome %+v", r)
			}
			channels[ch], targets[target.Addr()], subjects[subject] = true, true, true
			g.event("export_entity", map[string]any{"index": i, "target": target.Addr(), "subject": subject, "channel_id": ch, "exposure_id": id, "revision": revision, "request": payload, "response": got, "target_record": r})
			for role, addr := range addresses {
				x := g2ExporterRead(t, hc, addr)
				if !reflect.DeepEqual(initial[role].SeriesKeys, x.SeriesKeys) || !reflect.DeepEqual(initial[role].LabelValues, x.LabelValues) {
					t.Fatalf("%s labels/series grew with real entities", role)
				}
				for _, entity := range []string{target.Addr(), subject, ch, id, g.token(subject)} {
					if strings.Contains(x.Body, entity) {
						t.Fatalf("%s exporter leaked entity/credential", role)
					}
				}
				g.event("export_after_entity", map[string]any{"index": i, "role": role, "exposition": x})
			}
		}
		g.settled()
	})
}

func TestTVG2_12_ExporterSlowReaders(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "TV-G2-12", g2Options{})
		hc := g2MetricsClient(g)
		for role, addr := range g2ExporterAddresses(g) {
			for round := 0; round < 20; round++ {
				before := g2ExporterRead(t, hc, addr)
				slow, err := g2BeginSlowScrapes(context.Background(), addr)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = slow.Close() })
				statuses := []g2Exposition{}
				stop := time.Now().Add(time.Second)
				rejected := false
				for time.Now().Before(stop) {
					ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
					x, err := g2ReadExposition(ctx, hc, addr)
					cancel()
					if err != nil {
						_ = slow.Close()
						t.Fatal(err)
					}
					statuses = append(statuses, x)
					if x.Status == 429 {
						rejected = true
						break
					}
					if x.Status != 200 {
						_ = slow.Close()
						t.Fatalf("unexpected slow scrape status %d", x.Status)
					}
					time.Sleep(10 * time.Millisecond)
				}
				// Native business work must not wait for the two slow network writes.
				nativeStart := resourceNS()
				got, ch := request(t, g.consumer("alice"), "lab-a/http", "export-concurrent")
				nativeEnd := resourceNS()
				if got != "T1:export-concurrent" {
					_ = slow.Close()
					t.Fatal(got)
				}
				failures := func(x g2Exposition) float64 {
					return g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": "timeout"}) + g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": "write_error"})
				}
				// Observe failure before this instrument closes either socket. A
				// forced client close alone would not demonstrate the write bound.
				var boundedFailure g2Exposition
				for time.Now().Before(stop) {
					ctx, cancel := context.WithDeadline(context.Background(), stop)
					x, readErr := g2ReadExposition(ctx, hc, addr)
					cancel()
					if readErr != nil {
						_ = slow.Close()
						t.Fatal(readErr)
					}
					if x.Status != 200 && x.Status != 429 {
						_ = slow.Close()
						t.Fatalf("export failure poll status %d", x.Status)
					}
					// Both slow sockets must finish by the export bound before
					// Close. This also prevents a previous round's late second
					// failure from being misattributed to this round.
					if x.Status == 200 && failures(x)-failures(before) >= 2 {
						boundedFailure = x
						break
					}
					if x.Status == 429 {
						rejected = true
					}
					time.Sleep(10 * time.Millisecond)
				}
				closeStart := resourceNS()
				if err := slow.Close(); err != nil {
					t.Fatal(err)
				}
				var after g2Exposition
				deadline := time.Now().Add(2 * time.Second)
				for {
					ctx, cancel := context.WithDeadline(context.Background(), deadline)
					after, err = g2ReadExposition(ctx, hc, addr)
					cancel()
					if err != nil {
						t.Fatal(err)
					}
					if after.Status == 200 {
						break
					}
					if after.Status != 429 || time.Now().After(deadline) {
						t.Fatalf("exporter did not recover: %d", after.Status)
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !rejected || boundedFailure.Status != 200 || failures(after)-failures(before) < 2 {
					t.Fatalf("slow inputs did not prove rejection AND write failure: rejected=%v failures %v -> %v", rejected, failures(before), failures(after))
				}
				if after.CompletedNS-closeStart > int64(2*time.Second) || !reflect.DeepEqual(before.SeriesKeys, after.SeriesKeys) {
					t.Fatal("recovery budget/cardinality changed")
				}
				g.event("export_slow_round", map[string]any{"role": role, "round": round, "read_buffer_requested": 1024, "requests_per_socket": 2048, "write_budget_ms": 1000, "slow_writes": slow.Snapshot(), "before": before, "polls": statuses, "bounded_failure_before_close": boundedFailure, "after": after, "close_started_ns": closeStart, "native_started_ns": nativeStart, "native_finished_ns": nativeEnd, "channel_id": ch, "native_response": got})
			}
		}
		g.settled()
	})
}
