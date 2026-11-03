package relay

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/policy"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Admin object limits.
const (
	maxLabels     = 32
	maxLabelValue = 256
	maxPins       = 2
)

func invalid(format string, args ...any) error {
	return protocol.Errorf(protocol.CodeInvalidRequest, format, args...)
}

func notFound(kind, id string) error {
	return protocol.Errorf(protocol.CodeNotFound, "%s %s not found", kind, id)
}

func versionConflict(current int64) error {
	return &protocol.Error{Code: protocol.CodeVersionConflict, ConfigVersion: current,
		Message: "If-Match does not match the current config_version"}
}

func auditEntry(action, objectType, objectID string, detail any) store.AuditEntry {
	b, _ := json.Marshal(detail)
	return store.AuditEntry{Action: action, ObjectType: objectType, ObjectID: objectID, Detail: b}
}

type buildFn func(now time.Time) ([]store.Mutation, []store.AuditEntry, error)

// write runs one configuration change: it checks If-Match against the
// snapshot, commits through the store, then applies the change and every
// resulting revocation under the coordinator lock before returning, so a
// later commit check always sees the new version (B1 §6, B3 §3).
func (h *Hub) write(ctx context.Context, p Principal, ifMatch int64, build buildFn) (int64, error) {
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	if err := h.resyncIfStale(ctx); err != nil {
		return 0, protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable: configuration writes are refused")
	}
	h.mu.Lock()
	now := h.now().UTC().Truncate(time.Microsecond)
	healthy, current := h.storeOK, h.version
	var muts []store.Mutation
	var audits []store.AuditEntry
	var err error
	if healthy && ifMatch == current {
		muts, audits, err = build(now)
	}
	h.mu.Unlock()
	switch {
	case !healthy:
		return 0, protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable: configuration writes are refused")
	case ifMatch != current:
		return 0, versionConflict(current)
	case err != nil:
		return 0, err
	}
	for i := range audits {
		audits[i].At = now
		audits[i].Actor = "admin:" + p.Subject
	}
	v, err := h.st.ApplyConfig(ctx, ifMatch, muts, audits)
	if err != nil {
		return 0, h.writeError(ctx, err)
	}
	h.mu.Lock()
	h.applyMutations(muts, v, h.now())
	h.mu.Unlock()
	h.log.Info("config applied", "config_version", v, "actor", p.Subject, "mutations", len(muts))
	return v, nil
}

// writeError maps a failed ApplyConfig to the API (B3 §3). A failure whose
// outcome is unknown marks the snapshot stale so it is reloaded first.
func (h *Hub) writeError(ctx context.Context, err error) error {
	var conflict *store.ConflictError
	switch {
	case errors.As(err, &conflict):
		h.mu.Lock()
		h.stale = conflict.Current != h.version
		h.mu.Unlock()
		_ = h.resyncIfStale(ctx)
		return versionConflict(conflict.Current)
	case errors.Is(err, store.ErrBusy):
		return protocol.Errorf(protocol.CodeStoreBusy, "store busy; retry")
	case errors.Is(err, store.ErrInvalid):
		return protocol.Errorf(protocol.CodeInvalidRequest, "%v", err)
	}
	h.mu.Lock()
	h.stale = true
	h.mu.Unlock()
	h.storeFailed(err)
	return protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable: configuration writes are refused")
}

