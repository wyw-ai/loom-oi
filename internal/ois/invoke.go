package ois

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type outcome struct {
	result       json.RawMessage
	resultFormat string
	err          *protocol.Error
}

type pendingCall struct {
	conn   *connection
	format string // captured from the fixed Action at dispatch, independent of later registrations
	done   chan outcome
	once   sync.Once
}

func (p *pendingCall) complete(value outcome) { p.once.Do(func() { p.done <- value }) }

func (p *pendingCall) result(frame protocol.Frame) outcome {
	if err := protocol.ValidateResult(p.format, frame); err != nil {
		if problem, ok := err.(*protocol.Error); ok {
			return outcome{err: problem}
		}
		return outcome{err: protocol.Err("EXECUTION_ERROR", "OIC returned an invalid result")}
	}
	return outcome{result: frame.Result, resultFormat: p.format}
}

func (s *Server) invoke(ctx context.Context, serviceID, name string, arguments map[string]any, bearer string, secure bool, mcpIdentity string, expected *Source) (string, outcome) {
	s.mu.Lock()
	svc, identity, authErr := s.accessedServiceLocked(serviceID, bearer, secure, mcpIdentity != "")
	if authErr == nil && mcpIdentity != "" && identity != mcpIdentity {
		authErr = protocol.Err("FORBIDDEN", "service access changed during this request")
	}
	if authErr != nil {
		s.mu.Unlock()
		return "", outcome{err: authErr}
	}
	var source Source
	for _, member := range svc.Actions {
		if member.Name == name {
			source = member.Source
			break
		}
	}
	if source.Channel == "" {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("NOT_FOUND", "action is not present in this service")}
	}
	if expected != nil && source != *expected {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("BINDING_CHANGED", "fixed source changed since discovery")}
	}
	if s.closed {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("CHANNEL_UNAVAILABLE", "server is closing")}
	}
	c := s.live[source.Channel]
	if c == nil {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("CHANNEL_UNAVAILABLE", "fixed channel is offline; call was not delivered")}
	}
	action, available := s.readyActionLocked(source)
	if !available {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("APP_UNAVAILABLE", "fixed Action is not ready; call was not delivered")}
	}
	if ctx.Err() != nil {
		s.mu.Unlock()
		return "", outcome{err: protocol.Err("CANCELLED", "call cancelled before delivery")}
	}
	id := protocol.ID("call_")
	p := &pendingCall{conn: c, format: action.ResultFormat, done: make(chan outcome, 1)}
	s.pending[id] = p
	frame := protocol.Frame{Type: "invoke", CallID: id, App: source.App, Action: source.Action, Input: arguments, Context: protocol.Context{AccessIdentity: identity, VMCPID: serviceID}}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	// Business input is intentionally not validated against the consumer schema.
	// OIC binds it using the local Action declaration, including light coercion.
	wire, err := protocol.MarshalFrame(frame, false)
	if err != nil {
		return id, outcome{err: protocol.Err("INVALID_ARGUMENT", "invoke exceeds the channel frame limit; call was not delivered")}
	}
	if err := c.sendMessage(wire); err != nil {
		c.close()
		return id, outcome{err: protocol.Err("CALL_RESULT_UNKNOWN", "connection failed while delivering the call; execution is unknown")}
	}
	select {
	case result := <-p.done:
		return id, result
	case <-ctx.Done():
		select {
		case result := <-p.done:
			return id, result
		default:
		}
		if err := c.send(protocol.Frame{Type: "cancel", CallID: id}); err != nil {
			c.close()
			return id, outcome{err: protocol.Err("CALL_RESULT_UNKNOWN", "cancellation could not be delivered")}
		}
		// Let the execution owner settle cancellation against result publication.
		// Never equate sending a cancel frame with successful process termination.
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case result := <-p.done:
			return id, result
		case <-timer.C:
			return id, outcome{err: protocol.Err("CALL_RESULT_UNKNOWN", "cancellation outcome was not confirmed")}
		}
	}
}

func (s *Server) restExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Service   string         `json:"service"`
		Action    string         `json:"action"`
		VMCP      string         `json:"vmcp"`
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "invalid execution request"))
		return
	}
	if req.Service == "" && req.Action == "" {
		req.Service, req.Action = req.VMCP, req.Tool
	} else if req.VMCP != "" || req.Tool != "" {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "use one service/action or vmcp/tool pair"))
		return
	}
	if req.Service == "" || req.Action == "" || req.Arguments == nil {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "service, action and an arguments object are required"))
		return
	}
	// A broken HTTP socket does not reliably express user cancellation. Calls
	// have their own server deadline and are never automatically replayed.
	ctx, cancel := context.WithTimeout(context.Background(), duration(s.cfg.Server.RequestTimeoutMS))
	defer cancel()
	id, result := s.invoke(ctx, req.Service, req.Action, req.Arguments, token(r), r.TLS != nil, "", nil)
	if result.err != nil {
		writeError(w, errorStatus(result.err), id, result.err)
		return
	}
	response := struct {
		CallID       string          `json:"call_id"`
		ResultFormat string          `json:"result_format,omitempty"`
		Result       json.RawMessage `json:"result"`
	}{CallID: id, Result: result.result}
	if result.resultFormat == protocol.FormatContent {
		response.ResultFormat = result.resultFormat
	}
	if !writeBoundedJSON(w, http.StatusOK, response) {
		writeError(w, 422, id, protocol.Err("RESULT_TOO_LARGE", "result exceeds the HTTP response budget"))
	}
}

func (s *Server) Status() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	apps, ready, available := 0, 0, 0
	for ch, snap := range s.snapshots {
		for _, a := range snap.Apps {
			apps++
			if a.Ready && s.live[ch] != nil && s.live[ch].registered {
				ready++
			}
		}
	}
	for _, t := range s.state.Tools {
		if s.availableLocked(t.Source) {
			available++
		}
	}
	return map[string]any{
		"running": !s.closed, "listen": s.cfg.Server.Listen,
		"channels": map[string]int{"configured": len(s.state.Channels), "online": len(s.live)},
		"apps":     map[string]int{"registered": apps, "available": ready},
		"tools":    map[string]int{"configured": len(s.state.Tools), "available": available},
		"vmcps":    len(s.state.VMCPS),
		"services": len(s.state.Services),
	}
}
