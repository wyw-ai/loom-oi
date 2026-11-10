package ois

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// An MCP session identifies a protocol conversation, not an operating-system
// shell. Execution sessions belong to the selected application.
type mcpSession struct {
	vmcp, identity string
	mu             sync.Mutex
	initialized    bool
	closed         bool
	requests       map[string]context.CancelFunc
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func rpcResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func requestKey(id json.RawMessage) (string, bool) {
	if len(id) == 0 {
		return "", false
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return "s:" + v, true
	case json.Number:
		if _, err := v.Int64(); err != nil {
			return "", false
		}
		return "n:" + v.String(), true
	default:
		return "", false
	}
}

func acceptsMCP(header string) bool {
	jsonOK, streamOK := false, false
	for _, part := range strings.Split(header, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil || params["q"] == "0" {
			continue
		}
		if media == "application/json" {
			jsonOK = true
		}
		if media == "text/event-stream" {
			streamOK = true
		}
	}
	return jsonOK && streamOK
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	vmcpID := strings.TrimPrefix(r.URL.Path, "/mcp/")
	if !validID(vmcpID) {
		http.NotFound(w, r)
		return
	}
	identity, authErr := s.authorizedVMCP(r, vmcpID)
	if authErr != nil {
		writeError(w, errorStatus(authErr), "", authErr)
		return
	}
	if r.Method == http.MethodGet {
		// This implementation returns JSON on POST and does not open unsolicited
		// server-to-client event streams, as allowed by Streamable HTTP.
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(405)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(405)
		return
	}
	version := r.Header.Get("MCP-Protocol-Version")
	if version != "" && version != protocol.MCPVersion {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "unsupported MCP protocol version"))
		return
	}
	if r.Method == http.MethodDelete {
		sess, ok := s.mcpLookup(w, r, vmcpID, identity)
		if !ok {
			return
		}
		s.mu.Lock()
		delete(s.sessions, r.Header.Get("MCP-Session-Id"))
		s.mu.Unlock()
		sess.mu.Lock()
		sess.closed = true
		for _, cancel := range sess.requests {
			cancel()
		}
		sess.mu.Unlock()
		w.WriteHeader(204)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		writeError(w, 415, "", protocol.Err("INVALID_REQUEST", "Content-Type must be application/json"))
		return
	}
	if !acceptsMCP(r.Header.Get("Accept")) {
		writeError(w, 406, "", protocol.Err("INVALID_REQUEST", "Accept must include application/json and text/event-stream"))
		return
	}
	var req rpcRequest
	if err := readJSON(w, r, &req); err != nil {
		rpcError(w, nil, -32700, "invalid JSON-RPC request")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		rpcError(w, req.ID, -32600, "invalid JSON-RPC request")
		return
	}
	key, hasID := requestKey(req.ID)
	if len(req.ID) != 0 && !hasID {
		rpcError(w, nil, -32600, "request ID must be a string or integer")
		return
	}
	if req.Method == "initialize" {
		if !hasID {
			w.WriteHeader(202)
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" {
			rpcError(w, req.ID, -32602, "protocolVersion is required")
			return
		}
		id := protocol.ID("mcp_")
		s.mu.Lock()
		if len(s.sessions) >= 4096 {
			s.mu.Unlock()
			writeError(w, 503, "", protocol.Err("CAPACITY_EXCEEDED", "too many MCP sessions"))
			return
		}
		s.sessions[id] = &mcpSession{vmcp: vmcpID, identity: identity, requests: make(map[string]context.CancelFunc)}
		s.mu.Unlock()
		w.Header().Set("MCP-Session-Id", id)
		rpcResult(w, req.ID, map[string]any{"protocolVersion": protocol.MCPVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "loom-oi", "version": "beta"}})
		return
	}
	if version == "" {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "MCP-Protocol-Version is required after initialization"))
		return
	}
	sess, ok := s.mcpLookup(w, r, vmcpID, identity)
	if !ok {
		return
	}
	if !hasID {
		switch req.Method {
		case "notifications/initialized":
			sess.mu.Lock()
			sess.initialized = true
			sess.mu.Unlock()
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(req.Params, &p) == nil {
				if k, ok := requestKey(p.RequestID); ok {
					sess.mu.Lock()
					cancel := sess.requests[k]
					sess.mu.Unlock()
					if cancel != nil {
						cancel()
					}
				}
			}
		}
		w.WriteHeader(202)
		return
	}
	if req.Method == "ping" {
		rpcResult(w, req.ID, map[string]any{})
		return
	}
	sess.mu.Lock()
	initialized := sess.initialized
	sess.mu.Unlock()
	if !initialized {
		rpcError(w, req.ID, -32600, "send notifications/initialized before using tools")
		return
	}
	switch req.Method {
	case "tools/list":
		s.mu.RLock()
		vm, exists := s.state.VMCPS[vmcpID]
		listed := make([]map[string]any, 0, len(vm.Tools))
		if exists {
			for _, name := range vm.Tools {
				t := s.state.Tools[name]
				listed = append(listed, map[string]any{"name": name, "description": t.Description, "inputSchema": t.InputSchema})
			}
		}
		s.mu.RUnlock()
		if !exists {
			rpcError(w, req.ID, -32602, "vMCP no longer exists")
			return
		}
		rpcResult(w, req.ID, map[string]any{"tools": listed})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Meta      map[string]any `json:"_meta"`
		}
		d := json.NewDecoder(bytes.NewReader(req.Params))
		d.UseNumber()
		if d.Decode(&p) != nil || p.Name == "" {
			rpcError(w, req.ID, -32602, "name and an arguments object are required")
			return
		}
		if p.Arguments == nil {
			p.Arguments = make(map[string]any)
		}
		// Deliberately independent of r.Context: losing the HTTP stream is not
		// an MCP cancellation notification and must not kill an accepted command.
		ctx, cancel := context.WithTimeout(context.Background(), duration(s.cfg.Server.RequestTimeoutMS))
		sess.mu.Lock()
		if sess.closed {
			sess.mu.Unlock()
			cancel()
			writeError(w, 404, "", protocol.Err("NOT_FOUND", "MCP session is closed"))
			return
		}
		if _, exists := sess.requests[key]; exists {
			sess.mu.Unlock()
			cancel()
			rpcError(w, req.ID, -32600, "request ID is already active")
			return
		}
		sess.requests[key] = cancel
		sess.mu.Unlock()
		defer func() { cancel(); sess.mu.Lock(); delete(sess.requests, key); sess.mu.Unlock() }()
		id, result := s.invoke(ctx, vmcpID, p.Name, p.Arguments, token(r), r.TLS != nil)
		if ctx.Err() == context.Canceled {
			w.WriteHeader(204)
			return
		}
		if result.err != nil && id == "" && result.err.Code == "HTTPS_REQUIRED" {
			writeError(w, 403, "", result.err)
			return
		}
		if result.err != nil && id == "" && (result.err.Code == "NOT_FOUND" || result.err.Code == "UNAUTHENTICATED") {
			rpcError(w, req.ID, -32602, result.err.Message)
			return
		}
		body := map[string]any{"isError": result.err != nil}
		if id != "" {
			body["_meta"] = map[string]any{"call_id": id}
		}
		var payload any
		if result.err != nil {
			payload = map[string]any{"error": result.err}
		} else {
			decoder := json.NewDecoder(bytes.NewReader(result.result))
			decoder.UseNumber()
			_ = decoder.Decode(&payload)
		}
		encoded, _ := json.Marshal(payload)
		body["content"] = []map[string]any{{"type": "text", "text": string(encoded)}}
		if obj, ok := payload.(map[string]any); ok {
			body["structuredContent"] = obj
		}
		rpcResult(w, req.ID, body)
	default:
		rpcError(w, req.ID, -32601, fmt.Sprintf("unknown method %s", req.Method))
	}
}

func (s *Server) mcpLookup(w http.ResponseWriter, r *http.Request, vmcpID, identity string) (*mcpSession, bool) {
	id := r.Header.Get("MCP-Session-Id")
	if id == "" {
		writeError(w, 400, "", protocol.Err("INVALID_REQUEST", "MCP-Session-Id is required"))
		return nil, false
	}
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if sess == nil {
		writeError(w, 404, "", protocol.Err("NOT_FOUND", "MCP session does not exist"))
		return nil, false
	}
	if sess.vmcp != vmcpID || sess.identity != identity {
		writeError(w, 403, "", protocol.Err("FORBIDDEN", "MCP session belongs to another consumer"))
		return nil, false
	}
	return sess, true
}
