package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type apiFn func(w http.ResponseWriter, r *http.Request, p relay.Principal) error

type items[T any] struct {
	Items []T `json:"items"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError renders a coded error as {"code","message"} with the B1 §7
// status; uncoded errors are internal and their text is not exposed.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		s.log.Error("internal error", "error", err.Error())
		pe = protocol.Errorf(protocol.CodeInternal, "internal error")
	}
	writeJSON(w, pe.Code.HTTPStatus(), protocol.ErrorBody{Code: pe.Code, Message: pe.Message, Reason: pe.Reason, ConfigVersion: pe.ConfigVersion})
}

func (s *Server) notFound(w http.ResponseWriter, _ *http.Request) {
	s.writeError(w, protocol.Errorf(protocol.CodeNotFound, "no such endpoint"))
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, protocol.ErrorBody{Code: protocol.CodeInvalidRequest, Message: "method not allowed"})
}

// authenticate checks the bearer token and that its scope matches the API
// (B2 §2: admin tokens cannot Open, consumer tokens cannot administer).
func (s *Server) authenticate(r *http.Request, scope string) (relay.Principal, error) {
	bearer, ok := identity.BearerToken(r)
	if !ok {
		return relay.Principal{}, protocol.Errorf(protocol.CodeUnauthenticated, "missing bearer credential")
	}
	p, err := s.hub.Authenticate(bearer)
	if err != nil {
		return relay.Principal{}, err
	}
	if p.Scope != scope {
		return relay.Principal{}, protocol.Errorf(protocol.CodeDenied, "a %s token cannot use this API", p.Scope)
	}
	return p, nil
}

func (s *Server) route(scope string, methods map[string]apiFn) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn, ok := methods[r.Method]
		if !ok {
			methodNotAllowed(w)
			return
		}
		p, err := s.authenticate(r, scope)
		if err == nil {
			err = fn(w, r, p)
		}
		if err != nil {
			s.writeError(w, err)
		}
	})
}

func (s *Server) apiHandler() http.Handler {
	c, a := protocol.ScopeConsumer, protocol.ScopeAdmin
	mux := http.NewServeMux()
	mux.Handle("/v1/exposures", s.route(c, map[string]apiFn{http.MethodGet: s.listExposures}))
	mux.Handle("/v1/channels", s.route(c, map[string]apiFn{http.MethodPost: s.openChannel}))
	mux.Handle("/v1/channels/{id}", s.route(c, map[string]apiFn{http.MethodGet: s.getChannel, http.MethodDelete: s.deleteChannel}))
	mux.HandleFunc("/v1/channels/{id}/attach", s.attachConsumer)
	mux.Handle("/v1/admin/config", s.route(a, map[string]apiFn{http.MethodGet: s.adminConfig}))
	mux.Handle("/v1/admin/stats", s.route(a, map[string]apiFn{http.MethodGet: s.adminStats}))
	mux.Handle("/v1/admin/audit", s.route(a, map[string]apiFn{http.MethodGet: s.adminAudit}))
	mux.Handle("/v1/admin/audit/prune", s.route(a, map[string]apiFn{http.MethodPost: s.pruneAudit}))
	mux.Handle("/v1/admin/maintenance", s.route(a, map[string]apiFn{http.MethodPut: s.setMaintenance}))
	mux.Handle("/v1/admin/store", s.route(a, map[string]apiFn{http.MethodGet: s.storeView}))
	mux.Handle("/v1/admin/store/maintain", s.route(a, map[string]apiFn{http.MethodPost: s.maintainStore}))
	mux.Handle("/v1/admin/accesses", s.route(a, map[string]apiFn{http.MethodGet: s.listAccesses, http.MethodPost: s.createAccess}))
	mux.Handle("/v1/admin/accesses/{id}", s.route(a, map[string]apiFn{
		http.MethodGet: s.getAccess, http.MethodPatch: s.patchAccess, http.MethodDelete: s.deleteAccess}))
	mux.Handle("/v1/admin/tokens", s.route(a, map[string]apiFn{http.MethodGet: s.listTokens, http.MethodPost: s.createToken}))
	mux.Handle("/v1/admin/tokens/{id}", s.route(a, map[string]apiFn{http.MethodDelete: s.revokeToken}))
	mux.Handle("/v1/admin/policies", s.route(a, map[string]apiFn{http.MethodGet: s.listPolicies, http.MethodPost: s.createPolicy}))
	mux.Handle("/v1/admin/policies/{id}", s.route(a, map[string]apiFn{
		http.MethodGet: s.getPolicy, http.MethodPatch: s.patchPolicy, http.MethodDelete: s.deletePolicy}))
	mux.HandleFunc("/", s.notFound)
	return mux
}

func queryInt(r *http.Request, name string) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, protocol.Errorf(protocol.CodeInvalidRequest, "%s must be an integer", name)
	}
	return n, nil
}

func (s *Server) decode(r *http.Request, v any) error {
	return protocol.DecodeStrict(r.Body, s.cfg.Limits.HTTPMax, v)
}

func (s *Server) listExposures(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	list, err := s.hub.Discover(p, int(limit), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) openChannel(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var req protocol.OpenRequest
	if err := s.decode(r, &req); err != nil {
		return err
	}
	res, err := s.hub.OpenContext(r.Context(), p, req)
	if err != nil {
		return err
	}
	if a := res.Response.Attach; a != nil {
		a.URL = "wss://" + r.Host + "/v1/channels/" + res.Response.ChannelID + "/attach"
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, res.Response)
	return nil
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, err := s.hub.Channel(p, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, changed, err := s.hub.CloseChannel(p, r.PathValue("id"))
	if err != nil {
		return err
	}
	status := http.StatusOK
	if changed {
		status = http.StatusAccepted
	}
	writeJSON(w, status, v)
	return nil
}

// ifMatch parses `If-Match: "<config_version>"` (B3 §3).
func ifMatch(r *http.Request) (int64, error) {
	h := r.Header.Get("If-Match")
	if h == "" {
		return 0, protocol.Errorf(protocol.CodePreconditionRequired, "If-Match with the current config_version is required")
	}
	v, err := strconv.ParseInt(strings.Trim(h, `"`), 10, 64)
	if err != nil || v < 0 || len(h) < 3 || h[0] != '"' || h[len(h)-1] != '"' {
		return 0, protocol.Errorf(protocol.CodeInvalidRequest, `If-Match must be a quoted config_version such as "3"`)
	}
	return v, nil
}

