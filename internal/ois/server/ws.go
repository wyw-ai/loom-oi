package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// wsConn adapts a coder/websocket connection to relay.Conn.
type wsConn struct{ c *websocket.Conn }

func (a wsConn) Read(ctx context.Context) (bool, []byte, error) {
	typ, data, err := a.c.Read(ctx)
	if errors.Is(err, websocket.ErrMessageTooBig) {
		return false, nil, relay.ErrTooBig
	}
	return typ == websocket.MessageText, data, err
}

func (a wsConn) Write(ctx context.Context, text bool, data []byte) error {
	typ := websocket.MessageBinary
	if text {
		typ = websocket.MessageText
	}
	return a.c.Write(ctx, typ, data)
}

func (a wsConn) Ping(ctx context.Context) error { return a.c.Ping(ctx) }
func (a wsConn) Close(reason string) error      { return a.c.Close(websocket.StatusNormalClosure, reason) }
func (a wsConn) CloseNow() error                { return a.c.CloseNow() }
func acceptOptions(proto string) *websocket.AcceptOptions {
	// permessage-deflate stays disabled on every WebSocket (B1 §2 rule 4).
	return &websocket.AcceptOptions{Subprotocols: []string{proto}, CompressionMode: websocket.CompressionDisabled}
}

// checkUpgrade requires a WebSocket upgrade offering proto; without a
// common subprotocol the upgrade is refused with 400 VersionUnsupported
// and no state is created (B1 §2 rule 3).
func checkUpgrade(r *http.Request, proto string) error {
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return protocol.Errorf(protocol.CodeInvalidRequest, "a WebSocket upgrade is required")
	}
	for _, v := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(v, ",") {
			if strings.TrimSpace(p) == proto {
				return nil
			}
		}
	}
	return protocol.Errorf(protocol.CodeVersionUnsupported, "subprotocol %s is required", proto)
}

// tunnelReadLimit lets a DATA frame of F+1 bytes through so it is answered
// with ABORT(PayloadTooLarge); larger messages hit the WebSocket limit,
// which the relay reports with the same code.
func (s *Server) tunnelReadLimit() int64 {
	return int64(max(s.cfg.Limits.DataMax+2, 3+protocol.MaxAbortDetail))
}

func (s *Server) attachConsumer(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		s.writeError(w, protocol.Errorf(protocol.CodeInvalidRequest, "tickets and parameters must not appear in the URL"))
		return
	}
	p, err := s.authenticate(r, protocol.ScopeConsumer)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.attach(w, r, relay.AttachRequest{Role: relay.Consumer, Principal: p})
}

func (s *Server) attachProvider(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		s.writeError(w, protocol.Errorf(protocol.CodeInvalidRequest, "tickets and parameters must not appear in the URL"))
		return
	}
	accessID, _, err := s.peerAccess(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.attach(w, r, relay.AttachRequest{Role: relay.Provider, AccessID: accessID})
}

func (s *Server) attach(w http.ResponseWriter, r *http.Request, req relay.AttachRequest) {
	if err := checkUpgrade(r, protocol.TunnelSubprotocol); err != nil {
		s.writeError(w, err)
		return
	}
	req.ChannelID = r.PathValue("id")
	req.Ticket = r.Header.Get(protocol.HeaderAttachTicket)
	res, err := s.hub.ReserveAttach(req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	c, err := websocket.Accept(w, r, acceptOptions(protocol.TunnelSubprotocol))
	if err != nil {
		s.hub.CancelAttach(res)
		return
	}
	c.SetReadLimit(s.tunnelReadLimit())
	s.hub.Attach(res, wsConn{c})
}

// peerAccess maps the verified client certificate to its enabled Access.
// The TLS handshake already refused unpinned certificates; this re-check
// covers Accesses disabled since the handshake.
func (s *Server) peerAccess(r *http.Request) (accessID, pin string, err error) {
	pin = identity.SPKIPin(r.TLS.PeerCertificates[0])
	accessID, ok := s.hub.AccessForPin(pin)
	if !ok {
		return "", "", protocol.Errorf(protocol.CodeUnauthenticated, "client certificate is not pinned to an enabled access")
	}
	return accessID, pin, nil
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	accessID, pin, err := s.peerAccess(r)
	if err == nil {
		err = checkUpgrade(r, protocol.ControlSubprotocol)
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	c, err := websocket.Accept(w, r, acceptOptions(protocol.ControlSubprotocol))
	if err != nil {
		return
	}
	c.SetReadLimit(s.cfg.Limits.CtrlMax)
	s.hub.ServeControl(wsConn{c}, accessID, pin)
}

func (s *Server) accessHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/access/control", s.control)
	mux.HandleFunc("/v1/channels/{id}/attach", s.attachProvider)
	mux.HandleFunc("/", s.notFound)
	return mux
}
