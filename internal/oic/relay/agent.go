// Package relay is the OIC runtime: one outbound mTLS control connection
// per Access (B1 §4), local re-check and dialing on prepare_channel
// (B2 §4), provider legs relayed through the tunnel bridge, the
// heartbeat-renewed local license (B2 §5), configuration reload and
// optional loopback diagnostics.
package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/oic/registration"
	"github.com/wyw-ai/loom-oi/internal/oic/target"
	"github.com/wyw-ai/loom-oi/internal/procstats"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
)

// Version is set at build time with -ldflags "-X ...relay.Version=...".
var Version = "dev"

const connectTimeout = 10 * time.Second

// Options configure an Agent.
type Options struct {
	ConfigPath string
	Logger     *slog.Logger
	// Resolver replaces DNS for targets (tests).
	Resolver target.Resolver
	// BackoffMin and BackoffMax bound reconnect delays (default 1 s, 30 s).
	BackoffMin, BackoffMax time.Duration
	// ReloadEvery is the configuration polling period (default 2 s).
	ReloadEvery time.Duration
}

type license struct {
	gen      int64
	deadline time.Time
}

type channel struct {
	id, exposureID   string
	gen              int64
	cancel           context.CancelFunc
	bridge           *tunnel.Bridge
	aborted          protocol.Code
	binding          observation.Binding
	mode             observation.Mode
	deadline         time.Time
	decisionID       string
	finalCheckID     string
	decisionDeadline time.Time
	sampleDeadline   time.Time
	running          bool
	decisionTimer    *time.Timer
}

// Agent is one running OIC.
type Agent struct {
	path   string
	log    *slog.Logger
	dialer target.Dialer
	client *http.Client
	base   string
	opts   Options

	license atomic.Pointer[license]
	wg      sync.WaitGroup
	// hard bounds channel I/O; it is cancelled only after shutdown's grace
	// so legs can still carry ABORT(Shutdown).
	hard       context.Context
	hardCancel context.CancelFunc
	// regMu orders register snapshots and sends; it is taken before mu.
	regMu sync.Mutex
	// reloadMu serializes candidate file reads without holding the state lock.
	reloadMu sync.Mutex

	mu               sync.Mutex
	cfg              *config.Config
	cfgSum           [32]byte
	cfgErr           string
	sess             *session
	gen, epoch       int64
	lastEpoch        int64
	registered       bool
	dataMax          int
	channels         map[string]*channel
	closed           map[string]int64
	relayBytes       int64
	maxBuffered      int64
	timer            *time.Timer
	diagAddr         string
	metrics          metrics.Registry
	exporter         *metrics.Handler
	observations     *observation.Table
	bindings         map[string]observation.Binding
	accepted         map[string]observation.Binding
	runtimeScheduler *observation.Scheduler
	probeTotals      observation.SchedulerStats
}

// New loads the configuration and prepares the mTLS client.
func New(opts Options) (*Agent, error) {
	cfg, data, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.OIS.CAFile)
	if err != nil {
		return nil, fmt.Errorf("OIS CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("OIS CA: %s holds no PEM certificate", cfg.OIS.CAFile)
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.BackoffMin <= 0 {
		opts.BackoffMin = time.Second
	}
	if opts.BackoffMax < opts.BackoffMin {
		opts.BackoffMax = max(30*time.Second, opts.BackoffMin)
	}
	if opts.ReloadEvery <= 0 {
		opts.ReloadEvery = 2 * time.Second
	}
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: pool,
		ServerName: cfg.OIS.ServerName, NextProtos: []string{"http/1.1"}}
	a := &Agent{
		path: opts.ConfigPath, log: opts.Logger, opts: opts,
		dialer: target.Dialer{Resolver: opts.Resolver},
		// Only WebSocket upgrades use this client. Without keep-alives a
		// refused attach (e.g. 410 after a revocation) closes its connection
		// instead of idling in the pool, where it would hold a descriptor on
		// OIS and be reused by a later attach (B4 §4, TV-B1-11).
		client: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConf, DisableKeepAlives: true}},
		base:   "wss://" + hostOf(cfg.OIS.URL),
		cfg:    cfg, cfgSum: cfg.ContentDigest(data),
		dataMax: cfg.Limits.DataMax, channels: map[string]*channel{}, closed: map[string]int64{},
	}
	a.hard, a.hardCancel = context.WithCancel(context.Background())
	a.observations, err = observation.NewTable(cfg.Limits.MaxExposures)
	if err != nil {
		return nil, err
	}
	a.bindings = make(map[string]observation.Binding)
	a.accepted = make(map[string]observation.Binding)
	return a, nil
}

