// Package relay is the OIS channel coordinator (B1 §6). One lock
// linearizes channel state transitions, the application of configuration
// versions and revocation; per-leg ordered write queues carry READY, DATA,
// FIN and ABORT; control sessions register exposures and renew OIC
// licenses (B2 §5). Channels, tickets and idempotency records live only in
// memory (B3 §2).
package relay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/catalog"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/policy"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/procstats"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Role identifies a data leg.
type Role int

// Leg roles.
const (
	Consumer Role = iota
	Provider
)

func (r Role) String() string {
	if r == Consumer {
		return "consumer"
	}
	return "provider"
}

// Principal is an authenticated bearer credential.
type Principal struct {
	TokenID string
	Subject string
	Scope   string
}

// Config wires a Hub to its store and startup state.
type Config struct {
	Limits   limits.Limits
	Store    store.Store
	Snapshot store.Snapshot
	Epoch    int64
	RunID    string
	Logger   *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

type access struct {
	rec     store.Access
	sess    *session
	gen     int64
	missed  int
	offline bool
}

func (a *access) online() bool { return a.sess != nil && a.sess.registered }

type exposure struct {
	rec store.Exposure
	gen int64
}

type idemKey struct{ subject, requestID string }

// regPending is an Access registry snapshot not yet persisted.
type regPending struct {
	seq    int64
	audits []store.AuditEntry
}

// Hub is the single OIS coordinator.
type Hub struct {
	lim   limits.Limits
	st    store.Store
	log   *slog.Logger
	now   func() time.Time
	runID string
	key   []byte

	ctx    context.Context
	cancel context.CancelFunc
	loops  sync.WaitGroup // background loops and per-connection helpers
	conns  sync.WaitGroup // attached legs and control sessions

	relayBytes  atomic.Int64
	inflight    atomic.Int64
	maxInflight atomic.Int64

	adminMu sync.Mutex
	// adminSlots bounds configuration writes waiting for adminMu by
	// WRITE_QUEUE: beyond it a write is refused with StoreBusy at once
	// instead of queueing without bound (B3 §3).
	adminSlots chan struct{}

	mu              sync.Mutex
	meta            store.Meta
	version         int64
	epoch           int64
	storeOK         bool
	writeFailure    string // category of a latched write failure; "" when none
	storeFailures   uint64 // changes on each observed failure; fences stale probes
	stale           bool   // a write had an unknown outcome; reload before the next
	shutting        bool
	accesses        map[string]*access
	pins            map[string]string
	tokens          map[string]store.Token
	policies        map[string]store.Policy
	policyList      []store.Policy
	exposures       map[string]*exposure
	channels        map[string]*channel
	idem            map[idemKey]*channel
	quota           quota
	regDirty        map[string]*regPending
	regSeq          int64
	epochDirty      bool
	epochAudit      *store.AuditEntry
	rollbackAudited bool
	kick            chan struct{}
}

// New builds a hub from the startup snapshot. Call Start to run its
// background loops.
func New(cfg Config) *Hub {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &Hub{
		lim: cfg.Limits, st: cfg.Store, log: log, now: now, runID: cfg.RunID,
		key:        protocol.RandomBytes(32),
		meta:       cfg.Snapshot.Meta,
		version:    cfg.Snapshot.Meta.ConfigVersion,
		epoch:      cfg.Epoch,
		storeOK:    true,
		accesses:   map[string]*access{},
		tokens:     map[string]store.Token{},
		policies:   map[string]store.Policy{},
		exposures:  map[string]*exposure{},
		channels:   map[string]*channel{},
		idem:       map[idemKey]*channel{},
		quota:      newQuota(),
		regDirty:   map[string]*regPending{},
		kick:       make(chan struct{}, 1),
		adminSlots: make(chan struct{}, max(1, cfg.Limits.WriteQueue)),
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	for _, a := range cfg.Snapshot.Accesses {
		h.accesses[a.AccessID] = &access{rec: a}
	}
	for _, t := range cfg.Snapshot.Tokens {
		h.tokens[t.TokenID] = t
	}
	for _, p := range cfg.Snapshot.Policies {
		h.policies[p.PolicyID] = p
	}
	for _, e := range cfg.Snapshot.Exposures {
		h.exposures[e.ExposureID] = &exposure{rec: e}
	}
	h.rebuildIndexes()
	return h
}

func (h *Hub) rebuildIndexes() {
	h.pins = map[string]string{}
	for id, a := range h.accesses {
		for _, p := range a.rec.SPKIPins {
			h.pins[p] = id
		}
	}
	h.policyList = h.policyList[:0]
	for _, p := range h.policies {
		h.policyList = append(h.policyList, p)
	}
}

func ms(d time.Duration) int64 { return int64(d / time.Millisecond) }

// ticket derives a channel's one-time ticket for role. Only its SHA-256 is
// kept per channel; idempotent replays recompute the same consumer ticket.
func (h *Hub) ticket(role Role, channelID string) string {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte{byte(role)})
	m.Write([]byte(channelID))
	return "tk_" + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

var errUnauthenticated = protocol.Errorf(protocol.CodeUnauthenticated, "missing, invalid, expired or revoked credential")

// stillValid re-checks a credential where its request takes effect: a token
// revoked or expired after the request was authenticated (for instance
// while the body was still arriving) must not act (B2 §2, B1 §6). Caller
// holds h.mu.
func (h *Hub) stillValid(p Principal, now time.Time) error {
	tok, ok := h.tokens[p.TokenID]
	if !ok || tok.RevokedAt != nil || !now.Before(tok.ExpiresAt) {
		return errUnauthenticated
	}
	return nil
}

// Authenticate checks a bearer token (B2 §2).
func (h *Hub) Authenticate(bearer string) (Principal, error) {
	id, secret, ok := identity.ParseToken(bearer)
	if !ok {
		return Principal{}, errUnauthenticated
	}
	h.mu.Lock()
	tok, found := h.tokens[id]
	now := h.now()
	h.mu.Unlock()
	if !found || !identity.SecretMatches(secret, tok.SecretSHA256) || tok.RevokedAt != nil || !now.Before(tok.ExpiresAt) {
		return Principal{}, errUnauthenticated
	}
	return Principal{TokenID: id, Subject: tok.Subject, Scope: tok.Scope}, nil
}

// AccessForPin maps a client certificate pin to its enabled Access.
func (h *Hub) AccessForPin(pin string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.pins[pin]
	if !ok || !h.accesses[id].rec.Enabled {
		return "", false
	}
	return id, true
}

func (h *Hub) allowed(subject, exposureID, action string) bool {
	return policy.Allowed(h.policyList, subject, exposureID, action)
}

// permitted is the authorization a channel needs for its whole life: the
// B2 §3 chain grants both discover and open.
func (h *Hub) permitted(subject, exposureID string) bool {
	return h.allowed(subject, exposureID, protocol.ActionDiscover) && h.allowed(subject, exposureID, protocol.ActionOpen)
}

func (e *exposure) view(online bool) protocol.ExposureView {
	return protocol.ExposureView{
		ExposureID: e.rec.ExposureID, AccessID: e.rec.AccessID, Revision: e.rec.Revision,
		Protocol: e.rec.Protocol, TransportProfile: e.rec.TransportProfile,
		Description: e.rec.Description, AdmissionMode: e.rec.AdmissionMode,
		Readiness: protocol.ReadinessUnknown, AccessOnline: online,
	}
}

// Discover lists the exposures the subject may discover (B1 §3).
func (h *Hub) Discover(p Principal, limit int, cursor string) (protocol.ExposureList, error) {
	if limit == 0 {
		limit = h.lim.PageDefault
	}
	if limit < 0 || limit > h.lim.PageMax {
		return protocol.ExposureList{}, protocol.Errorf(protocol.CodeInvalidRequest, "limit must be within 1..%d", h.lim.PageMax)
	}
	h.mu.Lock()
	items := []protocol.ExposureView{}
	for id, e := range h.exposures {
		if !h.allowed(p.Subject, id, protocol.ActionDiscover) {
			continue
		}
		a := h.accesses[e.rec.AccessID]
		items = append(items, e.view(a != nil && a.online()))
	}
	h.mu.Unlock()
	page, next, err := catalog.Page(items, cursor, limit)
	if err != nil {
		return protocol.ExposureList{}, err
	}
	return protocol.ExposureList{Items: page, NextCursor: next}, nil
}

// OpenResult is an accepted Open. Ticket is set while the consumer ticket
// is unconsumed; the server turns it into the attach object.
type OpenResult struct {
	Response protocol.OpenResponse
	Ticket   string
	Replay   bool
}

func validOpen(req protocol.OpenRequest) error {
	_, _, ok := protocol.SplitExposureID(req.ExposureID)
	switch {
	case !ok:
		return protocol.Errorf(protocol.CodeInvalidRequest, "exposure_id is malformed")
	case !protocol.ValidRevision(req.ExpectedRevision):
		return protocol.Errorf(protocol.CodeInvalidRequest, "expected_revision is malformed")
	case req.ClientRequestID != "" && !protocol.ValidClientRequestID(req.ClientRequestID):
		return protocol.Errorf(protocol.CodeInvalidRequest, "client_request_id is malformed")
	case req.Admission != "" && req.Admission != protocol.AdmissionRequireReady:
		return protocol.Errorf(protocol.CodeInvalidRequest, "admission may only be %q", protocol.AdmissionRequireReady)
	}
	return nil
}

// Open runs the B1 §7 precheck and creates a Preparing channel.
func (h *Hub) Open(p Principal, req protocol.OpenRequest) (OpenResult, error) {
	if err := validOpen(req); err != nil {
		return OpenResult{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if err := h.stillValid(p, now); err != nil {
		return OpenResult{}, err
	}
	key := idemKey{p.Subject, req.ClientRequestID}
	if req.ClientRequestID != "" {
		if ch := h.idem[key]; ch != nil {
			if ch.exposureID != req.ExposureID || ch.revision != req.ExpectedRevision || ch.reqAdmission != req.Admission {
				return OpenResult{}, protocol.Errorf(protocol.CodeIdempotencyConflict, "client_request_id was used with other parameters")
			}
			return h.openResult(ch, now, true), nil
		}
	}
	if h.shutting {
		return OpenResult{}, protocol.Errorf(protocol.CodeShutdown, "OIS is stopping")
	}
	exp := h.exposures[req.ExposureID]
	if exp == nil || !h.allowed(p.Subject, req.ExposureID, protocol.ActionDiscover) {
		return OpenResult{}, protocol.Errorf(protocol.CodeNotFound, "exposure not found")
	}
	if !h.allowed(p.Subject, req.ExposureID, protocol.ActionOpen) {
		return OpenResult{}, protocol.Errorf(protocol.CodeDenied, "open is not granted")
	}
	if exp.rec.Revision != req.ExpectedRevision {
		return OpenResult{}, protocol.Errorf(protocol.CodeRevisionMismatch, "expected_revision is not current")
	}
	a := h.accesses[exp.rec.AccessID]
	if a == nil || !a.online() {
		return OpenResult{}, protocol.Errorf(protocol.CodeAccessOffline, "access is offline")
	}
	mode := protocol.StricterAdmission(exp.rec.AdmissionMode, req.Admission)
	// P1 has no probes: readiness is always unknown, so require_ready can
	// never be met and NotReady never occurs (B1 §1, TV-B1-18).
	if mode == protocol.AdmissionRequireReady {
		return OpenResult{}, protocol.Errorf(protocol.CodeReadinessRequired, "require_ready needs a valid Ready observation")
	}
	if !h.quota.admit(h.lim, a.rec.AccessID, p.Subject) {
		return OpenResult{}, protocol.Errorf(protocol.CodeResourceExhausted, "channel quota exhausted")
	}
	if !h.storeOK {
		return OpenResult{}, protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable: new channels are refused")
	}
	if h.meta.Maintenance {
		return OpenResult{}, protocol.Errorf(protocol.CodeMaintenance, "maintenance mode: new channels are refused until an administrator resumes")
	}
	ch := &channel{
		id: protocol.NewID("ch_"), exposureID: exp.rec.ExposureID, accessID: a.rec.AccessID,
		revision: exp.rec.Revision, subject: p.Subject, tokenID: p.TokenID,
		admission: mode, readiness: protocol.ReadinessUnknown, reqAdmission: req.Admission,
		requestID: req.ClientRequestID, epoch: a.sess.epoch, gen: a.sess.gen,
		created: now, deadline: now.Add(limits.D(h.lim.PrepareMS)),
	}
	ch.setState(statePreparing)
	for _, r := range []Role{Consumer, Provider} {
		ch.ticketHash[r] = sha256.Sum256([]byte(h.ticket(r, ch.id)))
	}
	h.quota.take(ch)
	h.channels[ch.id] = ch
	if req.ClientRequestID != "" {
		h.idem[key] = ch
	}
	a.sess.send(&protocol.PrepareChannel{
		ChannelID: ch.id, ExposureID: ch.exposureID, Revision: ch.revision, Subject: ch.subject,
		Epoch: ch.epoch, Generation: ch.gen, PrepareTTLMS: ms(ch.deadline.Sub(now)),
		ProviderTicket: h.ticket(Provider, ch.id),
	})
	h.log.Info("open", "channel", ch.id, "exposure", ch.exposureID, "subject", ch.subject,
		"admission", ch.admission, "readiness", ch.readiness)
	return h.openResult(ch, now, false), nil
}

func (h *Hub) openResult(ch *channel, now time.Time, replay bool) OpenResult {
	r := OpenResult{Replay: replay, Response: protocol.OpenResponse{
		ChannelID: ch.id, State: stateNames[ch.state], ExposureID: ch.exposureID, Revision: ch.revision,
		AdmissionMode: ch.admission, Readiness: ch.readiness,
	}}
	if ch.state == statePreparing {
		left := max(ms(ch.deadline.Sub(now)), 0)
		r.Response.PrepareExpiresInMS = left
		if !ch.used[Consumer] {
			r.Ticket = h.ticket(Consumer, ch.id)
			r.Response.Attach = &protocol.Attach{Ticket: r.Ticket, ExpiresInMS: left}
		}
	}
	return r
}

// Channel returns a channel visible to its owner (B1 §3).
func (h *Hub) Channel(p Principal, id string) (protocol.ChannelView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := h.channels[id]
	if ch == nil || ch.subject != p.Subject {
		return protocol.ChannelView{}, protocol.Errorf(protocol.CodeNotFound, "channel not found")
	}
	return ch.view(), nil
}

// CloseChannel closes a channel for its owner. changed is false when the
// channel was already Closed.
func (h *Hub) CloseChannel(p Principal, id string) (view protocol.ChannelView, changed bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := h.channels[id]
	if ch == nil || ch.subject != p.Subject {
		return protocol.ChannelView{}, false, protocol.Errorf(protocol.CodeNotFound, "channel not found")
	}
	if ch.state == stateClosed {
		return ch.view(), false, nil
	}
	h.terminate(ch, protocol.CodeClosedByClient, h.now())
	return ch.view(), true, nil
}

// ConfigView reports the configuration and run state.
func (h *Hub) ConfigView() protocol.ConfigView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return protocol.ConfigView{
		ConfigVersion: h.version, Epoch: h.epoch, StoreID: h.meta.StoreID, Backend: h.st.Backend(),
		RunID: h.runID, Maintenance: h.meta.Maintenance, StoreHealthy: h.storeOK,
	}
}

// Stats reports live counters for resource regression checks (B4 §4).
func (h *Hub) Stats() protocol.OISStats {
	h.mu.Lock()
	var s protocol.OISStats
	for _, ch := range h.channels {
		switch ch.state {
		case statePreparing:
			s.Channels.Preparing++
			for _, used := range ch.used {
				if !used {
					s.Tickets++
				}
			}
		case stateOpen:
			s.Channels.Open++
		case stateClosing:
			s.Channels.Closing++
		default:
			s.Channels.Closed++
		}
	}
	for _, a := range h.accesses {
		if a.online() {
			s.AccessesOnline++
		}
	}
	s.StoreHealthy = h.storeOK
	s.Epoch = h.epoch
	h.mu.Unlock()
	s.RelayBytes = h.relayBytes.Load()
	s.MaxBufferedBytes = h.maxInflight.Load()
	s.Process = procstats.Read()
	return s
}

func (h *Hub) addInflight(n int64) {
	v := h.inflight.Add(n)
	for {
		m := h.maxInflight.Load()
		if v <= m || h.maxInflight.CompareAndSwap(m, v) {
			return
		}
	}
}
