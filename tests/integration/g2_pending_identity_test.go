package integration

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const g2PendingHeader = "X-OI-Diagnostic-Sample"

func g2PendingEnabled(vector, value string) bool {
	return vector == "TV-G2-11" && value == "1"
}

func g2PendingConfigure(cfg map[string]any, enabled bool) {
	if enabled {
		cfg["log_level"] = "debug"
	}
}

// This is a bounded request association, not another acceptance predicate or
// a server history. Overflow and entropy failure never suppress a request.
type g2PendingRequest struct {
	Attempt  int                `json:"attempt"`
	Phase    string             `json:"phase"`
	SampleID *string            `json:"sample_id"`
	IDError  *string            `json:"id_error"`
	OISCall  *g2JointCall       `json:"ois_call"`
	OISStats *protocol.OISStats `json:"ois_stats"`
	entropy  io.Reader
}

type g2PendingDiagnostics struct {
	Schema   string             `json:"schema"`
	Test     string             `json:"test"`
	Backend  string             `json:"backend"`
	Round    int                `json:"round"`
	OISPID   int                `json:"ois_pid"`
	Requests []g2PendingRequest `json:"requests"`
	Overflow bool               `json:"overflow"`
	Omitted  int                `json:"omitted"`
	entropy  io.Reader
}

func (d *g2PreparationFailure) pendingRequest(g *g2Env) *g2PendingRequest {
	if !g.pendingDiagnostics {
		return nil
	}
	if d.pendingDiagnostics == nil {
		d.pendingDiagnostics = &g2PendingDiagnostics{Schema: "g2-pending-request-set-v1",
			Test: g.t.Name(), Backend: g.be, Round: d.Round, OISPID: g2PreparationPID(g.ois),
			Requests: make([]g2PendingRequest, 0, 512), entropy: rand.Reader}
	}
	s := d.pendingDiagnostics
	r := g2PendingRequest{Attempt: d.Attempts, Phase: d.Phase, entropy: s.entropy}
	if len(s.Requests) == cap(s.Requests) {
		s.Overflow, s.Omitted = true, s.Omitted+1
		return &r
	}
	s.Requests = append(s.Requests, r)
	return &s.Requests[len(s.Requests)-1]
}

func (r *g2PendingRequest) prepare(req *http.Request) {
	var raw [16]byte
	if _, err := io.ReadFull(r.entropy, raw[:]); err != nil {
		r.IDError = g2DiagnosticPtr("entropy_error")
		return
	}
	r.SampleID = g2DiagnosticPtr(hex.EncodeToString(raw[:]))
	req.Header.Set(g2PendingHeader, *r.SampleID)
}

func g2PendingCopyStats(s *protocol.OISStats) *protocol.OISStats {
	if s == nil {
		return nil
	}
	out := *s
	if s.Observation != nil {
		o := *s.Observation
		out.Observation = &o
	}
	return &out
}

func g2PendingCopyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	return g2DiagnosticPtr(*p)
}

func (d *g2PreparationFailure) pendingFields(fields map[string]any) map[string]any {
	if d.pendingDiagnostics == nil {
		return fields
	}
	out := *d.pendingDiagnostics
	out.entropy = nil
	out.Requests = make([]g2PendingRequest, len(d.pendingDiagnostics.Requests))
	for i, r := range d.pendingDiagnostics.Requests {
		r.entropy = nil
		r.SampleID, r.IDError = g2PendingCopyPtr(r.SampleID), g2PendingCopyPtr(r.IDError)
		r.OISStats = g2PendingCopyStats(r.OISStats)
		if r.OISCall != nil {
			call := *r.OISCall
			call.ReturnedNS = g2PendingCopyPtr(call.ReturnedNS)
			call.ContextDeadlineNS = g2PendingCopyPtr(call.ContextDeadlineNS)
			call.ContextState, call.ErrorClass = g2PendingCopyPtr(call.ContextState), g2PendingCopyPtr(call.ErrorClass)
			r.OISCall = &call
		}
		out.Requests[i] = r
	}
	fields["pending_identity_diagnostics"] = out
	return fields
}
