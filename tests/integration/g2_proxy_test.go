package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

const (
	g2OICToOIS               = "oic_to_ois"
	g2OISToOIC               = "ois_to_oic"
	g2RoleControl            = "control"
	g2RoleProvider           = "provider"
	g2MaxConnections         = 4096
	g2MaxActiveConnections   = 128
	g2MaxEvents              = 32768
	g2MaxRecordedBytes       = 32 << 20
	g2MaxWireMessage         = 256 << 10
	g2MaxBarriers            = 128
	g2MaxChecksPerConnection = 4096
)

// g2WireEvent 只记录代理实际边界。Raw 为完整且独立的副本；Injected
// 必须单独判读，不能作为 OIC 自发决定。写入尚未开始/失败时对应时刻为零。
type g2WireEvent struct {
	ID, ConnID                                                uint64
	Role, Direction, Type, Stage, ChannelID, CheckID, QueryID string
	MessageType                                               websocket.MessageType
	Raw                                                       []byte
	ReceivedNS, WriteStartedNS, WrittenNS                     int64
	Injected                                                  bool
	WriteFailed                                               bool
	Dropped                                                   bool
}
type g2ConnectionSnapshot struct {
	ID                           uint64
	Role, ChannelID, Subprotocol string
	AcceptedNS, ClosedNS         int64
	Workers                      int
	Closed                       bool
	UpstreamStatus               int
	HandshakeBody                []byte
}
type g2ProxySnapshot struct {
	TransportConnections int
	PID                  int
	Workers              int
	Overflow             bool
	Failure              string
	Connections          []g2ConnectionSnapshot
	Events               []g2WireEvent
}
type g2Match struct {
	ConnID                                                    uint64
	Role, Direction, Type, Stage, ChannelID, CheckID, QueryID string
}

func (m g2Match) matches(e g2WireEvent) bool {
	return !e.Injected && (m.ConnID == 0 || m.ConnID == e.ConnID) && (m.Role == "" || m.Role == e.Role) && (m.Direction == "" || m.Direction == e.Direction) &&
		(m.Type == "" || m.Type == e.Type) && (m.Stage == "" || m.Stage == e.Stage) && (m.ChannelID == "" || m.ChannelID == e.ChannelID) && (m.CheckID == "" || m.CheckID == e.CheckID) && (m.QueryID == "" || m.QueryID == e.QueryID)
}

type g2Barrier struct {
	proxy              *g2Proxy
	match              g2Match
	hit                chan g2WireEvent
	release            chan struct{}
	cancelledCh        chan struct{}
	once               sync.Once
	claimed, cancelled bool // proxy.mu 保护
	dropped            bool // once 决定后不可反转，proxy.mu 保护
	eventID            uint64
}

func (b *g2Barrier) Wait(ctx context.Context) (g2WireEvent, error) {
	select {
	case e := <-b.hit:
		return g2CloneEvent(e), nil
	case <-ctx.Done():
		return g2WireEvent{}, ctx.Err()
	case <-b.cancelledCh:
		return g2WireEvent{}, context.Canceled
	case <-b.proxy.ctx.Done():
		return g2WireEvent{}, errors.New("代理已关闭")
	}
}
func (b *g2Barrier) Release() {
	b.once.Do(func() { close(b.release) })
	b.proxy.mu.Lock()
	if b.claimed {
		b.proxy.removeBarrierLocked(b)
	}
	b.proxy.mu.Unlock()
}

// Drop 显式丢弃该门闩的一条原消息。它不注入替身、不修改序号，也不让
// 后续消息越过尚未决定的门闩。与 Release 竞争时仅首次决定生效。
func (b *g2Barrier) Drop() {
	b.once.Do(func() {
		b.proxy.mu.Lock()
		b.dropped = true
		if b.eventID != 0 {
			b.proxy.events[b.eventID-1].Dropped = true
		}
		b.proxy.notify()
		b.proxy.mu.Unlock()
		close(b.release)
	})
	b.proxy.mu.Lock()
	if b.claimed {
		b.proxy.removeBarrierLocked(b)
	}
	b.proxy.mu.Unlock()
}
func (b *g2Barrier) isDropped() bool {
	b.proxy.mu.Lock()
	defer b.proxy.mu.Unlock()
	return b.dropped
}

