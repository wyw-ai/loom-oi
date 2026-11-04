package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const resourceSchema = "b4-resource-v2"

var resourceOrigin = time.Now()
var resourceIDs atomic.Uint64

func resourceNS() int64               { return time.Since(resourceOrigin).Nanoseconds() }
func resourceID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, resourceIDs.Add(1)) }

type resourceProcess[T any] struct {
	PID       int   `json:"pid"`
	SampledNS int64 `json:"sampled_ns"`
	Stats     T     `json:"stats"`
}

type resourceSnapshot struct {
	StartedNS int64                                         `json:"started_ns"`
	EndedNS   int64                                         `json:"ended_ns"`
	OIS       resourceProcess[protocol.OISStats]            `json:"ois"`
	OICs      map[string]resourceProcess[protocol.OICStats] `json:"oics"`
}

type resourceProfile struct {
	ID         string
	Backend    string
	RecordedNS int64
	OIS        resourceProcess[protocol.OISStats]
	OICs       map[string]resourceProcess[protocol.OICStats]
}

type oisResourceBaseline struct {
	process resourceProcess[protocol.OISStats]
	backend string
}

func processAlive(p *proc) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.cmd.Process.Pid <= 0 {
		return fmt.Errorf("expected live process is missing")
	}
	select {
	case <-p.exited:
		return fmt.Errorf("%s exited unexpectedly: %v", p.name, p.err)
	default:
		return nil
	}
}

