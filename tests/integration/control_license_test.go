package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

// These observations surround the entire HTTP call. SampledNS retains the
// existing resource helper's meaning; it is not a server capture timestamp.
type controlLicenseObservation struct {
	RequestStartedNS   int64                              `json:"request_started_ns"`
	RequestCompletedNS int64                              `json:"request_completed_ns"`
	Process            resourceProcess[protocol.OICStats] `json:"process"`
}

func (e *env) controlLicense(ctx context.Context, access string) (controlLicenseObservation, error) {
	o := controlLicenseObservation{RequestStartedNS: resourceNS()}
	var err error
	o.Process, err = e.resourceOIC(ctx, access)
	o.RequestCompletedNS = resourceNS()
	if err != nil {
		return o, err
	}
	if err = validControlLicense(o); err != nil {
		return o, err
	}
	return o, nil
}

func validControlLicense(o controlLicenseObservation) error {
	s := o.Process.Stats
	if o.RequestStartedNS <= 0 || o.RequestStartedNS > o.Process.SampledNS ||
		o.Process.SampledNS > o.RequestCompletedNS || o.Process.PID <= 0 {
		return fmt.Errorf("invalid control-license request window/process")
	}
	if err := validOICStats(s); err != nil {
		return err
	}
	if s.AccessID == "" || !s.Connected || !s.Registered || s.Epoch <= 0 || s.Generation <= 0 || s.LicenseRemainingMS > 3000 {
		return fmt.Errorf("invalid registered control-license identity")
	}
	return nil
}

func controlLicenseIdentity(o controlLicenseObservation, pid int, access string, epoch, generation int64) error {
	if err := validControlLicense(o); err != nil {
		return err
	}
	s := o.Process.Stats
	if o.Process.PID != pid || s.AccessID != access || s.Epoch != epoch || s.Generation != generation {
		return fmt.Errorf("control-license PID/access/epoch/generation changed")
	}
	return nil
}

func controlLicenseRemainingNS(o controlLicenseObservation, at int64) (int64, error) {
	if err := validControlLicense(o); err != nil {
		return 0, err
	}
	if at < o.RequestCompletedNS {
		return 0, fmt.Errorf("fault precedes complete license observation")
	}
	// r is floor(positive actual lease remaining), unlike readiness authority.
	// Bounds above ensure multiplication and the nonnegative subtraction fit.
	return max(int64(0), o.Process.Stats.LicenseRemainingMS*int64(time.Millisecond)-(at-o.RequestStartedNS)), nil
}

func controlLicenseEligible(o controlLicenseObservation, at, prepareDeadline int64) (int64, error) {
	n, err := controlLicenseRemainingNS(o, at)
	if err != nil {
		return n, err
	}
	if at > prepareDeadline || o.Process.Stats.LicenseRemainingMS < 2250 || n < int64(2*time.Second) {
		return n, fmt.Errorf("control fault input not established: remaining=%dns deadline=%d at=%d", n, prepareDeadline, at)
	}
	return n, nil
}

type controlTuple struct {
	ClientLocal  string `json:"client_local"`
	ClientRemote string `json:"client_remote"`
	ServerLocal  string `json:"server_local"`
	ServerRemote string `json:"server_remote"`
}

type controlCutObservation struct {
	ControlIndex int `json:"control_index"`
	controlTuple
	StartedNS        int64   `json:"started_ns"`
	CompletedNS      int64   `json:"completed_ns"`
	ClientCloseError *string `json:"client_close_error"`
	ServerCloseError *string `json:"server_close_error"`
	RemainingNS      int64   `json:"remaining_ns"`
}

func controlCloseError(err error) *string {
	if err == nil {
		return nil
	}
	s := err.Error()
	return &s
}

