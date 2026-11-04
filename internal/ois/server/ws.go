package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// wsConn adapts a coder/websocket connection to relay.Conn. coder/websocket
// waits up to 5 s for the peer's close frame, and a concurrent CloseNow
// neither preempts that wait nor returns before it ends, so a peer that
// never answers would keep its connection past T_close (B1 §5, TV-B4-03).
// The adapter therefore holds the TCP connection under the TLS session:
// Close gives the closing handshake at most T_close and CloseNow drops the
// transport first.
type wsConn struct {
	c     *websocket.Conn
	tcp   net.Conn
	grace time.Duration
	// wsConn is passed by value. Keep the mutable buffer behind one pointer
	// so all copies used by the sequential tunnel reader retain its storage.
	reader *wsio.Reader
}

func (a wsConn) Read(ctx context.Context) (bool, []byte, error) {
	var typ websocket.MessageType
	var data []byte
	var err error
	if a.reader != nil {
		typ, data, err = a.reader.Read(ctx, a.c)
	} else {
		typ, data, err = a.c.Read(ctx)
	}
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

func (a wsConn) Close(reason string) error {
	t := time.AfterFunc(a.grace, func() { _ = a.tcp.Close() })
	defer t.Stop()
	return a.c.Close(websocket.StatusNormalClosure, reason)
}

func (a wsConn) CloseNow() error {
	_ = a.tcp.Close()
	return a.c.CloseNow()
}

// hijackRecorder keeps the TCP connection that websocket.Accept hijacks.
type hijackRecorder struct {
	http.ResponseWriter
	tcp net.Conn
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := http.NewResponseController(h.ResponseWriter).Hijack()
	h.tcp = c
	if tc, ok := c.(*tls.Conn); ok {
		// Closing below TLS cannot block on a close_notify write.
		h.tcp = tc.NetConn()
	}
	return c, rw, err
}

// accept upgrades to a WebSocket with subprotocol proto.
func (s *Server) accept(w http.ResponseWriter, r *http.Request, proto string, readLimit int64) (wsConn, error) {
	rec := &hijackRecorder{ResponseWriter: w}
	// permessage-deflate stays disabled on every WebSocket (B1 §2 rule 4).
	c, err := websocket.Accept(rec, r, &websocket.AcceptOptions{Subprotocols: []string{proto}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return wsConn{}, err
	}
	c.SetReadLimit(readLimit)
	conn := wsConn{c: c, tcp: rec.tcp, grace: limits.D(s.cfg.Limits.CloseMS)}
	if proto == protocol.TunnelSubprotocol {
		conn.reader = wsio.NewReader(s.cfg.Limits.DataMax+2, int(readLimit))
	}
	return conn, nil
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
	conn, err := s.accept(w, r, protocol.TunnelSubprotocol, s.tunnelReadLimit())
	if err != nil {
		s.hub.CancelAttach(res)
		return
	}
	s.hub.Attach(res, conn)
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
	var version string
	if err == nil {
		version, err = controlVersion(r)
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	conn, err := s.accept(w, r, version, s.cfg.Limits.CtrlMax)
	if err != nil {
		return
	}
	s.hub.ServeControlVersion(conn, accessID, pin, version)
}

func controlVersion(r *http.Request) (string, error) {
	version := protocol.ControlSubprotocolV2
	err := checkUpgrade(r, version)
	if protocol.CodeOf(err) == protocol.CodeVersionUnsupported {
		version = protocol.ControlSubprotocol
		err = checkUpgrade(r, version)
	}
	return version, err
}

func (s *Server) accessHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/access/control", s.control)
	mux.HandleFunc("/v1/channels/{id}/attach", s.attachProvider)
	mux.HandleFunc("/", s.notFound)
	return mux
}