// resyncIfStale reloads accesses, tokens and policies from the store after
// a write with an unknown outcome and enforces them. Caller holds adminMu.
func (h *Hub) resyncIfStale(ctx context.Context) error {
	h.mu.Lock()
	stale := h.stale
	h.mu.Unlock()
	if !stale {
		return nil
	}
	snap, err := h.st.LoadSnapshot(ctx)
	if err != nil {
		h.storeFailed(err)
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.meta = snap.Meta
	h.version = snap.Meta.ConfigVersion
	next := map[string]*access{}
	for _, rec := range snap.Accesses {
		a := h.accesses[rec.AccessID]
		if a == nil {
			a = &access{}
		}
		a.rec = rec
		next[rec.AccessID] = a
	}
	for id, a := range h.accesses {
		if next[id] == nil {
			if a.sess != nil {
				h.endSession(a.sess, causeRevoked, protocol.CodeRevoked, "access deleted")
			}
			h.dropRegistry(id)
		}
	}
	h.accesses = next
	h.tokens = map[string]store.Token{}
	for _, t := range snap.Tokens {
		h.tokens[t.TokenID] = t
	}
	h.policies = map[string]store.Policy{}
	for _, pol := range snap.Policies {
		h.policies[pol.PolicyID] = pol
	}
	h.rebuildIndexes()
	h.stale = false
	h.enforce(now)
	h.log.Info("configuration reloaded", "config_version", h.version)
	return nil
}

func (h *Hub) dropRegistry(accessID string) {
	for id, e := range h.exposures {
		if e.rec.AccessID == accessID {
			delete(h.exposures, id)
		}
	}
	delete(h.regDirty, accessID)
}

// applyMutations mirrors a committed change in memory. Caller holds h.mu.
func (h *Hub) applyMutations(muts []store.Mutation, version int64, now time.Time) {
	var revoked []*session
	for _, m := range muts {
		switch v := m.(type) {
		case store.PutAccess:
			if a := h.accesses[v.Access.AccessID]; a != nil {
				a.rec = v.Access
			} else {
				h.accesses[v.Access.AccessID] = &access{rec: v.Access}
			}
		case store.DeleteAccess:
			if a := h.accesses[v.AccessID]; a != nil && a.sess != nil {
				revoked = append(revoked, a.sess)
			}
			delete(h.accesses, v.AccessID)
			h.dropRegistry(v.AccessID)
		case store.PutToken:
			h.tokens[v.Token.TokenID] = v.Token
		case store.RevokeToken:
			if t, ok := h.tokens[v.TokenID]; ok && t.RevokedAt == nil {
				at := v.At
				t.RevokedAt = &at
				h.tokens[v.TokenID] = t
			}
		case store.PutPolicy:
			pol := v.Policy
			pol.UpdatedVersion = version
			h.policies[pol.PolicyID] = pol
		case store.DeletePolicy:
			delete(h.policies, v.PolicyID)
		}
	}
	h.version = version
	h.rebuildIndexes()
	for _, s := range revoked {
		h.endSession(s, causeRevoked, protocol.CodeRevoked, "access deleted")
	}
	h.enforce(now)
}

// enforce ends the sessions and channels the current configuration no
// longer allows (B2 §2: revoked → Revoked, expired → Unauthenticated).
// Caller holds h.mu.
func (h *Hub) enforce(now time.Time) {
	for _, a := range h.accesses {
		if s := a.sess; s != nil && (!a.rec.Enabled || !slices.Contains(a.rec.SPKIPins, s.pin)) {
			h.endSession(s, causeRevoked, protocol.CodeRevoked, "access disabled or certificate unpinned")
		}
	}
	for _, ch := range h.channels {
		if ch.state != statePreparing && ch.state != stateOpen {
			continue
		}
		code := h.tokenProblem(ch, now)
		if a := h.accesses[ch.accessID]; code == "" && (a == nil || !a.rec.Enabled || !h.permitted(ch.subject, ch.exposureID)) {
			code = protocol.CodeRevoked
		}
		if code != "" {
			h.terminate(ch, code, now)
		}
	}
}

func accessDetail(a store.Access) map[string]any {
	return map[string]any{"enabled": a.Enabled, "spki_pins": a.SPKIPins, "max_exposures": a.MaxExposures, "labels": a.Labels}
}

func (h *Hub) accessView(a *access) protocol.AccessView {
	return protocol.AccessView{
		AccessID: a.rec.AccessID, Enabled: a.rec.Enabled, SPKIPins: slices.Clone(a.rec.SPKIPins),
		MaxExposures: a.rec.MaxExposures, Labels: a.rec.Labels, Online: a.online(), Generation: a.gen,
		CreatedAt: a.rec.CreatedAt, UpdatedAt: a.rec.UpdatedAt,
	}
}

// ListAccesses returns every Access sorted by ID.
func (h *Hub) ListAccesses() []protocol.AccessView {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []protocol.AccessView{}
	for _, a := range h.accesses {
		out = append(out, h.accessView(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccessID < out[j].AccessID })
	return out
}

// GetAccess returns one Access.
func (h *Hub) GetAccess(id string) (protocol.AccessView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := h.accesses[id]
	if a == nil {
		return protocol.AccessView{}, notFound("access", id)
	}
	return h.accessView(a), nil
}

// patchAccess validates spec and applies its set fields. Caller holds h.mu.
func (h *Hub) patchAccess(a *store.Access, spec protocol.AccessSpec) error {
	if spec.Enabled != nil {
		a.Enabled = *spec.Enabled
	}
	if spec.SPKIPins != nil {
		if len(spec.SPKIPins) > maxPins {
			return invalid("an access has at most %d pins", maxPins)
		}
		for i, pin := range spec.SPKIPins {
			if !identity.ValidPin(pin) || slices.Contains(spec.SPKIPins[:i], pin) {
				return invalid("spki_pins must be distinct lowercase hex SHA-256 values")
			}
			if owner, ok := h.pins[pin]; ok && owner != a.AccessID {
				return invalid("pin is already used by access %s", owner)
			}
		}
		a.SPKIPins = slices.Clone(spec.SPKIPins)
	}
	if spec.MaxExposures != nil {
		if *spec.MaxExposures < 1 || *spec.MaxExposures > h.lim.MaxExposures {
			return invalid("max_exposures must be within 1..%d", h.lim.MaxExposures)
		}
		a.MaxExposures = *spec.MaxExposures
	}
	if spec.Labels != nil {
		if len(spec.Labels) > maxLabels {
			return invalid("at most %d labels", maxLabels)
		}
		for k, v := range spec.Labels {
			if !protocol.ValidName(k) || len(v) > maxLabelValue {
				return invalid("label %q is invalid", k)
			}
		}
		a.Labels = spec.Labels
	}
	return nil
}

// CreateAccess adds an Access.
func (h *Hub) CreateAccess(ctx context.Context, p Principal, ifMatch int64, spec protocol.AccessSpec) (int64, error) {
	return h.write(ctx, p, ifMatch, func(now time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		if !protocol.ValidAccessID(spec.AccessID) {
			return nil, nil, invalid("access_id is malformed")
		}
		if h.accesses[spec.AccessID] != nil {
			return nil, nil, invalid("access %s already exists", spec.AccessID)
		}
		a := store.Access{AccessID: spec.AccessID, Enabled: true, SPKIPins: []string{},
			MaxExposures: h.lim.MaxExposures, Labels: map[string]string{}, CreatedAt: now, UpdatedAt: now}
		if err := h.patchAccess(&a, spec); err != nil {
			return nil, nil, err
		}
		return []store.Mutation{store.PutAccess{Access: a}},
			[]store.AuditEntry{auditEntry("access_created", "access", a.AccessID, accessDetail(a))}, nil
	})
}

// PatchAccess changes an Access; removing a pin or disabling the Access
// closes its control connection and channels (B2 §2).
func (h *Hub) PatchAccess(ctx context.Context, p Principal, ifMatch int64, id string, spec protocol.AccessSpec) (int64, error) {
	return h.write(ctx, p, ifMatch, func(now time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		cur := h.accesses[id]
		if cur == nil {
			return nil, nil, notFound("access", id)
		}
		if spec.AccessID != "" && spec.AccessID != id {
			return nil, nil, invalid("access_id cannot change")
		}
		a := cur.rec
		a.SPKIPins = slices.Clone(a.SPKIPins)
		if err := h.patchAccess(&a, spec); err != nil {
			return nil, nil, err
		}
		a.UpdatedAt = now
		return []store.Mutation{store.PutAccess{Access: a}},
			[]store.AuditEntry{auditEntry("access_updated", "access", id, accessDetail(a))}, nil
	})
}

// DeleteAccess removes an Access and its registry.
func (h *Hub) DeleteAccess(ctx context.Context, p Principal, ifMatch int64, id string) (int64, error) {
	return h.write(ctx, p, ifMatch, func(time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		if h.accesses[id] == nil {
			return nil, nil, notFound("access", id)
		}
		return []store.Mutation{store.DeleteAccess{AccessID: id}},
			[]store.AuditEntry{auditEntry("access_deleted", "access", id, map[string]string{})}, nil
	})
}

func tokenView(t store.Token) protocol.TokenView {
	return protocol.TokenView{TokenID: t.TokenID, Subject: t.Subject, Scope: t.Scope,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt}
}

// ListTokens returns token records without secrets, sorted by ID.
func (h *Hub) ListTokens() []protocol.TokenView {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []protocol.TokenView{}
	for _, t := range h.tokens {
		out = append(out, tokenView(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TokenID < out[j].TokenID })
	return out
}

// CreateToken issues a token; the response holds the only copy of its
// secret (B2 §2).
func (h *Hub) CreateToken(ctx context.Context, p Principal, ifMatch int64, req protocol.TokenCreate) (protocol.TokenCreated, error) {
	var out protocol.TokenCreated
	v, err := h.write(ctx, p, ifMatch, func(now time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		t, bearer, err := NewTokenRecord(h.lim, req, now)
		if err != nil {
			return nil, nil, err
		}
		out = protocol.TokenCreated{TokenID: t.TokenID, Token: bearer, Subject: t.Subject, Scope: t.Scope, ExpiresAt: t.ExpiresAt}
		return []store.Mutation{store.PutToken{Token: t}},
			[]store.AuditEntry{auditEntry("token_created", "token", t.TokenID, tokenView(t))}, nil
	})
	if err != nil {
		return protocol.TokenCreated{}, err
	}
	out.ConfigVersion = v
	return out, nil
}

// NewTokenRecord validates a token request and creates its record and
// bearer string; `ois init` uses it for the first admin token.
func NewTokenRecord(l limits.Limits, req protocol.TokenCreate, now time.Time) (store.Token, string, error) {
	switch {
	case !protocol.ValidSubject(req.Subject):
		return store.Token{}, "", invalid("subject is malformed")
	case req.Scope != protocol.ScopeConsumer && req.Scope != protocol.ScopeAdmin:
		return store.Token{}, "", invalid("scope must be %q or %q", protocol.ScopeConsumer, protocol.ScopeAdmin)
	case req.TTLSeconds < 0 || req.TTLSeconds > l.TokenTTLMaxMS/1000:
		return store.Token{}, "", invalid("ttl_seconds must be within 1..%d", l.TokenTTLMaxMS/1000)
	}
	ttl := limits.D(l.TokenTTLMS)
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	id, bearer, hash := identity.NewToken()
	return store.Token{TokenID: id, Subject: req.Subject, Scope: req.Scope, SecretSHA256: hash,
		ExpiresAt: now.Add(ttl), CreatedAt: now}, bearer, nil
}

// RevokeToken revokes a token; its channels end with Revoked.
func (h *Hub) RevokeToken(ctx context.Context, p Principal, ifMatch int64, id string) (int64, error) {
	return h.write(ctx, p, ifMatch, func(now time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		if _, ok := h.tokens[id]; !ok {
			return nil, nil, notFound("token", id)
		}
		return []store.Mutation{store.RevokeToken{TokenID: id, At: now}},
			[]store.AuditEntry{auditEntry("token_revoked", "token", id, map[string]string{})}, nil
	})
}

func policyView(p store.Policy) protocol.PolicyView {
	return protocol.PolicyView{PolicyID: p.PolicyID, Subject: p.Subject, ExposureSelector: p.ExposureSelector,
		Actions: slices.Clone(p.Actions), Enabled: p.Enabled, UpdatedVersion: p.UpdatedVersion}
}

// ListPolicies returns every policy sorted by ID.
func (h *Hub) ListPolicies() []protocol.PolicyView {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []protocol.PolicyView{}
	for _, p := range h.policies {
		out = append(out, policyView(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PolicyID < out[j].PolicyID })
	return out
}

// GetPolicy returns one policy.
func (h *Hub) GetPolicy(id string) (protocol.PolicyView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.policies[id]
	if !ok {
		return protocol.PolicyView{}, notFound("policy", id)
	}
	return policyView(p), nil
}

func patchPolicy(p *store.Policy, spec protocol.PolicySpec) error {
	if spec.Subject != "" {
		if !protocol.ValidSubject(spec.Subject) {
			return invalid("subject is malformed")
		}
		p.Subject = spec.Subject
	}
	if spec.ExposureSelector != "" {
		if !policy.ValidSelector(spec.ExposureSelector) {
			return invalid("exposure_selector must be an exposure ID or <access_id>/*")
		}
		p.ExposureSelector = spec.ExposureSelector
	}
	if spec.Actions != nil {
		if !policy.ValidActions(spec.Actions) {
			return invalid("actions must be distinct values of discover and open")
		}
		p.Actions = slices.Clone(spec.Actions)
	}
	if spec.Enabled != nil {
		p.Enabled = *spec.Enabled
	}
	return nil
}

// CreatePolicy adds a policy.
func (h *Hub) CreatePolicy(ctx context.Context, pr Principal, ifMatch int64, spec protocol.PolicySpec) (int64, error) {
	return h.write(ctx, pr, ifMatch, func(time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		if !protocol.ValidPolicyID(spec.PolicyID) {
			return nil, nil, invalid("policy_id is malformed")
		}
		if _, ok := h.policies[spec.PolicyID]; ok {
			return nil, nil, invalid("policy %s already exists", spec.PolicyID)
		}
		if spec.Subject == "" || spec.ExposureSelector == "" || spec.Actions == nil {
			return nil, nil, invalid("subject, exposure_selector and actions are required")
		}
		p := store.Policy{PolicyID: spec.PolicyID, Enabled: true}
		if err := patchPolicy(&p, spec); err != nil {
			return nil, nil, err
		}
		return []store.Mutation{store.PutPolicy{Policy: p}},
			[]store.AuditEntry{auditEntry("policy_created", "policy", p.PolicyID, policyView(p))}, nil
	})
}

// PatchPolicy changes a policy; channels it no longer allows end with
// Revoked before the call returns.
func (h *Hub) PatchPolicy(ctx context.Context, pr Principal, ifMatch int64, id string, spec protocol.PolicySpec) (int64, error) {
	return h.write(ctx, pr, ifMatch, func(time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		cur, ok := h.policies[id]
		if !ok {
			return nil, nil, notFound("policy", id)
		}
		if spec.PolicyID != "" && spec.PolicyID != id {
			return nil, nil, invalid("policy_id cannot change")
		}
		p := cur
		if err := patchPolicy(&p, spec); err != nil {
			return nil, nil, err
		}
		return []store.Mutation{store.PutPolicy{Policy: p}},
			[]store.AuditEntry{auditEntry("policy_updated", "policy", id, policyView(p))}, nil
	})
}

// DeletePolicy removes a policy.
func (h *Hub) DeletePolicy(ctx context.Context, pr Principal, ifMatch int64, id string) (int64, error) {
	return h.write(ctx, pr, ifMatch, func(time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		if _, ok := h.policies[id]; !ok {
			return nil, nil, notFound("policy", id)
		}
		return []store.Mutation{store.DeletePolicy{PolicyID: id}},
			[]store.AuditEntry{auditEntry("policy_deleted", "policy", id, map[string]string{})}, nil
	})
}

// Audit pages through the audit log after seq.
func (h *Hub) Audit(ctx context.Context, after int64, limit int) (protocol.AuditList, error) {
	if limit == 0 {
		limit = h.lim.PageDefault
	}
	if limit < 0 || limit > h.lim.PageMax || after < 0 {
		return protocol.AuditList{}, invalid("limit must be within 1..%d and after non-negative", h.lim.PageMax)
	}
	entries, err := h.st.ListAudit(ctx, after, limit)
	if err != nil {
		h.storeFailed(err)
		return protocol.AuditList{}, protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable")
	}
	out := protocol.AuditList{Items: []protocol.AuditView{}}
	for _, e := range entries {
		out.Items = append(out.Items, protocol.AuditView{Seq: e.Seq, At: e.At, Actor: e.Actor, Action: e.Action,
			ObjectType: e.ObjectType, ObjectID: e.ObjectID, ConfigVersion: e.ConfigVersion, Detail: e.Detail})
	}
	return out, nil
}

// storeFailed marks the store unavailable after an Unavailable error.
func (h *Hub) storeFailed(err error) {
	if !errors.Is(err, store.ErrUnavailable) {
		return
	}
	h.mu.Lock()
	was := h.storeOK
	h.storeOK = false
	h.mu.Unlock()
	if was {
		h.log.Error("store unavailable", "error", strings.TrimSpace(err.Error()))
	}
}