func (p *proxy) controlConnection(i int) (pconn, controlTuple, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.conns) || p.conns[i] == nil || p.conns[i].a == nil || p.conns[i].b == nil {
		return pconn{}, controlTuple{}, fmt.Errorf("control index %d has no complete forwarded connection", i)
	}
	c := *p.conns[i] // Copy handles under the owner lock, never Close under it.
	tuple := controlTuple{c.a.LocalAddr().String(), c.a.RemoteAddr().String(), c.b.LocalAddr().String(), c.b.RemoteAddr().String()}
	return c, tuple, nil
}

type controlBinding struct {
	PID          int            `json:"pid"`
	ControlIndex int            `json:"control_index"`
	Tuple        controlTuple   `json:"control_tuple"`
	LogIndex     int            `json:"log_index"`
	Connected    map[string]any `json:"control_connected_event"`
}

func controlLogInteger(m map[string]any, key string) (int64, error) {
	x, ok := m[key].(float64)
	if !ok || x <= 0 || x >= float64(math.MaxInt64) || math.Trunc(x) != x {
		return 0, fmt.Errorf("invalid control log %s", key)
	}
	return int64(x), nil
}

// logIndex is zero based. The returned maps are immutable process-log rows.
var errControlConnectedMissing = errors.New("no control connected log after fence")

func controlConnectedLog(p *proc, after int) (map[string]any, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if after < 0 || after > len(p.lines) {
		return nil, 0, fmt.Errorf("invalid control log fence")
	}
	var found map[string]any
	index := -1
	for i := after; i < len(p.lines); i++ {
		if p.lines[i]["msg"] == "control connected" {
			if found != nil {
				return nil, 0, fmt.Errorf("more than one control connection after fence")
			}
			found, index = p.lines[i], i
		}
	}
	if found == nil {
		return nil, 0, errControlConnectedMissing
	}
	return found, index, nil
}

func latestControlIdentity(p *proc) (int64, int64, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.lines) - 1; i >= 0; i-- {
		m := p.lines[i]
		if m["msg"] == "control connected" {
			epoch, err := controlLogInteger(m, "epoch")
			if err != nil {
				return 0, 0, 0, err
			}
			gen, err := controlLogInteger(m, "generation")
			return epoch, gen, i, err
		}
	}
	return 0, 0, 0, fmt.Errorf("no current control identity")
}

func (e *env) initialControlBinding(access string) (controlBinding, error) {
	o := e.oics[access]
	if o == nil {
		return controlBinding{}, fmt.Errorf("OIC missing")
	}
	if err := processAlive(o.proc); err != nil {
		return controlBinding{}, err
	}
	row, index, err := controlConnectedLog(o.proc, 0)
	if err != nil {
		return controlBinding{}, err
	}
	_, tuple, err := e.acc.controlConnection(o.ctrl)
	return controlBinding{o.cmd.Process.Pid, o.ctrl, tuple, index, row}, err
}

type controlTargetRecord struct {
	Index         int    `json:"index"`
	AcceptedNS    int64  `json:"accepted_ns"`
	LocalAddress  string `json:"local_address"`
	RemoteAddress string `json:"remote_address"`
	DataBase64    string `json:"data_base64"`
	EOF           bool   `json:"eof"`
	Reset         bool   `json:"reset"`
	Done          bool   `json:"done"`
}

type controlTargetObservation struct {
	StartedNS   int64                 `json:"started_ns"`
	CompletedNS int64                 `json:"completed_ns"`
	Connections []controlTargetRecord `json:"connections"`
}

func controlTarget(tg *fixture.Target) (controlTargetObservation, error) {
	o := controlTargetObservation{StartedNS: resourceNS(), Connections: []controlTargetRecord{}}
	records := tg.Conns()
	o.CompletedNS = resourceNS()
	if len(records) > 16 {
		return o, fmt.Errorf("control target instrument exceeds 16 connections")
	}
	for i, r := range records {
		if r.Accepted.IsZero() || r.LocalAddress == "" || r.RemoteAddress == "" || len(r.Data) > 1024 {
			return o, fmt.Errorf("invalid/oversized target record %d", i)
		}
		o.Connections = append(o.Connections, controlTargetRecord{i, r.Accepted.Sub(resourceOrigin).Nanoseconds(), r.LocalAddress, r.RemoteAddress,
			base64.StdEncoding.EncodeToString(r.Data), r.EOF, r.Reset, r.Done})
	}
	return o, nil
}