// Cancel 取消这道一次性仪器门闩，并释放已命中的原始消息；不会伪造协议取消。
func (b *g2Barrier) Cancel() {
	b.proxy.mu.Lock()
	if !b.cancelled {
		b.cancelled = true
		close(b.cancelledCh)
	}
	b.proxy.removeBarrierLocked(b)
	b.proxy.mu.Unlock()
	b.Release()
}

// Only the active matcher is reclaimed. The barrier object remains valid for
// its waiters, and complete immutable event IDs/bytes remain in proxy history.
func (p *g2Proxy) removeBarrierLocked(b *g2Barrier) {
	for i, current := range p.barriers {
		if current == b {
			copy(p.barriers[i:], p.barriers[i+1:])
			p.barriers[len(p.barriers)-1] = nil
			p.barriers = p.barriers[:len(p.barriers)-1]
			p.notify()
			return
		}
	}
}

type g2Proxy struct {
	capped         *g2CappedListener
	mu             sync.Mutex
	base           string
	ca             *pki.CA
	leaf           *pki.Leaf
	ln             net.Listener
	server         *http.Server
	ctx            context.Context
	cancel         context.CancelFunc
	conns          []*g2ProxyConn
	events         []g2WireEvent
	bytes, workers int
	barriers       []*g2Barrier
	overflow       bool
	failure        string
	closing        bool
	changed        chan struct{}
	handlers       sync.WaitGroup
	done           chan struct{}
	serveDone      chan struct{}
	closeOnce      sync.Once
}
type g2ProxyConn struct {
	proxy          *g2Proxy
	snap           g2ConnectionSnapshot
	ctx            context.Context
	cancel         context.CancelFunc
	down, up       *websocket.Conn
	downTCP, upTCP net.Conn
	lanes          [2]chan struct{}
	stages         map[string]string
	stopOnce       sync.Once
}
type g2Hijack struct {
	http.ResponseWriter
	tcp net.Conn
}

// 在 TLS/HTTP goroutine 创建之前限连接；不是只在升级后统计 worker。
type g2CappedListener struct {
	rejected func()
	net.Listener
	slots chan struct{}
}
type g2CappedConn struct {
	net.Conn
	slots chan struct{}
	once  sync.Once
}

func (c *g2CappedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.slots })
	return err
}
func (l *g2CappedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			return &g2CappedConn{Conn: c, slots: l.slots}, nil
		default:
			l.rejected()
			_ = c.Close()
		}
	}
}

func (h *g2Hijack) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := http.NewResponseController(h.ResponseWriter).Hijack()
	if err == nil {
		h.tcp = c
		if tc, ok := c.(*tls.Conn); ok {
			h.tcp = tc.NetConn()
		}
	}
	return c, rw, err
}