func hostOf(u string) string {
	// config validated "wss://host[:port][/]".
	h := u[len("wss://"):]
	if h[len(h)-1] == '/' {
		h = h[:len(h)-1]
	}
	return h
}

// Run keeps the control connection up until ctx ends, then aborts all
// channels with Shutdown.
func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		a.watchConfig(ctx)
	}()
	if err := a.startDiag(ctx, &bg); err != nil {
		cancel()
		bg.Wait()
		return err
	}
	backoff := a.opts.BackoffMin
	for {
		welcomed, err := a.session(ctx)
		if ctx.Err() != nil {
			break
		}
		a.log.Warn("control connection ended", "error", errString(err))
		if welcomed {
			backoff = a.opts.BackoffMin
		}
		delay := backoff/2 + rand.N(backoff/2+1)
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
		backoff = min(2*backoff, a.opts.BackoffMax)
		if ctx.Err() != nil {
			break
		}
	}
	a.shutdown()
	cancel()
	bg.Wait()
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// shutdown aborts every channel and waits up to T_close for them.
func (a *Agent) shutdown() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.channels))
	for id := range a.channels {
		ids = append(ids, id)
	}
	wait := limits.D(a.cfg.Limits.CloseMS) + time.Second
	if a.timer != nil {
		a.timer.Stop()
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.abort(id, protocol.CodeShutdown)
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		a.log.Warn("channels still closing at shutdown")
	}
	a.hardCancel()
}

// ---- control session ----

type session struct {
	c               *websocket.Conn
	ctx             context.Context
	cancel          context.CancelFunc
	writer          *controlWriter
	limits          limits.Observation
	ids             *protocol.ControlIDs
	scheduler       *observation.Scheduler
	registers       []map[string]observation.Binding // Agent.mu
	expiry          *time.Timer                      // Agent.mu
	readSeq         uint64
	probeIDs        map[string]bool                // Agent.mu
	pendingDirty    map[string]observation.Binding // Agent.mu
	notify          chan struct{}
	publicationDone chan struct{}
	checks          int // Agent.mu; actual local checks not yet returned to the writer
}

func (s *session) send(ctx context.Context, m protocol.Message) error {
	return s.writer.send(ctx, m)
}

func (s *session) read(ctx context.Context) (protocol.Message, error) {
	typ, data, err := s.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, protocol.Errorf(protocol.CodeProtocolError, "control messages must be text")
	}
	m, err := protocol.DecodeControlForVersion(data, protocol.ControlSubprotocolV2)
	if err != nil {
		return nil, err
	}
	if _, ok := m.(*protocol.ObservationQuery); ok && len(data) > s.limits.MessageMax {
		return nil, protocol.Errorf(protocol.CodePayloadTooLarge, "query exceeds negotiated telemetry limit")
	}
	h := protocol.ControlHeader(m)
	if h.Seq <= s.readSeq {
		return nil, protocol.Errorf(protocol.CodeProtocolError, "control sequence did not increase")
	}
	s.readSeq = h.Seq
	return m, nil
}

// fail reports a protocol error to OIS before the session ends.
func (s *session) fail(ctx context.Context, err error) error {
	_ = s.send(ctx, &protocol.ControlError{Code: protocol.CodeOf(err), Detail: err.Error()})
	return err
}