func (s *Server) adminView(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(s.hub.ConfigView().ConfigVersion, 10)))
	writeJSON(w, status, v)
	return nil
}

func (s *Server) written(w http.ResponseWriter, status int, v int64, err error) error {
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v, 10)))
	writeJSON(w, status, protocol.WriteResult{ConfigVersion: v})
	return nil
}

func (s *Server) adminConfig(w http.ResponseWriter, _ *http.Request, _ relay.Principal) error {
	return s.adminView(w, http.StatusOK, s.hub.ConfigView())
}

func (s *Server) adminStats(w http.ResponseWriter, _ *http.Request, _ relay.Principal) error {
	writeJSON(w, http.StatusOK, s.hub.Stats())
	return nil
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request, _ relay.Principal) error {
	after, err := queryInt(r, "after")
	if err != nil {
		return err
	}
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	list, err := s.hub.Audit(r.Context(), after, int(limit))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) pruneAudit(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var req protocol.AuditPrune
	if err := s.decode(r, &req); err != nil {
		return err
	}
	out, err := s.hub.PruneAudit(r.Context(), p, req)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// setMaintenance enters or leaves maintenance mode (B3 §8).
func (s *Server) setMaintenance(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, err := ifMatch(r)
	if err != nil {
		return err
	}
	var spec protocol.MaintenanceSpec
	if err := s.decode(r, &spec); err != nil {
		return err
	}
	if spec.Maintenance == nil {
		return protocol.Errorf(protocol.CodeInvalidRequest, "maintenance is required")
	}
	n, err := s.hub.SetMaintenance(r.Context(), p, v, *spec.Maintenance)
	return s.written(w, http.StatusOK, n, err)
}

func (s *Server) storeView(w http.ResponseWriter, r *http.Request, _ relay.Principal) error {
	writeJSON(w, http.StatusOK, s.hub.StoreView(r.Context()))
	return nil
}

func (s *Server) maintainStore(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var req protocol.StoreMaintain
	if err := s.decode(r, &req); err != nil {
		return err
	}
	out, err := s.hub.MaintainStore(r.Context(), p, req)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) listAccesses(w http.ResponseWriter, _ *http.Request, _ relay.Principal) error {
	return s.adminView(w, http.StatusOK, items[protocol.AccessView]{s.hub.ListAccesses()})
}

func (s *Server) getAccess(w http.ResponseWriter, r *http.Request, _ relay.Principal) error {
	v, err := s.hub.GetAccess(r.PathValue("id"))
	if err != nil {
		return err
	}
	return s.adminView(w, http.StatusOK, v)
}

func (s *Server) createAccess(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var spec protocol.AccessSpec
	v, err := ifMatch(r)
	if err == nil {
		err = s.decode(r, &spec)
	}
	if err != nil {
		return err
	}
	v, err = s.hub.CreateAccess(r.Context(), p, v, spec)
	return s.written(w, http.StatusCreated, v, err)
}

func (s *Server) patchAccess(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var spec protocol.AccessSpec
	v, err := ifMatch(r)
	if err == nil {
		err = s.decode(r, &spec)
	}
	if err != nil {
		return err
	}
	v, err = s.hub.PatchAccess(r.Context(), p, v, r.PathValue("id"), spec)
	return s.written(w, http.StatusOK, v, err)
}

func (s *Server) deleteAccess(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, err := ifMatch(r)
	if err != nil {
		return err
	}
	v, err = s.hub.DeleteAccess(r.Context(), p, v, r.PathValue("id"))
	return s.written(w, http.StatusOK, v, err)
}

func (s *Server) listTokens(w http.ResponseWriter, _ *http.Request, _ relay.Principal) error {
	return s.adminView(w, http.StatusOK, items[protocol.TokenView]{s.hub.ListTokens()})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var req protocol.TokenCreate
	v, err := ifMatch(r)
	if err == nil {
		err = s.decode(r, &req)
	}
	if err != nil {
		return err
	}
	created, err := s.hub.CreateToken(r.Context(), p, v, req)
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(created.ConfigVersion, 10)))
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, err := ifMatch(r)
	if err != nil {
		return err
	}
	v, err = s.hub.RevokeToken(r.Context(), p, v, r.PathValue("id"))
	return s.written(w, http.StatusOK, v, err)
}