// newG2Proxy 在本次测试 CA 下验证两端 TLS，上行始终使用与下行相同的
// Access 测试 leaf。它不产生 hello/register/check/result，也不改写消息内容。
func newG2Proxy(t testing.TB, upstreamWSSBase string, ca *pki.CA, accessLeaf *pki.Leaf) *g2Proxy {
	t.Helper()
	u, err := url.Parse(upstreamWSSBase)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.RawQuery != "" || u.User != nil || u.Path != "" {
		t.Fatalf("G2 代理上行必须为 wss://host:port: %q", upstreamWSSBase)
	}
	cert := must(must(ca.Server("g2-proxy")).TLS())
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	ctx, cancel := context.WithCancel(context.Background())
	p := &g2Proxy{base: upstreamWSSBase, ca: ca, leaf: accessLeaf, ln: ln, ctx: ctx, cancel: cancel, changed: make(chan struct{}), done: make(chan struct{}), serveDone: make(chan struct{})}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.Pool(), NextProtos: []string{"http/1.1"}, VerifyConnection: func(s tls.ConnectionState) error {
		if len(s.PeerCertificates) == 0 || !bytes.Equal(s.PeerCertificates[0].RawSubjectPublicKeyInfo, accessLeaf.Cert.RawSubjectPublicKeyInfo) {
			return errors.New("G2 代理拒绝其他 Access leaf")
		}
		return nil
	}}
	p.server = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	capped := &g2CappedListener{Listener: ln, slots: make(chan struct{}, g2MaxActiveConnections), rejected: func() { p.mu.Lock(); p.overflow = true; p.failure = "transport capacity"; p.notify(); p.mu.Unlock() }}
	p.capped = capped
	go func() { defer close(p.serveDone); _ = p.server.Serve(tls.NewListener(capped, tlsConfig)) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			t.Errorf("G2 代理未回收: %v", err)
		}
	})
	return p
}
func (p *g2Proxy) URL() string               { return "wss://" + p.ln.Addr().String() }
func (p *g2Proxy) notify()                   { close(p.changed); p.changed = make(chan struct{}) }
func g2CloneEvent(e g2WireEvent) g2WireEvent { e.Raw = append([]byte(nil), e.Raw...); return e }
func (p *g2Proxy) Snapshot() g2ProxySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := g2ProxySnapshot{TransportConnections: len(p.capped.slots), PID: os.Getpid(), Workers: p.workers, Overflow: p.overflow, Failure: p.failure}
	for _, c := range p.conns {
		conn := c.snap
		conn.HandshakeBody = append([]byte(nil), conn.HandshakeBody...)
		s.Connections = append(s.Connections, conn)
	}
	for _, e := range p.events {
		s.Events = append(s.Events, g2CloneEvent(e))
	}
	return s
}
func (p *g2Proxy) Arm(m g2Match) (*g2Barrier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return nil, errors.New("代理已关闭")
	}
	if (m.Direction != "" && m.Direction != g2OICToOIS && m.Direction != g2OISToOIC) || (m.Role != "" && m.Role != g2RoleControl && m.Role != g2RoleProvider) || m.Type == "" {
		return nil, errors.New("门闩需要消息类型及有效方向/角色")
	}
	if len(p.barriers) >= g2MaxBarriers {
		return nil, errors.New("G2 门闩容量耗尽")
	}
	b := &g2Barrier{proxy: p, match: m, hit: make(chan g2WireEvent, 1), release: make(chan struct{}), cancelledCh: make(chan struct{})}
	p.barriers = append(p.barriers, b)
	return b, nil
}

// WaitEvent 等待实际捕获的消息；调用方应保存 after，不能拿旧消息冒充新回应。
func (p *g2Proxy) WaitEvent(ctx context.Context, after uint64, m g2Match) (g2WireEvent, error) {
	for {
		p.mu.Lock()
		for _, e := range p.events {
			if e.ID > after && m.matches(e) {
				p.mu.Unlock()
				return g2CloneEvent(e), nil
			}
		}
		changed, closing := p.changed, p.closing
		p.mu.Unlock()
		if closing {
			return g2WireEvent{}, errors.New("代理已关闭")
		}
		select {
		case <-ctx.Done():
			return g2WireEvent{}, ctx.Err()
		case <-changed:
		}
	}
}