func sameControlTarget(a, b controlTargetRecord) bool {
	return a.Index == b.Index && a.AcceptedNS == b.AcceptedNS && a.LocalAddress == b.LocalAddress && a.RemoteAddress == b.RemoteAddress
}

func controlTargetPrefix(a, b controlTargetObservation, extra int) error {
	if len(b.Connections) != len(a.Connections)+extra {
		return fmt.Errorf("target identity count changed")
	}
	for i, old := range a.Connections {
		if !sameControlTarget(old, b.Connections[i]) {
			return fmt.Errorf("target identity %d replaced", i)
		}
	}
	return nil
}

func controlRunningTarget(before, after controlTargetObservation) (bool, error) {
	if len(after.Connections) == len(before.Connections) {
		return false, controlTargetPrefix(before, after, 0)
	}
	if err := controlTargetPrefix(before, after, 1); err != nil {
		return false, err
	}
	data, err := base64.StdEncoding.Strict().DecodeString(after.Connections[len(before.Connections)].DataBase64)
	return err == nil && bytes.Contains(data, []byte("k")), err
}

type controlChannelLog struct {
	StartedNS   int64            `json:"started_ns"`
	CompletedNS int64            `json:"completed_ns"`
	AfterIndex  int              `json:"after_index"`
	ReadyIndex  int              `json:"ready_index"`
	Ready       map[string]any   `json:"ready_event"`
	Opens       []map[string]any `json:"new_open_events"`
	Readies     []map[string]any `json:"new_ready_events"`
}

var errControlReadyMissing = errors.New("channel ready log not observed")

func controlLogFence(p *proc, channel string) (controlChannelLog, error) {
	o := controlChannelLog{StartedNS: resourceNS(), Opens: []map[string]any{}, Readies: []map[string]any{}}
	p.mu.Lock()
	defer p.mu.Unlock()
	o.AfterIndex = len(p.lines)
	o.ReadyIndex = -1
	for i, row := range p.lines {
		if row["msg"] == "ready" && row["channel"] == channel {
			if o.Ready != nil {
				return o, fmt.Errorf("duplicate channel ready log")
			}
			o.Ready, o.ReadyIndex = row, i
		}
	}
	o.CompletedNS = resourceNS()
	if o.Ready == nil {
		return o, errControlReadyMissing
	}
	return o, nil
}

func controlLogsAfter(p *proc, fence controlChannelLog) (controlChannelLog, error) {
	o := fence
	o.StartedNS = resourceNS()
	o.Opens, o.Readies = []map[string]any{}, []map[string]any{}
	p.mu.Lock()
	defer p.mu.Unlock()
	if fence.AfterIndex < 0 || fence.AfterIndex > len(p.lines) {
		return o, fmt.Errorf("invalid channel log fence")
	}
	for _, row := range p.lines[fence.AfterIndex:] {
		if row["msg"] == "open" {
			o.Opens = append(o.Opens, row)
		}
		if row["msg"] == "ready" {
			o.Readies = append(o.Readies, row)
		}
		if len(o.Opens) > 16 || len(o.Readies) > 16 {
			return o, fmt.Errorf("control channel log instrument overflow")
		}
	}
	o.CompletedNS = resourceNS()
	if len(o.Opens) != 0 || len(o.Readies) != 0 {
		return o, fmt.Errorf("ZERO-TOLERANCE: new open/ready under control fault")
	}
	return o, nil
}

type controlTerminal struct {
	AtNS      int64         `json:"at_ns"`
	Reason    protocol.Code `json:"reason"`
	ReadBytes int64         `json:"read_bytes"`
}