func (a *Agent) session(ctx context.Context) (bool, error) {
	dctx, cancel := context.WithTimeout(ctx, connectTimeout)
	c, response, err := websocket.Dial(dctx, a.base+"/v1/access/control", &websocket.DialOptions{
		HTTPClient: a.client, Subprotocols: []string{protocol.ControlSubprotocolV2}})
	cancel()
	if err != nil {
		return false, controlHandshakeError(response, err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != protocol.ControlSubprotocolV2 {
		return false, protocol.Errorf(protocol.CodeVersionUnsupported, "OIS did not negotiate control v2")
	}
	a.mu.Lock()
	c.SetReadLimit(a.cfg.Limits.CtrlMax)
	last := a.lastEpoch
	localObs, queue, maxExposures := a.cfg.ObservationLimits, a.cfg.Limits.WriteQueue, a.cfg.Limits.MaxExposures
	a.mu.Unlock()
	sctx, stop := context.WithCancel(ctx)
	s := &session{c: c, ctx: sctx, cancel: stop, limits: localObs, probeIDs: make(map[string]bool), pendingDirty: make(map[string]observation.Binding), notify: make(chan struct{}, 1), publicationDone: make(chan struct{})}
	s.ids, err = protocol.NewControlIDs()
	if err != nil {
		stop()
		return false, err
	}
	s.writer = newControlWriter(sctx, c, queue, maxExposures+localObs.QuerySlots, localObs.QuerySlots, localObs.MessageMax, limits.D(localObs.WriteMS))
	go a.publicationLoop(s)
	defer func() {
		stop()
		s.writer.stop()
		<-s.publicationDone
		if s.scheduler != nil {
			_ = s.scheduler.Close(context.Background())
			final := s.scheduler.Stats()
			a.mu.Lock()
			a.probeTotals.Started += final.Started
			a.probeTotals.Finished += final.Finished
			a.probeTotals.MaxActive = max(a.probeTotals.MaxActive, final.MaxActive)
			a.probeTotals.MaxQueued = max(a.probeTotals.MaxQueued, final.MaxQueued)
			a.probeTotals.MaxWait = max(a.probeTotals.MaxWait, final.MaxWait)
			if a.runtimeScheduler == s.scheduler {
				a.runtimeScheduler = nil
			}
			a.mu.Unlock()
		}
	}()
	if err := s.send(ctx, &protocol.Hello{OICVersion: "oic/" + Version, LastEpochSeen: last}); err != nil {
		return false, err
	}
	rctx, cancel := context.WithTimeout(ctx, connectTimeout)
	m, err := s.read(rctx)
	cancel()
	if err != nil {
		return false, err
	}
	w, ok := m.(*protocol.Welcome)
	if !ok {
		return false, s.fail(ctx, protocol.Errorf(protocol.CodeProtocolError, "expected welcome, got %T", m))
	}
	defer a.lost(s)
	if err := a.welcome(s, w); err != nil {
		return false, s.fail(ctx, err)
	}
	if _, err := a.sendRegister(ctx, s); err != nil {
		return true, err
	}
	for {
		m, err := s.read(ctx)
		if err == nil {
			err = a.handle(ctx, s, m)
		} else if protocol.CodeOf(err) == protocol.CodeProtocolError {
			err = s.fail(ctx, err)
		}
		if err != nil {
			return true, err
		}
	}
}

// welcome adopts the connection's epoch and generation. Channels of older
// generations end at once (B1 D1-f, B2 §5).
func (a *Agent) welcome(s *session, w *protocol.Welcome) error {
	a.mu.Lock()
	if w.ObservationLimits == nil {
		a.mu.Unlock()
		return protocol.Errorf(protocol.CodeProtocolError, "welcome lacks observation limits")
	}
	r := w.ObservationLimits
	s.limits.MessageMax = min(s.limits.MessageMax, r.MessageMax)
	s.limits.PendingChecks = min(s.limits.PendingChecks, r.PendingChecks)
	s.limits.QuerySlots = min(s.limits.QuerySlots, r.QuerySlots)
	s.limits.CheckMS = min(s.limits.CheckMS, r.CheckMS)
	s.limits.QueryMS = min(s.limits.QueryMS, r.QueryMS)
	s.limits.WriteMS = min(s.limits.WriteMS, r.WriteMS)
	s.limits.RefreshMS = min(s.limits.RefreshMS, r.RefreshMS)
	a.sess, a.gen, a.epoch, a.registered = s, w.Generation, w.Epoch, false
	if err := a.resetObservations(s, w); err != nil {
		a.mu.Unlock()
		return err
	}
	var err error
	s.scheduler, err = a.newScheduler(s)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	a.runtimeScheduler = s.scheduler
	a.lastEpoch = max(a.lastEpoch, w.Epoch)
	a.dataMax = w.Limits.DataMax
	var stale []string
	for id, ch := range a.channels {
		if ch.gen != w.Generation {
			stale = append(stale, id)
		}
	}
	a.setLicense(w.Generation, w.LicenseTTLMS)
	a.mu.Unlock()
	s.writer.narrow(s.limits.MessageMax, s.limits.QuerySlots, limits.D(s.limits.WriteMS))
	for _, id := range stale {
		a.abort(id, protocol.CodeChannelInterrupted)
	}
	a.log.Info("control connected", "epoch", w.Epoch, "generation", w.Generation, "run_id", w.RunID)
	return nil
}

// sendRegister registers the current exposures on s. regMu orders taking
// the snapshot and sending it, so a register built from an older
// configuration never follows a newer one (a reload racing a reconnect).
func (a *Agent) sendRegister(ctx context.Context, s *session) (int, error) {
	a.regMu.Lock()
	defer a.regMu.Unlock()
	a.mu.Lock()
	snapshot := registration.Snapshot(a.cfg)
	if a.sess != s {
		a.mu.Unlock()
		return 0, context.Canceled
	}
	bindings := make(map[string]observation.Binding, len(snapshot))
	for i := range snapshot {
		b := a.bindings[snapshot[i].ExposureID]
		wb := wireBinding(b)
		snapshot[i].Binding = &wb
		snapshot[i].ProbeSupport = protocol.ProbeUnsupported
		if b.SourceID != "" {
			snapshot[i].ProbeSupport = protocol.ProbeSupported
		}
		bindings[b.ExposureID] = b
	}
	if len(s.registers) >= a.cfg.Limits.WriteQueue {
		a.mu.Unlock()
		s.cancel()
		_ = s.c.CloseNow()
		return 0, protocol.Errorf(protocol.CodeResourceExhausted, "pending register queue full")
	}
	s.registers = append(s.registers, bindings)
	a.mu.Unlock()
	return len(snapshot), s.send(ctx, &protocol.Register{Exposures: snapshot})
}

func (a *Agent) lost(s *session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == s {
		a.sess, a.registered = nil, false
		a.observations.Disconnect()
		a.accepted = make(map[string]observation.Binding)
		if s.expiry != nil {
			s.expiry.Stop()
		}
	}
}

func (a *Agent) handle(ctx context.Context, s *session, m protocol.Message) error {
	started := time.Now()
	key := ""
	switch m.(type) {
	case *protocol.RegisterAck:
		key = "register"
	case *protocol.Heartbeat:
		key = "heartbeat"
	case *protocol.CloseChannel:
		key = "revoke"
	}
	if key != "" {
		defer func() { a.metrics.Observe(metrics.CriticalControlDuration, key, time.Since(started)) }()
	}
	switch v := m.(type) {
	case *protocol.RegisterAck:
		a.mu.Lock()
		a.registered = a.sess == s
		if len(s.registers) == 0 {
			a.mu.Unlock()
			return protocol.Errorf(protocol.CodeProtocolError, "unexpected register acknowledgement")
		}
		registered := s.registers[0]
		s.registers = s.registers[1:]
		var dirty []observation.Binding
		for _, id := range v.Accepted {
			if b, ok := registered[id]; ok && a.bindings[id] == b {
				a.accepted[id] = b
				dirty = append(dirty, b)
			}
		}
		a.mu.Unlock()
		if err := a.reconcileProbes(s); err != nil {
			return err
		}
		for _, b := range dirty {
			a.markDirty(s, b)
		}
		for _, r := range v.Rejected {
			a.log.Warn("exposure rejected by OIS", "exposure", r.ExposureID, "code", r.Code)
		}
	case *protocol.Heartbeat:
		a.mu.Lock()
		current, gen := a.sess == s, a.gen
		a.mu.Unlock()
		if !current {
			return s.send(ctx, &protocol.HeartbeatAck{AckSeq: v.Seq})
		}
		// A heartbeat read after the license lapsed, e.g. one buffered while
		// the process was paused past L, cannot revive the channels: the
		// lapse is the first check after resuming and ends them (B2 §5).
		if l := a.license.Load(); l != nil && l.gen == gen && !time.Now().Before(l.deadline) {
			a.expireChannels(gen)
		}
		a.mu.Lock()
		if a.sess == s {
			a.setLicense(a.gen, v.LicenseTTLMS)
		}
		a.mu.Unlock()
		return s.send(ctx, &protocol.HeartbeatAck{AckSeq: v.Seq})
	case *protocol.PrepareChannel:
		a.prepare(ctx, s, v)
	case *protocol.ObservationQuery:
		return a.query(s, v)
	case *protocol.AdmissionCheck:
		return a.admission(ctx, s, v)
	case *protocol.CloseChannel:
		a.abort(v.ChannelID, v.Code)
	case *protocol.ControlError:
		return fmt.Errorf("OIS reported %s: %s", v.Code, v.Detail)
	default:
		return s.fail(ctx, protocol.Errorf(protocol.CodeProtocolError, "unexpected %T from OIS", m))
	}
	return nil
}

// ---- license (B2 §5) ----

// setLicense renews the local license of gen from now. Caller holds a.mu.
func (a *Agent) setLicense(gen, ttlMS int64) {
	d := limits.D(ttlMS - a.cfg.Limits.LicenseMarginMS)
	a.license.Store(&license{gen: gen, deadline: time.Now().Add(d)})
	if a.timer == nil {
		a.timer = time.AfterFunc(d, a.expire)
	} else {
		a.timer.Reset(d)
	}
}

func (a *Agent) expire() {
	l := a.license.Load()
	if left := time.Until(l.deadline); left > 0 {
		a.mu.Lock()
		a.timer.Reset(left)
		a.mu.Unlock()
		return
	}
	a.expireChannels(l.gen)
}

// expireChannels aborts every channel with LicenseExpired; channels of
// older generations already ended when the newer one was welcomed.
func (a *Agent) expireChannels(gen int64) {
	a.mu.Lock()
	ids := make([]string, 0, len(a.channels))
	for id := range a.channels {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	if len(ids) > 0 {
		a.log.Warn("license expired: aborting channels", "generation", gen, "channels", len(ids))
	}
	for _, id := range ids {
		a.abort(id, protocol.CodeLicenseExpired)
	}
}

// allowFor is checked before every forward: nothing is relayed once the
// license of the channel's generation has expired.
func (a *Agent) allowFor(gen int64) func() protocol.Code {
	return func() protocol.Code {
		if l := a.license.Load(); l == nil || l.gen != gen || !time.Now().Before(l.deadline) {
			return protocol.CodeLicenseExpired
		}
		return ""
	}
}

// ---- channels ----

// prepare re-checks a prepare_channel locally (B2 §4) and starts the
// provider leg. Prepares outside this connection's epoch and generation,
// or before register_ack, are refused without dialing (TV-B1-17).
func (a *Agent) prepare(ctx context.Context, s *session, p *protocol.PrepareChannel) {
	a.prepareReceived(ctx, s, p, time.Now())
}

func (a *Agent) prepareReceived(ctx context.Context, s *session, p *protocol.PrepareChannel, started time.Time) {
	deadline := started.Add(limits.D(p.PrepareTTLMS))
	defer func() { a.metrics.Observe(metrics.CriticalControlDuration, "prepare", time.Since(started)) }()
	a.mu.Lock()
	var code protocol.Code
	exp := a.cfg.Find(p.ExposureID)
	var binding observation.Binding
	if p.Binding != nil {
		binding = localBinding(*p.Binding)
	}
	switch {
	case a.sess != s || !a.registered || p.Epoch != a.epoch || p.Generation != a.gen || a.channels[p.ChannelID] != nil:
		code = protocol.CodeProtocolError
	case exp == nil || !exp.IsEnabled() || exp.Revision != p.Revision:
		code = protocol.CodeLocalDenied
	case p.Binding == nil || a.bindings[p.ExposureID] != binding || a.accepted[p.ExposureID] != binding:
		code = protocol.CodeReadinessCheckFailed
	case a.allowFor(a.gen)() != "":
		code = protocol.CodeLicenseExpired
	case !time.Now().Before(deadline):
		code = protocol.CodePrepareTimeout
	case len(a.channels) >= a.cfg.Limits.MaxLocalChannels:
		code = protocol.CodeResourceExhausted
	}
	mode := observation.BestEffort
	var effect observation.Effect
	if code == "" {
		mode = narrowerMode(exp.AdmissionMode, p.EffectiveAdmission)
		d, e, err := a.observations.Evaluate(binding, mode, time.Now())
		effect = e
		a.effectMetrics(e)
		if err != nil {
			code = protocol.CodeReadinessCheckFailed
		} else {
			code = checkCode(d)
		}
	}
	if code != "" {
		a.mu.Unlock()
		a.log.Info("prepare refused", "channel", p.ChannelID, "exposure", p.ExposureID, "code", code)
		reason := ""
		if code == protocol.CodeReadinessCheckFailed {
			reason = "observation_changed"
		}
		_ = s.send(ctx, &protocol.PrepareFailed{ChannelID: p.ChannelID, Code: code, Reason: reason})
		if effect.Dirty {
			a.markDirty(s, binding)
		}
		return
	}
	pctx, cancel := context.WithDeadline(ctx, deadline)
	ch := &channel{id: p.ChannelID, exposureID: p.ExposureID, gen: p.Generation, cancel: cancel, binding: binding, mode: mode, deadline: deadline}
	a.channels[ch.id] = ch
	tgt, dataMax, dial := exp.Target, a.dataMax, limits.D(a.cfg.Limits.DialMS)
	grace := limits.D(a.cfg.Limits.CloseMS)
	a.mu.Unlock()
	if effect.Dirty {
		a.markDirty(s, binding)
	}
	a.wg.Add(1)
	go a.provide(ctx, pctx, s, ch, tgt, p.ProviderTicket, dataMax, dial, grace, deadline.Add(grace))
}

func (a *Agent) provide(ctx, pctx context.Context, s *session, ch *channel, tgt config.Target, ticket string,
	dataMax int, dial, grace time.Duration, readyBy time.Time) {
	defer a.wg.Done()
	defer ch.cancel()
	dctx, cancel := context.WithTimeout(pctx, dial)
	conn, err := a.dialer.Dial(dctx, tgt)
	cancel()
	if err != nil {
		if a.drop(ch) {
			a.log.Info("dial failed", "channel", ch.id, "exposure", ch.exposureID, "error", err.Error())
			_ = s.send(ctx, &protocol.PrepareFailed{ChannelID: ch.id, Code: protocol.CodeOf(err)})
		}
		return
	}
	leg, _, err := websocket.Dial(pctx, a.base+"/v1/channels/"+ch.id+"/attach", &websocket.DialOptions{
		HTTPClient: a.client, HTTPHeader: http.Header{protocol.HeaderAttachTicket: {ticket}},
		Subprotocols: []string{protocol.TunnelSubprotocol}})
	if err != nil {
		_ = conn.SetLinger(0)
		_ = conn.Close()
		a.drop(ch)
		a.log.Info("provider attach failed", "channel", ch.id, "error", err.Error())
		return
	}
	leg.SetReadLimit(int64(max(dataMax+2, 3+protocol.MaxAbortDetail)))
	b := tunnel.New(a.hard, leg, conn, dataMax, grace, a.allowFor(ch.gen))
	if code := a.bind(ch, b); code != "" {
		b.Abort(code)
		a.done(ch, b.Result(), b.Diagnostics())
		return
	}
	t := time.AfterFunc(time.Until(readyBy), func() { b.Abort(protocol.CodePrepareTimeout) })
	err = b.WaitReady()
	t.Stop()
	if err != nil {
		a.done(ch, b.Result(), b.Diagnostics())
		return
	}
	if code := a.allowReady(ch); code != "" {
		b.Abort(code)
		reason := ""
		if code == protocol.CodeReadinessCheckFailed {
			reason = "timeout"
		}
		_ = s.send(ctx, &protocol.PrepareFailed{ChannelID: ch.id, Code: code, Reason: reason})
		a.done(ch, b.Result(), b.Diagnostics())
		return
	}
	result := b.Run()
	a.done(ch, result, b.Diagnostics())
}

// bind attaches the bridge unless the channel was aborted meanwhile.
func (a *Agent) bind(ch *channel, b *tunnel.Bridge) protocol.Code {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch.aborted == "" {
		ch.bridge = b
	}
	return ch.aborted
}

// drop forgets a channel that never got a bridge; it reports whether the
// channel was still live (not aborted by OIS or the license).
func (a *Agent) drop(ch *channel) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.channels, ch.id)
	if ch.decisionTimer != nil {
		ch.decisionTimer.Stop()
	}
	return ch.aborted == ""
}

func (a *Agent) done(ch *channel, r tunnel.Result, diag tunnel.Diagnostics) {
	a.mu.Lock()
	delete(a.channels, ch.id)
	if ch.decisionTimer != nil {
		ch.decisionTimer.Stop()
	}
	a.closed[string(r.Code)]++
	a.relayBytes += r.Up + r.Down
	a.mu.Unlock()
	a.log.Info("channel ended", "channel", ch.id, "exposure", ch.exposureID, "code", r.Code, "remote", r.Remote,
		"bytes_up", r.Up, "bytes_down", r.Down, "io_boundaries", diag)
}

// abort ends a channel; unknown channels are ignored (close_channel is
// idempotent, B1 §4).
func (a *Agent) abort(id string, code protocol.Code) {
	if _, ok := protocol.AbortReason(code); !ok {
		code = protocol.CodeProtocolError
	}
	a.mu.Lock()
	ch := a.channels[id]
	if ch == nil {
		a.mu.Unlock()
		return
	}
	b := ch.bridge
	a.markCancelledLocked(ch, code)
	code = ch.aborted
	a.mu.Unlock()
	ch.cancel()
	if b != nil {
		b.Abort(code)
	}
}

// Stats reports live counters (B4 §4 resource regression, O3).
func (a *Agent) Stats() protocol.OICStats {
	a.mu.Lock()
	var buffered int64
	for _, ch := range a.channels {
		if ch.bridge != nil {
			buffered += ch.bridge.Buffered()
		}
	}
	a.maxBuffered = max(a.maxBuffered, buffered)
	s := protocol.OICStats{
		AccessID: a.cfg.AccessID, Connected: a.sess != nil, Registered: a.registered, Epoch: a.epoch,
		Generation: a.gen, Channels: len(a.channels), RelayBytes: a.relayBytes, MaxBufferedBytes: a.maxBuffered,
		ConfigError: a.cfgErr, Closed: map[string]int64{}, Process: procstats.Read(),
	}
	for k, v := range a.closed {
		s.Closed[k] = v
	}
	if l := a.license.Load(); l != nil && l.gen == a.gen {
		s.LicenseRemainingMS = max(0, time.Until(l.deadline).Milliseconds())
	}
	scheduler, totals, current := a.runtimeScheduler, a.probeTotals, a.sess
	exporter := a.exporter
	diag := &protocol.ObservationRuntimeStats{}
	if current != nil {
		diag.PendingChecks = current.checks
	}
	a.mu.Unlock()
	activity := exporter.Activity()
	diag.ExporterActive = activity.Active
	diag.ExporterYoungestActiveAgeNS = activity.YoungestActiveAge.Nanoseconds()
	if scheduler != nil {
		r := scheduler.Stats()
		diag.ProbeJobs = r.Jobs
		diag.ProbeActive = r.Active
		diag.ProbeQueued = r.Queued
		diag.ProbeDue = r.Due
		totals.Started += r.Started
		totals.Finished += r.Finished
		totals.MaxActive = max(totals.MaxActive, r.MaxActive)
		totals.MaxQueued = max(totals.MaxQueued, r.MaxQueued)
		totals.MaxWait = max(totals.MaxWait, r.MaxWait)
		diag.ProbeOldestDueNS = r.OldestDueAge.Nanoseconds()
	}
	diag.ProbeStarted, diag.ProbeFinished = totals.Started, totals.Finished
	diag.MaxProbeActive, diag.MaxProbeQueued = totals.MaxActive, totals.MaxQueued
	diag.ProbeMaxWaitNS = totals.MaxWait.Nanoseconds()
	if current != nil {
		current.writer.mu.Lock()
		diag.PendingQueries = len(current.writer.queries)
		current.writer.mu.Unlock()
	}
	s.Observation = diag
	return s
}

var errRestart = errors.New("only exposures can be reloaded; restart oic to apply other changes")