// WaitWritten 以实际 Write 返回为准；接收或命中门闩不等于已发给另一端。
func (p *g2Proxy) WaitWritten(ctx context.Context, eventID uint64) (g2WireEvent, error) {
	for {
		p.mu.Lock()
		if eventID == 0 || eventID > uint64(len(p.events)) {
			p.mu.Unlock()
			return g2WireEvent{}, errors.New("未知事件")
		}
		e := p.events[eventID-1]
		changed, closing := p.changed, p.closing
		p.mu.Unlock()
		if e.WrittenNS > 0 {
			return g2CloneEvent(e), nil
		}
		if e.Dropped || e.WriteFailed || closing {
			return g2CloneEvent(e), errors.New("消息未写出")
		}
		select {
		case <-ctx.Done():
			return g2CloneEvent(e), ctx.Err()
		case <-changed:
		}
	}
}
func g2Route(path string) (role, channel string) {
	if path == "/v1/access/control" {
		return g2RoleControl, ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 5 && parts[1] == "v1" && parts[2] == "channels" && parts[4] == "attach" && protocol.ValidChannelID(parts[3]) {
		return g2RoleProvider, parts[3]
	}
	return "", ""
}
func (p *g2Proxy) serve(w http.ResponseWriter, r *http.Request) {
	role, channel := g2Route(r.URL.Path)
	if role == "" || r.Method != http.MethodGet || r.URL.RawQuery != "" {
		http.Error(w, "unsupported test route", 400)
		return
	}
	p.mu.Lock()
	active := 0
	for _, c := range p.conns {
		if !c.snap.Closed {
			active++
		}
	}
	if p.closing || len(p.conns) >= g2MaxConnections || active >= g2MaxActiveConnections {
		if !p.closing {
			p.overflow = true
			p.failure = "connection capacity"
			p.notify()
		}
		p.mu.Unlock()
		http.Error(w, "proxy capacity", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(p.ctx)
	c := &g2ProxyConn{proxy: p, ctx: ctx, cancel: cancel, snap: g2ConnectionSnapshot{ID: uint64(len(p.conns) + 1), Role: role, ChannelID: channel, AcceptedNS: resourceNS()}, stages: make(map[string]string)}
	c.lanes = [2]chan struct{}{make(chan struct{}, 1), make(chan struct{}, 1)}
	p.conns = append(p.conns, c)
	p.handlers.Add(1)
	p.notify()
	p.mu.Unlock()
	defer func() {
		c.stop()
		p.mu.Lock()
		c.snap.Closed = true
		c.snap.ClosedNS = resourceNS()
		p.notify()
		p.mu.Unlock()
		p.handlers.Done()
	}()
	leaf := must(p.leaf.TLS())
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: p.ca.Pool(), Certificates: []tls.Certificate{leaf}, NextProtos: []string{"http/1.1"}}}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		c.upTCP = raw
		closed := c.ctx.Err() != nil
		p.mu.Unlock()
		if closed {
			raw.Close()
			return nil, context.Canceled
		}
		return raw, nil
	}
	defer transport.CloseIdleConnections()
	var protocols []string
	for _, value := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				protocols = append(protocols, item)
			}
		}
	}
	header := http.Header{}
	for _, key := range []string{protocol.HeaderAttachTicket, "Authorization"} {
		if value := r.Header.Values(key); len(value) > 0 {
			header[key] = append([]string(nil), value...)
		}
	}
	dialCtx, dialCancel := context.WithTimeout(c.ctx, 3*time.Second)
	up, res, err := websocket.Dial(dialCtx, p.base+r.URL.Path, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, HTTPHeader: header, Subprotocols: protocols, CompressionMode: websocket.CompressionDisabled})
	dialCancel()
	if res != nil {
		p.mu.Lock()
		c.snap.UpstreamStatus = res.StatusCode
		p.mu.Unlock()
	}
	if err != nil {
		if res != nil && res.Body != nil {
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 1025))
			_ = res.Body.Close()
			// coder/websocket 的失败响应只保留前 1024 字节。触边不能
			// 证明原文完整，因此明确标记仪器失败，不能静默截断证据。
			if readErr == nil && len(body) < 1024 && res.StatusCode != http.StatusSwitchingProtocols {
				p.mu.Lock()
				c.snap.HandshakeBody = append([]byte(nil), body...)
				p.mu.Unlock()
				w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
				w.WriteHeader(res.StatusCode)
				_, _ = w.Write(body)
				return
			}
			p.mu.Lock()
			p.overflow = true
			p.failure = "incomplete handshake response"
			p.notify()
			p.mu.Unlock()
		}
		http.Error(w, "upstream handshake failed", http.StatusBadGateway)
		return
	}
	p.mu.Lock()
	c.up = up
	closed := c.ctx.Err() != nil
	p.mu.Unlock()
	if closed {
		_ = up.CloseNow()
		return
	}
	up.SetReadLimit(g2MaxWireMessage)
	rec := &g2Hijack{ResponseWriter: w}
	down, err := websocket.Accept(rec, r, &websocket.AcceptOptions{Subprotocols: []string{up.Subprotocol()}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	down.SetReadLimit(g2MaxWireMessage)
	p.mu.Lock()
	c.down = down
	c.downTCP = rec.tcp
	c.snap.Subprotocol = up.Subprotocol()
	closed = p.closing || c.ctx.Err() != nil
	p.mu.Unlock()
	if closed {
		_ = rec.tcp.Close()
		_ = down.CloseNow()
		return
	}
	var wg sync.WaitGroup
	for _, direction := range []string{g2OICToOIS, g2OISToOIC} {
		wg.Add(1)
		p.mu.Lock()
		p.workers++
		c.snap.Workers++
		p.notify()
		p.mu.Unlock()
		go func() {
			defer wg.Done()
			defer func() { p.mu.Lock(); p.workers--; c.snap.Workers--; p.notify(); p.mu.Unlock() }()
			c.pump(direction)
		}()
	}
	wg.Wait()
}
func (c *g2ProxyConn) stop() {
	c.stopOnce.Do(func() {
		c.cancel()
		p := c.proxy
		p.mu.Lock()
		rawDown, rawUp, down, up := c.downTCP, c.upTCP, c.down, c.up
		p.mu.Unlock()
		if rawDown != nil {
			_ = rawDown.Close()
		}
		if rawUp != nil {
			_ = rawUp.Close()
		}
		if down != nil {
			_ = down.CloseNow()
		}
		if up != nil {
			_ = up.CloseNow()
		}
	})
}
func (c *g2ProxyConn) pump(direction string) {
	defer c.stop()
	src := c.down
	if direction == g2OISToOIC {
		src = c.up
	}
	for {
		typ, raw, err := src.Read(c.ctx)
		receivedNS := resourceNS()
		if err != nil {
			return
		}
		e, b, err := c.capture(direction, typ, raw, false, receivedNS)
		if err != nil {
			return
		}
		if b != nil {
			select {
			case <-b.release:
			case <-c.ctx.Done():
				return
			}
			if b.isDropped() {
				continue
			}
		}
		if err = c.write(c.ctx, direction, e.ID, typ, raw); err != nil {
			return
		}
	}
}
func (c *g2ProxyConn) capture(direction string, typ websocket.MessageType, raw []byte, injected bool, receivedNS int64) (g2WireEvent, *g2Barrier, error) {
	e := g2WireEvent{ConnID: c.snap.ID, Role: c.snap.Role, Direction: direction, ChannelID: c.snap.ChannelID, MessageType: typ, Raw: append([]byte(nil), raw...), Injected: injected}
	if !injected {
		e.ReceivedNS = receivedNS
	}
	if typ == websocket.MessageText {
		var m struct {
			Type, Stage string
			ChannelID   string `json:"channel_id"`
			CheckID     string `json:"check_id"`
			QueryID     string `json:"query_id"`
		}
		if json.Unmarshal(raw, &m) == nil {
			e.Type = m.Type
			e.Stage = m.Stage
			e.CheckID = m.CheckID
			e.QueryID = m.QueryID
			if m.ChannelID != "" {
				e.ChannelID = m.ChannelID
			}
		}
	} else if len(raw) > 0 {
		switch raw[0] {
		case protocol.FrameReady:
			e.Type = "READY"
		case protocol.FrameData:
			e.Type = "DATA"
		case protocol.FrameFin:
			e.Type = "FIN"
		case protocol.FrameAbort:
			e.Type = "ABORT"
		default:
			e.Type = "UNKNOWN"
		}
	}
	p := c.proxy
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return e, nil, context.Canceled
	}
	if len(p.events) >= g2MaxEvents || p.bytes+len(raw) > g2MaxRecordedBytes {
		p.overflow = true
		p.failure = "event capacity"
		p.notify()
		return e, nil, errors.New("G2 完整消息记录容量耗尽")
	}
	if e.Role == g2RoleControl && e.Type == protocol.MsgAdmissionCheck && e.Direction == g2OISToOIC && !injected {
		if _, exists := c.stages[e.CheckID]; !exists && len(c.stages) >= g2MaxChecksPerConnection {
			p.overflow = true
			p.failure = "check identity capacity"
			p.notify()
			return e, nil, errors.New("G2 check 身份容量耗尽")
		}
		c.stages[e.CheckID] = e.Stage
	}
	if e.Type == protocol.MsgAdmissionResult {
		e.Stage = c.stages[e.CheckID]
	}
	e.ID = uint64(len(p.events) + 1)
	p.events = append(p.events, e)
	p.bytes += len(raw)
	var hit *g2Barrier
	for _, b := range p.barriers {
		if !b.claimed && !b.cancelled && b.match.matches(e) {
			b.claimed = true
			b.eventID = e.ID
			if b.dropped {
				e.Dropped = true
				p.events[e.ID-1].Dropped = true
			}
			hit = b
			b.hit <- g2CloneEvent(e)
			break
		}
	}
	if hit != nil {
		select {
		case <-hit.release:
			p.removeBarrierLocked(hit)
		default:
		}
	}
	p.notify()
	return e, hit, nil
}
func (c *g2ProxyConn) write(ctx context.Context, direction string, eventID uint64, typ websocket.MessageType, raw []byte) error {
	lane := 0
	dest := c.up
	if direction == g2OISToOIC {
		lane = 1
		dest = c.down
	}
	select {
	case c.lanes[lane] <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return context.Canceled
	}
	defer func() { <-c.lanes[lane] }()
	p := c.proxy
	p.mu.Lock()
	p.events[eventID-1].WriteStartedNS = resourceNS()
	p.notify()
	p.mu.Unlock()
	err := dest.Write(ctx, typ, raw)
	writtenNS := resourceNS()
	p.mu.Lock()
	if err == nil {
		p.events[eventID-1].WrittenNS = writtenNS
	} else {
		p.events[eventID-1].WriteFailed = true
	}
	p.notify()
	p.mu.Unlock()
	return err
}