func (s *Server) listPolicies(w http.ResponseWriter, _ *http.Request, _ relay.Principal) error {
	return s.adminView(w, http.StatusOK, items[protocol.PolicyView]{s.hub.ListPolicies()})
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request, _ relay.Principal) error {
	v, err := s.hub.GetPolicy(r.PathValue("id"))
	if err != nil {
		return err
	}
	return s.adminView(w, http.StatusOK, v)
}

func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var spec protocol.PolicySpec
	v, err := ifMatch(r)
	if err == nil {
		err = s.decode(r, &spec)
	}
	if err != nil {
		return err
	}
	v, err = s.hub.CreatePolicy(r.Context(), p, v, spec)
	return s.written(w, http.StatusCreated, v, err)
}

func (s *Server) patchPolicy(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	var spec protocol.PolicySpec
	v, err := ifMatch(r)
	if err == nil {
		err = s.decode(r, &spec)
	}
	if err != nil {
		return err
	}
	v, err = s.hub.PatchPolicy(r.Context(), p, v, r.PathValue("id"), spec)
	return s.written(w, http.StatusOK, v, err)
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request, p relay.Principal) error {
	v, err := ifMatch(r)
	if err != nil {
		return err
	}
	v, err = s.hub.DeletePolicy(r.Context(), p, v, r.PathValue("id"))
	return s.written(w, http.StatusOK, v, err)
}