type controlWorkers struct {
	stop      chan struct{}
	writeDone chan struct{}
	readDone  chan struct{}
	terminal  controlTerminal // Published by close(readDone).
	readErr   error
	once      sync.Once
	closeOnce sync.Once
	closeDone chan struct{}
}

func startControlWorkers(s io.ReadWriteCloser, now func() int64) *controlWorkers {
	w := &controlWorkers{stop: make(chan struct{}), writeDone: make(chan struct{}), readDone: make(chan struct{}), closeDone: make(chan struct{})}
	go func() {
		defer close(w.writeDone)
		timer := time.NewTicker(200 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-timer.C:
				if _, err := s.Write([]byte("k")); err != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer close(w.readDone)
		data, err := io.ReadAll(s)
		at := now()
		w.terminal = controlTerminal{at, abortCode(err), int64(len(data))}
		w.readErr = err
	}()
	return w
}

func (w *controlWorkers) close(ctx context.Context, s io.Closer) error {
	w.once.Do(func() { close(w.stop) })
	w.closeOnce.Do(func() { go func() { defer close(w.closeDone); _ = s.Close() }() })
	for _, ch := range []<-chan struct{}{w.closeDone, w.writeDone, w.readDone} {
		select {
		case <-ch:
		case <-ctx.Done():
			return fmt.Errorf("control fixture worker cleanup: %w", ctx.Err())
		}
	}
	return nil
}

func controlTerminalWithin(t controlTerminal, cut controlCutObservation) error {
	if cut.StartedNS <= 0 || cut.CompletedNS < cut.StartedNS || t.AtNS < cut.CompletedNS ||
		t.AtNS-cut.CompletedNS < int64(2*time.Second) || t.AtNS-cut.StartedNS > int64(4*time.Second) ||
		(t.Reason != protocol.CodeLicenseExpired && t.Reason != protocol.CodeAccessOffline) {
		return fmt.Errorf("control terminal outside original 2..4s/identity: %+v cut=%+v", t, cut)
	}
	return nil
}

type controlOffline struct {
	DeadlineNS        int64               `json:"deadline_ns"`
	AccessStartedNS   int64               `json:"access_request_started_ns"`
	AccessCompletedNS int64               `json:"access_request_completed_ns"`
	AccessStatus      int                 `json:"access_status"`
	Access            protocol.AccessView `json:"access_view"`
	OpenStartedNS     int64               `json:"open_request_started_ns"`
	OpenCompletedNS   int64               `json:"open_request_completed_ns"`
	OpenStatus        int                 `json:"open_status"`
	OpenCode          protocol.Code       `json:"open_code"`
	ChannelID         string              `json:"channel_id"`
	AttachPresent     bool                `json:"attach_present"`
}

func (e *env) controlOffline(ctx context.Context, generation, deadline int64, c *client.Client, revision string) (controlOffline, error) {
	o := controlOffline{DeadlineNS: deadline}
	err := g3Poll(ctx, func() (bool, error) {
		o.Access = protocol.AccessView{}
		o.AccessStartedNS = resourceNS()
		status, err := e.admin.Do(ctx, http.MethodGet, "/v1/admin/accesses/lab-a", "", nil, &o.Access)
		o.AccessCompletedNS, o.AccessStatus = resourceNS(), status
		if err != nil {
			return false, err
		}
		if status != http.StatusOK || o.Access.AccessID != "lab-a" || o.Access.Generation != generation || o.AccessCompletedNS > deadline {
			return false, fmt.Errorf("offline access identity/deadline changed")
		}
		return !o.Access.Online, nil
	})
	if err != nil {
		return o, err
	}
	o.OpenStartedNS = resourceNS()
	var response protocol.OpenResponse
	status, openErr := c.Do(ctx, http.MethodPost, "/v1/channels", "", protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: revision}, &response)
	o.OpenCompletedNS = resourceNS()
	o.OpenCode, o.ChannelID, o.AttachPresent = protocol.CodeOf(openErr), response.ChannelID, response.Attach != nil
	o.OpenStatus = status
	if o.OpenStatus != http.StatusServiceUnavailable || o.OpenCode != protocol.CodeAccessOffline || o.ChannelID != "" || o.AttachPresent || o.OpenCompletedNS > deadline {
		return o, fmt.Errorf("offline new Open: status=%d code=%s channel=%s within=%t", o.OpenStatus, o.OpenCode, o.ChannelID, o.OpenCompletedNS <= deadline)
	}
	return o, nil
}

