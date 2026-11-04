// Package metrics provides a fixed, process-local diagnostic registry. It does
// not accept entity identifiers or dynamically register names or label values.
package metrics

import (
	"sync"
	"time"
)

type Counter uint8

const (
	Samples Counter = iota
	SampleExpired
	DirtyCoalesced
	Checks
	OpenBestEffort
	OpenRequireReady
	Exports
	counterCount
)

type Histogram uint8

const (
	ProbeQueueWait Histogram = iota
	ProbeDuration
	QueryRTT
	PublicationDelay
	CheckDuration
	CriticalControlDuration
	histogramCount
)

// DurationBuckets are seconds, independent of configured millisecond values.
// They describe measured distributions, not production SLOs.
func DurationBuckets() []float64 { return append([]float64(nil), durationBounds[:]...) }

var durationBounds = [...]float64{0.001, 0.005, 0.025, 0.1, 0.5, 2, 10}

const maxSeries = 16

type series struct {
	key    string
	labels [2]string
	values [2]string
}

func one(key, label string) series {
	return series{key: key, labels: [2]string{label}, values: [2]string{key}}
}

type descriptor struct {
	name, help string
	series     []series
}

var counters = [...]descriptor{
	{"oi_observation_samples_total", "Explicit health samples created in this process.", []series{one("ready", "result"), one("not_ready", "result")}},
	{"oi_observation_sample_expired_total", "Active samples expired in this process.", []series{{}}},
	{"oi_observation_dirty_coalesced_total", "Dirty notifications merged into an existing pending notification.", []series{{}}},
	{"oi_readiness_checks_total", "Local or final checks classified by a finite terminal reason.", reasonSeries()},
	{"oi_open_best_effort_total", "Best effort Open outcomes by finite result.", openSeries()},
	{"oi_open_require_ready_total", "Require ready Open outcomes by finite result.", openSeries()},
	{"oi_metrics_exports_total", "Diagnostic export outcomes in this process.", exportSeries()},
}
var histograms = [...]descriptor{
	{"oi_probe_queue_wait_seconds", "Time from due marking to an actual worker start.", []series{{}}},
	{"oi_probe_duration_seconds", "Probe attempt time including DNS connection TLS and final headers.", probeSeries()},
	{"oi_observation_query_rtt_seconds", "Full challenge round trip including waiting to send.", resultSeries()},
	{"oi_observation_publication_delay_seconds", "Reduction to visibility delay measured by one monotonic observer.", []series{{}}},
	{"oi_readiness_check_duration_seconds", "Readiness check time by finite result.", resultSeries()},
	{"oi_control_critical_duration_seconds", "Critical control handling time by finite stage.", []series{one("heartbeat", "stage"), one("revoke", "stage"), one("prepare", "stage"), one("check", "stage"), one("register", "stage")}},
}

func probeSeries() []series {
	r := make([]series, 0, 9)
	for _, v := range []string{"matched_ready_status", "matched_not_ready_status", "timeout", "permission_error", "tls_error", "address_denied", "connect_error", "status_unmapped", "malformed_response"} {
		r = append(r, one(v, "reason"))
	}
	return r
}
func reasonSeries() []series {
	r := make([]series, 0, 14)
	for _, v := range []string{"allowed", "not_ready", "readiness_required", "timeout", "unavailable", "observation_changed", "revoked", "revision_changed", "access_offline", "license_expired", "prepare_timeout", "protocol_error", "cancelled", "binding_changed"} {
		r = append(r, one(v, "reason"))
	}
	return r
}
func openSeries() []series {
	r := make([]series, 0, 5)
	for _, result := range []string{"created", "reused", "rejected", "cancelled", "error"} {
		r = append(r, one(result, "result"))
	}
	return r
}
func exportSeries() []series {
	r := make([]series, 0, 7)
	for _, v := range []string{"success", "rejected", "too_large", "timeout", "write_error", "unsupported_writer", "request_rejected"} {
		r = append(r, one(v, "result"))
	}
	return r
}
func resultSeries() []series {
	return []series{one("success", "result"), one("rejected", "result"), one("timeout", "result"), one("cancelled", "result"), one("error", "result")}
}

type distribution struct {
	Buckets [len(durationBounds) + 1]uint64 // noncumulative internal buckets
	Count   uint64
	Sum     float64
}
type Snapshot struct {
	counters   [counterCount][maxSeries]uint64
	histograms [histogramCount][maxSeries]distribution
}
type Registry struct {
	mu   sync.Mutex
	data Snapshot
}

func findSeries(d descriptor, key string) int {
	for i, s := range d.series {
		if s.key == key {
			return i
		}
	}
	return -1
}

// Add accepts only the statically declared key for this counter. Rejected
// input cannot allocate a series, expose a secret label, or change the data.
func (r *Registry) Add(metric Counter, key string) bool {
	if metric >= counterCount {
		return false
	}
	i := findSeries(counters[metric], key)
	if i < 0 {
		return false
	}
	r.mu.Lock()
	r.data.counters[metric][i]++
	r.mu.Unlock()
	return true
}
func (r *Registry) Observe(metric Histogram, key string, d time.Duration) bool {
	if metric >= histogramCount || d < 0 {
		return false
	}
	i := findSeries(histograms[metric], key)
	if i < 0 {
		return false
	}
	seconds := d.Seconds()
	bucket := len(durationBounds)
	for j, b := range durationBounds {
		if seconds <= b {
			bucket = j
			break
		}
	}
	r.mu.Lock()
	h := &r.data.histograms[metric][i]
	h.Buckets[bucket]++
	h.Count++
	h.Sum += seconds
	r.mu.Unlock()
	return true
}
func (r *Registry) Snapshot() Snapshot { r.mu.Lock(); defer r.mu.Unlock(); return r.data }
func (s Snapshot) Counter(metric Counter, key string) (uint64, bool) {
	if metric >= counterCount {
		return 0, false
	}
	i := findSeries(counters[metric], key)
	if i < 0 {
		return 0, false
	}
	return s.counters[metric][i], true
}
func (s Snapshot) Histogram(metric Histogram, key string) (count uint64, sum float64, ok bool) {
	if metric >= histogramCount {
		return 0, 0, false
	}
	i := findSeries(histograms[metric], key)
	if i < 0 {
		return 0, 0, false
	}
	h := s.histograms[metric][i]
	return h.Count, h.Sum, true
}