// statsRequest never turns a failed or incomplete observation into zero.
// The client timeout also bounds callers outside the resource window.
func statsRequest(ctx context.Context, c *http.Client, req *http.Request, out any) error {
	res, err := c.Do(req.Clone(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("statistics HTTP status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("statistics body exceeds 64 KiB")
	}
	return validateStatsJSON(data, out)
}

func requiredFields(m map[string]json.RawMessage, keys ...string) error {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || string(v) == "null" {
			return fmt.Errorf("statistics missing %s", key)
		}
	}
	return nil
}

func validateStatsJSON(data []byte, out any) error {
	if err := protocol.DecodeStrictBytes(data, out); err != nil {
		return err
	}
	var m, process map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if err := requiredFields(m, "process", "channels", "relay_bytes", "max_buffered_bytes", "epoch"); err != nil {
		return err
	}
	if err := json.Unmarshal(m["process"], &process); err != nil {
		return err
	}
	if err := requiredFields(process, "goroutines", "open_fds", "rss_bytes", "heap_bytes", "cpu_ms"); err != nil {
		return err
	}
	switch s := out.(type) {
	case *protocol.OISStats:
		if err := requiredFields(m, "tickets", "accesses_online", "store_healthy"); err != nil {
			return err
		}
		var channels map[string]json.RawMessage
		if err := json.Unmarshal(m["channels"], &channels); err != nil {
			return err
		}
		if err := requiredFields(channels, "preparing", "open", "closing", "closed"); err != nil {
			return err
		}
		return validOISStats(*s)
	case *protocol.OICStats:
		if err := requiredFields(m, "access_id", "connected", "registered", "generation", "license_remaining_ms", "closed"); err != nil {
			return err
		}
		return validOICStats(*s)
	default:
		return fmt.Errorf("unsupported statistics type")
	}
}

func validProcessStats(s protocol.ProcessStats) error {
	if s.Goroutines <= 0 || s.HeapBytes <= 0 || s.OpenFDs < -1 || s.RSSBytes < -1 || s.CPUMillis < -1 {
		return fmt.Errorf("invalid process statistics: %+v", s)
	}
	if runtime.GOOS == "linux" && (s.OpenFDs <= 0 || s.RSSBytes <= 0 || s.CPUMillis < 0) {
		return fmt.Errorf("Linux process counters unavailable: %+v", s)
	}
	if runtime.GOOS != "linux" && (s.OpenFDs == 0 || s.RSSBytes == 0) {
		return fmt.Errorf("zero process counters are not unavailable markers: %+v", s)
	}
	return nil
}

func validOISStats(s protocol.OISStats) error {
	if s.Channels.Preparing < 0 || s.Channels.Open < 0 || s.Channels.Closing < 0 || s.Channels.Closed < 0 ||
		s.Tickets < 0 || s.AccessesOnline < 0 || s.Epoch <= 0 || s.RelayBytes < 0 || s.MaxBufferedBytes < 0 {
		return fmt.Errorf("invalid OIS statistics: %+v", s)
	}
	return validProcessStats(s.Process)
}

func validOICStats(s protocol.OICStats) error {
	if s.AccessID == "" || s.Epoch < 0 || s.Generation < 0 || s.LicenseRemainingMS < 0 || s.Channels < 0 || s.RelayBytes < 0 || s.MaxBufferedBytes < 0 || s.Closed == nil {
		return fmt.Errorf("invalid OIC statistics: %+v", s)
	}
	for _, n := range s.Closed {
		if n < 0 {
			return fmt.Errorf("negative closed-channel counter")
		}
	}
	return validProcessStats(s.Process)
}

func (e *env) resourceOIC(ctx context.Context, access string) (resourceProcess[protocol.OICStats], error) {
	var r resourceProcess[protocol.OICStats]
	o := e.oics[access]
	if o == nil {
		return r, fmt.Errorf("OIC %s missing", access)
	}
	if err := processAlive(o.proc); err != nil {
		return r, err
	}
	r.PID = o.cmd.Process.Pid
	req, err := http.NewRequest("GET", "http://"+o.diag+"/stats", nil)
	if err != nil {
		return r, err
	}
	if err = statsRequest(ctx, e.stats, req, &r.Stats); err != nil {
		return r, err
	}
	r.SampledNS = resourceNS()
	if r.Stats.AccessID != access {
		return r, fmt.Errorf("OIC statistics identity mismatch")
	}
	return r, processAlive(o.proc)
}

func (e *env) resourceSnapshot(ctx context.Context) (resourceSnapshot, error) {
	r := resourceSnapshot{StartedNS: resourceNS(), OICs: map[string]resourceProcess[protocol.OICStats]{}}
	if err := processAlive(e.ois); err != nil {
		return r, err
	}
	r.OIS.PID = e.ois.cmd.Process.Pid
	req, err := http.NewRequest("GET", "https://"+e.api+"/v1/admin/stats", nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Authorization", "Bearer "+e.adminTok)
	if err = statsRequest(ctx, e.stats, req, &r.OIS.Stats); err != nil {
		return r, err
	}
	r.OIS.SampledNS = resourceNS()
	accesses := make([]string, 0, len(e.oics))
	for a := range e.oics {
		accesses = append(accesses, a)
	}
	sort.Strings(accesses)
	for _, a := range accesses {
		s, err := e.resourceOIC(ctx, a)
		if err != nil {
			return r, err
		}
		r.OICs[a] = s
	}
	r.EndedNS = resourceNS()
	return r, processAlive(e.ois)
}

func emptyOIS(s protocol.OISStats) bool {
	return s.Channels.Preparing == 0 && s.Channels.Open == 0 && s.Channels.Closing == 0 && s.Tickets == 0
}

func processAtBaseline(base, sample protocol.ProcessStats) bool {
	return sample.Goroutines >= base.Goroutines-10 && sample.Goroutines <= base.Goroutines+10 && sample.OpenFDs == base.OpenFDs
}

// A valid but not-yet-settled snapshot resets the consecutive count. A
// malformed snapshot is evidence failure, not a state to silently retry.
func resourceSampleOK(base resourceProfile, s resourceSnapshot, start, deadline int64) (bool, error) {
	if s.StartedNS < start || s.EndedNS < s.StartedNS || s.EndedNS > deadline ||
		base.RecordedNS >= start || len(base.OICs) != len(s.OICs) {
		return false, fmt.Errorf("resource snapshot time/profile mismatch")
	}
	if s.OIS.PID <= 0 || base.OIS.PID <= 0 || base.OIS.SampledNS >= start || s.OIS.SampledNS < s.StartedNS || s.OIS.SampledNS > s.EndedNS {
		return false, fmt.Errorf("OIS resource identity/time invalid")
	}
	if err := validOISStats(s.OIS.Stats); err != nil {
		return false, err
	}
	ok := emptyOIS(s.OIS.Stats) && s.OIS.Stats.AccessesOnline == base.OIS.Stats.AccessesOnline && processAtBaseline(base.OIS.Stats.Process, s.OIS.Stats.Process)
	for a, old := range base.OICs {
		x, exists := s.OICs[a]
		if !exists || x.PID <= 0 || old.PID <= 0 || old.SampledNS >= start || x.SampledNS < s.StartedNS || x.SampledNS > s.EndedNS || x.Stats.AccessID != a {
			return false, fmt.Errorf("OIC %s resource identity/time invalid", a)
		}
		if err := validOICStats(x.Stats); err != nil {
			return false, err
		}
		ok = ok && x.Stats.Channels == 0 && x.Stats.Connected && x.Stats.Registered && x.Stats.Epoch > 0 && x.Stats.Generation > 0 && processAtBaseline(old.Stats.Process, x.Stats.Process)
	}
	return ok, nil
}

func (e *env) publishBaseline() {
	e.resourceBase.ID, e.resourceBase.RecordedNS = resourceID("profile"), resourceNS()
	e.obs("resources_baseline", map[string]any{"schema": resourceSchema, "profile_id": e.resourceBase.ID,
		"recorded_ns": e.resourceBase.RecordedNS, "ois": e.resourceBase.OIS, "oics": e.resourceBase.OICs})
	e.logResourceFDs("baseline", "", e.resourceBase.ID)
}

// baseline may only initialize an environment before faults. It waits for
// three stable idle samples instead of taking one arbitrary timed sample.
func (e *env) baseline() {
	e.t.Helper()
	if e.resourceBase.ID != "" {
		e.t.Fatal("resource baseline cannot be recaptured")
	}
	e.closeIdle()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var prev resourceSnapshot
	consecutive := 0
	for {
		s, err := e.resourceSnapshot(ctx)
		if err != nil {
			e.t.Fatal(err)
		}
		idle := emptyOIS(s.OIS.Stats)
		for _, x := range s.OICs {
			idle = idle && x.Stats.Channels == 0 && x.Stats.Registered && x.Stats.Connected
		}
		stable := prev.OIS.PID != 0 && processAtBaseline(prev.OIS.Stats.Process, s.OIS.Stats.Process)
		for a, x := range s.OICs {
			stable = stable && processAtBaseline(prev.OICs[a].Stats.Process, x.Stats.Process)
		}
		if idle && stable {
			consecutive++
		} else {
			consecutive = 1
			if !idle {
				consecutive = 0
			}
		}
		if consecutive >= 3 {
			e.resourceBase = resourceProfile{Backend: e.be, OIS: s.OIS, OICs: s.OICs}
			e.publishBaseline()
			return
		}
		prev = s
		select {
		case <-ctx.Done():
			e.t.Fatal("resource warmup did not settle")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// extendBaselineOIC is only used for the fake-OIS protocol vector, before
// its first failing prepare; the real OIS baseline is kept byte-for-byte.
func (e *env) extendBaselineOIC(access string) {
	e.t.Helper()
	if e.resourceBase.ID == "" {
		e.t.Fatal("missing initial resource profile")
	}
	if _, exists := e.resourceBase.OICs[access]; exists {
		e.t.Fatal("OIC baseline already exists")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	x, err := e.resourceOIC(ctx, access)
	if err != nil {
		e.t.Fatal(err)
	}
	if x.Stats.Channels != 0 || !x.Stats.Connected {
		e.t.Fatal("new OIC is not idle and connected")
	}
	e.resourceBase.OICs[access] = x
	e.publishBaseline()
}

func (e *env) prepareOISBaseline(be string) oisResourceBaseline {
	e.t.Helper()
	ref := newEnv(e.t, be, envOpts{dirSuffix: resourceID("reference")})
	r := oisResourceBaseline{process: ref.resourceBase.OIS, backend: be}
	for _, o := range ref.oics {
		o.stop(e.t)
	}
	ref.stopOIS()
	ref.closeIdle()
	return r
}

func (e *env) useOISBaseline(ref oisResourceBaseline) {
	e.t.Helper()
	if ref.backend != e.be || ref.process.PID <= 0 {
		e.t.Fatal("wrong backend resource reference")
	}
	e.resourceBase.OIS, e.resourceBase.Backend = ref.process, ref.backend
	e.publishBaseline()
}

// settled starts the single B4 vector_cleanup budget before client
// transport cleanup, and keeps every raw sample for independent replay.
func (e *env) settled() {
	e.t.Helper()
	start := resourceNS()
	deadline := start + settle.Nanoseconds()
	ctx, cancel := context.WithTimeout(context.Background(), settle)
	defer cancel()
	id, profile := resourceID("check"), e.resourceBase.ID
	complete := false
	defer func() {
		if !complete {
			e.logResourceFDs("cleanup_failure", id, profile)
		}
	}()
	e.obs("resources_begin", map[string]any{"schema": resourceSchema, "check_id": id, "profile_id": profile,
		"started_ns": start, "deadline_ns": deadline, "origin": "vector_cleanup"})
	e.closeIdle()
	count, consecutive := 0, 0
	var first resourceSnapshot
	for {
		s, err := e.resourceSnapshot(ctx)
		if err != nil {
			e.t.Fatalf("resource sampling: %v", err)
		}
		count++
		e.obs("resources_sample", map[string]any{"schema": resourceSchema, "check_id": id, "profile_id": profile,
			"started_ns": s.StartedNS, "ended_ns": s.EndedNS, "ois": s.OIS, "oics": s.OICs})
		if count == 1 {
			first = s
		}
		if first.OIS.PID != s.OIS.PID {
			e.t.Fatal("OIS process changed during resource check")
		}
		for a, x := range s.OICs {
			if first.OICs[a].PID != x.PID {
				e.t.Fatal("OIC process changed during resource check")
			}
		}
		ok, err := resourceSampleOK(e.resourceBase, s, start, deadline)
		if err != nil {
			e.t.Fatal(err)
		}
		if ok {
			consecutive++
		} else {
			consecutive = 0
		}
		if consecutive >= 3 {
			done := resourceNS()
			if done > deadline {
				e.t.Fatal("resource completion exceeded absolute budget")
			}
			e.obs("resources", map[string]any{"schema": resourceSchema, "check_id": id, "profile_id": profile,
				"completed_ns": done, "settled_ms": (done - start) / int64(time.Millisecond), "samples": count})
			complete = true
			return
		}
		if resourceNS()+int64(50*time.Millisecond) >= deadline {
			e.t.Fatalf("resources not settled within T_close+1s: sample %+v baseline %+v", s, e.resourceBase)
		}
		select {
		case <-ctx.Done():
			e.t.Fatal("resource cleanup deadline exceeded")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