type controlRestored struct {
	ConnectionMark int                       `json:"connection_mark"`
	ControlIndex   int                       `json:"control_index"`
	Tuple          controlTuple              `json:"control_tuple"`
	Connected      map[string]any            `json:"control_connected_event"`
	License        controlLicenseObservation `json:"license_observation"`
	ChannelLog     controlChannelLog         `json:"channel_log"`
}

func (e *env) restoreControlWitness(ctx context.Context, original, peer controlBinding, epoch, generation int64) (controlRestored, error) {
	r := controlRestored{ConnectionMark: e.acc.count()}
	e.acc.setMode(pass)
	err := g3Poll(ctx, func() (bool, error) {
		// Registration may legitimately still be false while reconnecting.
		started := resourceNS()
		v, err := e.resourceOIC(ctx, "lab-a")
		r.License = controlLicenseObservation{started, resourceNS(), v}
		if err != nil {
			return false, err
		}
		if v.PID != original.PID || v.Stats.AccessID != "lab-a" || v.Stats.Epoch != epoch {
			return false, fmt.Errorf("restored OIC process/epoch changed")
		}
		if !v.Stats.Connected || !v.Stats.Registered {
			return false, nil
		}
		if v.Stats.Generation <= generation {
			return false, fmt.Errorf("control registered without a new generation")
		}
		return true, nil
	})
	if err != nil {
		return r, err
	}
	if err = validControlLicense(r.License); err != nil {
		return r, err
	}
	if e.acc.count() != r.ConnectionMark+1 {
		return r, fmt.Errorf("restoration did not create exactly one forwarded connection")
	}
	r.ControlIndex = r.ConnectionMark
	_, r.Tuple, err = e.acc.controlConnection(r.ControlIndex)
	if err != nil {
		return r, err
	}
	err = g3Poll(ctx, func() (bool, error) {
		var logErr error
		r.Connected, _, logErr = controlConnectedLog(e.oics["lab-a"].proc, original.LogIndex+1)
		if errors.Is(logErr, errControlConnectedMissing) {
			return false, nil
		}
		return logErr == nil, logErr
	})
	if err != nil {
		return r, err
	}
	if e.acc.count() != r.ConnectionMark+1 {
		return r, fmt.Errorf("extra connection appeared while binding restored control")
	}
	newEpoch, err := controlLogInteger(r.Connected, "epoch")
	if err != nil {
		return r, err
	}
	newGen, err := controlLogInteger(r.Connected, "generation")
	if err != nil {
		return r, err
	}
	if err = controlLicenseIdentity(r.License, original.PID, "lab-a", newEpoch, newGen); err != nil {
		return r, err
	}
	if newEpoch != epoch || newGen <= generation {
		return r, fmt.Errorf("restored control log is not a new generation")
	}
	b, err := e.initialControlBinding("lab-b")
	if err != nil {
		return r, err
	}
	if b.PID != peer.PID || b.ControlIndex != peer.ControlIndex || b.Tuple != peer.Tuple || b.LogIndex != peer.LogIndex {
		return r, fmt.Errorf("unrelated control identity changed during restoration")
	}
	bEpoch, err := controlLogInteger(peer.Connected, "epoch")
	if err != nil {
		return r, err
	}
	bGen, err := controlLogInteger(peer.Connected, "generation")
	if err != nil {
		return r, err
	}
	bSample, err := e.controlLicense(ctx, "lab-b")
	if err != nil {
		return r, err
	}
	if err = controlLicenseIdentity(bSample, peer.PID, "lab-b", bEpoch, bGen); err != nil {
		return r, err
	}
	e.oics["lab-a"].ctrl = r.ControlIndex
	return r, nil
}