// Inject 是唯一产生额外 wire 消息的入口，必须由向量显式调用。
func (p *g2Proxy) Inject(ctx context.Context, connID uint64, direction string, typ websocket.MessageType, raw []byte) error {
	if direction != g2OICToOIS && direction != g2OISToOIC {
		return errors.New("无效注入方向")
	}
	if typ != websocket.MessageText && typ != websocket.MessageBinary || len(raw) > g2MaxWireMessage {
		return errors.New("无效注入消息")
	}
	p.mu.Lock()
	if connID == 0 || connID > uint64(len(p.conns)) {
		p.mu.Unlock()
		return errors.New("未知连接")
	}
	c := p.conns[connID-1]
	live := !c.snap.Closed && c.down != nil && c.up != nil
	p.mu.Unlock()
	if !live {
		return errors.New("连接未建立或已关闭")
	}
	owned := append([]byte(nil), raw...)
	e, _, err := c.capture(direction, typ, owned, true, 0)
	if err != nil {
		return err
	}
	return c.write(ctx, direction, e.ID, typ, owned)
}
func (p *g2Proxy) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closing = true
		p.cancel()
		conns := append([]*g2ProxyConn(nil), p.conns...)
		p.notify()
		p.mu.Unlock()
		_ = p.server.Close()
		for _, c := range conns {
			c.stop()
		}
		go func() { p.handlers.Wait(); <-p.serveDone; close(p.done) }()
	})
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("G2 proxy join: %w", ctx.Err())
	}
}
