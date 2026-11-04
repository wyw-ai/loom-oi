// Package relay is the OIC runtime: one outbound mTLS control connection
// per Access (B1 §4), local re-check and dialing on prepare_channel
// (B2 §4), provider legs relayed through the tunnel bridge, the
// heartbeat-renewed local license (B2 §5), configuration reload and
// optional loopback diagnostics.
package relay

import (
	"context"
	"crypto/sha256"
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
	id, exposureID string
	gen            int64
	cancel         context.CancelFunc
	bridge         *tunnel.Bridge
	aborted        protocol.Code
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

	mu          sync.Mutex
	cfg         *config.Config
	cfgSum      [32]byte
	cfgErr      string
	sess        *session
	gen, epoch  int64
	lastEpoch   int64
	registered  bool
	dataMax     int
	channels    map[string]*channel
	closed      map[string]int64
	relayBytes  int64
	maxBuffered int64
	timer       *time.Timer
	diagAddr    string
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
		cfg:    cfg, cfgSum: sha256.Sum256(data),
		dataMax: cfg.Limits.DataMax, channels: map[string]*channel{}, closed: map[string]int64{},
	}
	a.hard, a.hardCancel = context.WithCancel(context.Background())
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
	c   *websocket.Conn
	mu  sync.Mutex
	seq uint64
}

func (s *session) send(ctx context.Context, m protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	return s.c.Write(ctx, websocket.MessageText, protocol.EncodeControl(m, s.seq))
}

func (s *session) read(ctx context.Context) (protocol.Message, error) {
	typ, data, err := s.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, protocol.Errorf(protocol.CodeProtocolError, "control messages must be text")
	}
	return protocol.DecodeControl(data)
}

// fail reports a protocol error to OIS before the session ends.
func (s *session) fail(ctx context.Context, err error) error {
	_ = s.send(ctx, &protocol.ControlError{Code: protocol.CodeOf(err), Detail: err.Error()})
	return err
}

func (a *Agent) session(ctx context.Context) (bool, error) {
	dctx, cancel := context.WithTimeout(ctx, connectTimeout)
	c, _, err := websocket.Dial(dctx, a.base+"/v1/access/control", &websocket.DialOptions{
		HTTPClient: a.client, Subprotocols: []string{protocol.ControlSubprotocol}})
	cancel()
	if err != nil {
		return false, err
	}
	defer c.CloseNow()
	a.mu.Lock()
	c.SetReadLimit(a.cfg.Limits.CtrlMax)
	last := a.lastEpoch
	a.mu.Unlock()
	s := &session{c: c}
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
	snapshot := a.welcome(s, w)
	defer a.lost(s)
	if err := s.send(ctx, &protocol.Register{Exposures: snapshot}); err != nil {
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
func (a *Agent) welcome(s *session, w *protocol.Welcome) []protocol.RegisterExposure {
	a.mu.Lock()
	a.sess, a.gen, a.epoch, a.registered = s, w.Generation, w.Epoch, false
	a.lastEpoch = max(a.lastEpoch, w.Epoch)
	a.dataMax = w.Limits.DataMax
	var stale []string
	for id, ch := range a.channels {
		if ch.gen != w.Generation {
			stale = append(stale, id)
		}
	}
	a.setLicense(w.Generation, w.LicenseTTLMS)
	snapshot := registration.Snapshot(a.cfg)
	a.mu.Unlock()
	for _, id := range stale {
		a.abort(id, protocol.CodeChannelInterrupted)
	}
	a.log.Info("control connected", "epoch", w.Epoch, "generation", w.Generation, "run_id", w.RunID)
	return snapshot
}

func (a *Agent) lost(s *session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == s {
		a.sess, a.registered = nil, false
	}
}

func (a *Agent) handle(ctx context.Context, s *session, m protocol.Message) error {
	switch v := m.(type) {
	case *protocol.RegisterAck:
		a.mu.Lock()
		a.registered = a.sess == s
		a.mu.Unlock()
		for _, r := range v.Rejected {
			a.log.Warn("exposure rejected by OIS", "exposure", r.ExposureID, "code", r.Code)
		}
	case *protocol.Heartbeat:
		a.mu.Lock()
		if a.sess == s {
			a.setLicense(a.gen, v.LicenseTTLMS)
		}
		a.mu.Unlock()
		return s.send(ctx, &protocol.HeartbeatAck{AckSeq: v.Seq})
	case *protocol.PrepareChannel:
		a.prepare(ctx, s, v)
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
	a.mu.Lock()
	ids := make([]string, 0, len(a.channels))
	for id := range a.channels {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	if len(ids) > 0 {
		a.log.Warn("license expired: aborting channels", "generation", l.gen, "channels", len(ids))
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
	a.mu.Lock()
	var code protocol.Code
	exp := a.cfg.Find(p.ExposureID)
	switch {
	case a.sess != s || !a.registered || p.Epoch != a.epoch || p.Generation != a.gen || a.channels[p.ChannelID] != nil:
		code = protocol.CodeProtocolError
	case exp == nil || !exp.IsEnabled() || exp.Revision != p.Revision:
		code = protocol.CodeLocalDenied
	case a.allowFor(a.gen)() != "":
		code = protocol.CodeLicenseExpired
	case len(a.channels) >= a.cfg.Limits.MaxLocalChannels:
		code = protocol.CodeResourceExhausted
	}
	if code != "" {
		a.mu.Unlock()
		a.log.Info("prepare refused", "channel", p.ChannelID, "exposure", p.ExposureID, "code", code)
		_ = s.send(ctx, &protocol.PrepareFailed{ChannelID: p.ChannelID, Code: code})
		return
	}
	deadline := time.Now().Add(limits.D(p.PrepareTTLMS))
	pctx, cancel := context.WithDeadline(ctx, deadline)
	ch := &channel{id: p.ChannelID, exposureID: p.ExposureID, gen: p.Generation, cancel: cancel}
	a.channels[ch.id] = ch
	tgt, dataMax, dial := exp.Target, a.dataMax, limits.D(a.cfg.Limits.DialMS)
	grace := limits.D(a.cfg.Limits.CloseMS)
	a.mu.Unlock()
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
		a.done(ch, b.Result())
		return
	}
	t := time.AfterFunc(time.Until(readyBy), func() { b.Abort(protocol.CodePrepareTimeout) })
	err = b.WaitReady()
	t.Stop()
	if err != nil {
		a.done(ch, b.Result())
		return
	}
	a.done(ch, b.Run())
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
	return ch.aborted == ""
}

func (a *Agent) done(ch *channel, r tunnel.Result) {
	a.mu.Lock()
	delete(a.channels, ch.id)
	a.closed[string(r.Code)]++
	a.relayBytes += r.Up + r.Down
	a.mu.Unlock()
	a.log.Info("channel ended", "channel", ch.id, "exposure", ch.exposureID, "code", r.Code, "remote", r.Remote,
		"bytes_up", r.Up, "bytes_down", r.Down)
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
	if b == nil && ch.aborted == "" {
		ch.aborted = code
		ch.cancel()
	}
	a.mu.Unlock()
	if b != nil {
		b.Abort(code)
	}
}

// Stats reports live counters (B4 §4 resource regression, O3).
func (a *Agent) Stats() protocol.OICStats {
	a.mu.Lock()
	defer a.mu.Unlock()
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
	return s
}

var errRestart = errors.New("only exposures can be reloaded; restart oic to apply other changes")