func (e *env) controlCutWitness() (resultErr error) {
	fields := map[string]any{"vector": "TV-B2-10", "event": "control_cut", "access_id": "lab-a"}
	defer func() {
		if resultErr != nil {
			fields["input_error"] = resultErr.Error()
		} else if e.t.Failed() {
			fields["input_error"] = "vector failed before witness completion"
		}
		fields["recorded_ns"] = resourceNS()
		e.obs("O4", fields)
	}()
	original, err := e.initialControlBinding("lab-a")
	if err != nil {
		return err
	}
	fields["control_binding"] = original
	peer, err := e.initialControlBinding("lab-b")
	if err != nil {
		return err
	}
	epoch, err := controlLogInteger(original.Connected, "epoch")
	if err != nil {
		return err
	}
	generation, err := controlLogInteger(original.Connected, "generation")
	if err != nil {
		return err
	}
	fields["epoch"], fields["generation"] = epoch, generation
	before, err := controlTarget(e.t1)
	targets := map[string]controlTargetObservation{"before_dial": before}
	fields["targets"] = targets
	if err != nil {
		return err
	}
	c := e.consumer("alice")
	s, err := c.Dial(bg, "lab-a/http") // The only business Dial; never retried by the instrument.
	if err != nil {
		return err
	}
	prepareStart := resourceNS()
	prepareDeadline := prepareStart + int64(5*time.Second)
	prep, cancelPrep := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(prepareDeadline)))
	defer cancelPrep()
	fields["channel_id"], fields["preparation_started_ns"], fields["preparation_deadline_ns"] = s.ChannelID(), prepareStart, prepareDeadline
	workers := startControlWorkers(s, resourceNS)
	workersClosed := false
	defer func() {
		if workersClosed {
			return
		}
		// Only a previously failed input/behavior reaches this fallback. Never
		// turn forced I/O teardown into a successful resource observation.
		fields["cleanup_error"] = "failed vector required fixture I/O teardown before worker join"
		if resultErr == nil {
			resultErr = fmt.Errorf("control vector exited before worker join")
		}
		forceControlFixtureIO(e.acc)
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := workers.close(cleanup, s); err != nil {
			resultErr = errors.Join(resultErr, err)
			fields["cleanup_error"] = err.Error()
		}
	}()
	var running controlTargetObservation
	err = g3Poll(prep, func() (bool, error) {
		var sampleErr error
		running, sampleErr = controlTarget(e.t1)
		targets["running"] = running
		if sampleErr != nil {
			return false, sampleErr
		}
		return controlRunningTarget(before, running)
	})
	if err != nil {
		return err
	}
	view, err := c.Channel(prep, s.ChannelID())
	if err != nil {
		return err
	}
	if view.ChannelID != s.ChannelID() || view.ExposureID != "lab-a/http" || view.Revision == "" || view.State != "open" {
		return fmt.Errorf("running channel identity missing")
	}
	var fence controlChannelLog
	err = g3Poll(prep, func() (bool, error) {
		var logErr error
		fence, logErr = controlLogFence(e.ois, s.ChannelID())
		if errors.Is(logErr, errControlReadyMissing) {
			return false, nil
		}
		return logErr == nil, logErr
	})
	if err != nil {
		return err
	}
	fields["channel_log"] = fence
	e.acc.setMode(refuse)
	var license controlLicenseObservation
	attempts := 0
	err = g3Poll(prep, func() (bool, error) {
		attempts++
		var sampleErr error
		license, sampleErr = e.controlLicense(prep, "lab-a")
		fields["selection_attempts"], fields["license_observation"] = attempts, license
		if sampleErr != nil {
			return false, sampleErr
		}
		if sampleErr = controlLicenseIdentity(license, original.PID, "lab-a", epoch, generation); sampleErr != nil {
			return false, sampleErr
		}
		return license.Process.Stats.LicenseRemainingMS >= 2250, nil
	})
	if err != nil {
		return err
	}
	current, err := e.initialControlBinding("lab-a")
	if err != nil {
		return err
	}
	if current.PID != original.PID || current.ControlIndex != original.ControlIndex || current.LogIndex != original.LogIndex || current.Tuple != original.Tuple {
		return fmt.Errorf("control identity changed before cut")
	}
	if resourceNS() > prepareDeadline {
		return fmt.Errorf("control phase preparation expired before cut")
	}
	select {
	case <-workers.readDone:
		fields["terminal"] = workers.terminal
		return fmt.Errorf("business stream terminated before control fault: %v", workers.readErr)
	default:
	}
	cut, err := e.acc.cutObserved(original.ControlIndex, resourceNS)
	fields["cut"] = cut
	if err != nil {
		return err
	}
	cut.RemainingNS, err = controlLicenseEligible(license, cut.CompletedNS, prepareDeadline)
	fields["cut"] = cut
	if err != nil {
		return err
	}
	offlineDeadline := cut.StartedNS + int64(5*time.Second)
	faultCtx, cancelFault := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(offlineDeadline)))
	defer cancelFault()
	offline, err := e.controlOffline(faultCtx, generation, offlineDeadline, c, view.Revision)
	fields["offline"] = offline
	if err != nil {
		return err
	}
	select {
	case <-workers.readDone:
	case <-faultCtx.Done():
		return fmt.Errorf("control terminal: %w", faultCtx.Err())
	}
	terminal := workers.terminal
	fields["terminal"], fields["reason"], fields["ms"] = terminal, terminal.Reason, (terminal.AtNS-cut.CompletedNS)/int64(time.Millisecond)
	if err = controlTerminalWithin(terminal, cut); err != nil {
		return err
	}
	if err = workers.close(faultCtx, s); err != nil {
		fields["cleanup_error"] = err.Error()
		return err
	}
	workersClosed = true
	faultTarget, err := controlTarget(e.t1)
	targets["fault_complete"] = faultTarget
	if err != nil {
		return err
	}
	if err = controlTargetPrefix(running, faultTarget, 0); err != nil {
		return err
	}
	faultLog, err := controlLogsAfter(e.ois, fence)
	fields["channel_log"] = faultLog
	if err != nil {
		return err
	}
	restoreCtx, cancelRestore := context.WithTimeout(context.Background(), 15*time.Second) // Existing waitRegistered watchdog.
	defer cancelRestore()
	restored, err := e.restoreControlWitness(restoreCtx, original, peer, epoch, generation)
	fields["restored"] = restored
	if err != nil {
		return err
	}
	e.settled() // Original baseline and absolute 2s resource deadline unchanged.
	finalTarget, err := controlTarget(e.t1)
	targets["restored"] = finalTarget
	if err != nil {
		return err
	}
	if err = controlTargetPrefix(running, finalTarget, 0); err != nil {
		return err
	}
	restored.ChannelLog, err = controlLogsAfter(e.ois, fence)
	fields["restored"] = restored
	return err
}

// Failure-only closure of handles owned by this environment. Unlike iterating
// mutable *pconn pointers after unlocking, the snapshot cannot race an upstream
// dial publishing b. A dial published after the snapshot encounters closed a.
func forceControlFixtureIO(p *proxy) {
	p.mu.Lock()
	p.mode = refuse
	conns := make([]pconn, 0, len(p.conns))
	for _, c := range p.conns {
		if c != nil {
			conns = append(conns, *c)
		}
	}
	pending := append([]net.Conn(nil), p.pending...)
	p.mu.Unlock()
	for _, c := range conns {
		if c.a != nil {
			_ = c.a.Close()
		}
		if c.b != nil {
			_ = c.b.Close()
		}
	}
	for _, c := range pending {
		_ = c.Close()
	}
}
