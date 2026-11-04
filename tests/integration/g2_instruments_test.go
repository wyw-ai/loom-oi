package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

// 此处只证明仪器的传输/门闩/身份边界，不能冒充真实 OIS/OIC G2 向量。
func g2TestUpstream(t *testing.T, ca *pki.CA, leaf *pki.Leaf, fn func(*websocket.Conn, *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || !bytes.Equal(r.TLS.PeerCertificates[0].RawSubjectPublicKeyInfo, leaf.Cert.RawSubjectPublicKeyInfo) {
			t.Error("代理更换了 Access 身份")
			http.Error(w, "identity", http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.ControlSubprotocolV2, protocol.ControlSubprotocol, protocol.TunnelSubprotocol}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		fn(c, r)
	}))
	cert := must(must(ca.Server("upstream")).TLS())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.Pool()}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}
func g2TestDial(t *testing.T, ctx context.Context, p *g2Proxy, ca *pki.CA, leaf *pki.Leaf, path, version string) (*websocket.Conn, error) {
	t.Helper()
	cert := must(leaf.TLS())
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.Pool(), Certificates: []tls.Certificate{cert}}}
	t.Cleanup(tr.CloseIdleConnections)
	c, res, err := websocket.Dial(ctx, p.URL()+path, &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}, Subprotocols: []string{version}, HTTPHeader: http.Header{protocol.HeaderAttachTicket: {"controlled-test-ticket"}}})
	if res != nil && res.Body != nil {
		res.Body.Close()
	}
	if err == nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, err
}
func TestG2ProxyRawBarrierAndStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ca := must(pki.NewCA("g2-instrument"))
	leaf := must(ca.Client("lab-a"))
	id := strings.Repeat("1", 32)
	channel := "ch_" + strings.Repeat("a", 26)
	check := []byte(`{"type":"admission_check","seq":7,"check_id":"` + id + `","stage":"final","channel_id":"` + channel + `"}`)
	result := []byte(`{ "type":"admission_result", "seq":9,"check_id":"` + id + `","channel_id":"` + channel + `","opaque": [ 1, 2 ] }`)
	later := []byte(`{"type":"observation_changed","seq":10,"opaque":true}`)
	received := make(chan []byte, 2)
	srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, r *http.Request) {
		if err := c.Write(ctx, websocket.MessageText, check); err != nil {
			return
		}
		for range 2 {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			received <- b
		}
		<-ctx.Done()
	})
	p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
	barrier := must(p.Arm(g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckFinal}))
	c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
	_, got, err := c.Read(ctx)
	if err != nil || !bytes.Equal(got, check) {
		t.Fatal("check 原始字节改变", string(got), err)
	}
	if err = c.Write(ctx, websocket.MessageText, result); err != nil {
		t.Fatal(err)
	}
	held := must(barrier.Wait(ctx))
	if held.Stage != protocol.CheckFinal || held.ChannelID != channel || held.CheckID != id || !bytes.Equal(held.Raw, result) || held.WrittenNS != 0 || held.Injected {
		t.Fatalf("错误命中: %+v", held)
	}
	if err = c.Write(ctx, websocket.MessageText, later); err != nil {
		t.Fatal(err)
	}
	for _, e := range p.Snapshot().Events {
		if e.Type == protocol.MsgObservationChanged {
			t.Fatal("门闩后消息越过原 wire 顺序")
		}
	}
	select {
	case b := <-received:
		t.Fatalf("被挡结果已发出: %s", b)
	default:
	}
	// Snapshot/Wait 的 Raw 必须与仪器内部完全分离。
	held.Raw[0] = 'X'
	snap := p.Snapshot()
	snap.Events[len(snap.Events)-1].Raw[0] = 'Y'
	if !bytes.Equal(p.Snapshot().Events[len(snap.Events)-1].Raw, result) {
		t.Fatal("外部修改污染原始记录")
	}
	barrier.Release()
	for _, want := range [][]byte{result, later} {
		select {
		case b := <-received:
			if !bytes.Equal(b, want) {
				t.Fatal("转发重排或改写", string(b))
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	ev := must(p.WaitWritten(ctx, held.ID))
	if ev.ReceivedNS <= 0 || ev.WriteStartedNS < ev.ReceivedNS || ev.WrittenNS < ev.WriteStartedNS {
		t.Fatalf("不真实写出时间 %+v", ev)
	}
	if err = p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s := p.Snapshot()
	if s.TransportConnections != 0 || s.Workers != 0 || s.PID <= 0 || s.Overflow || len(s.Connections) != 1 || !s.Connections[0].Closed || s.Connections[0].ClosedNS < s.Connections[0].AcceptedNS {
		t.Fatalf("未 join: %+v", s)
	}
}
func TestG2ProxyProviderReadyAndExplicitInjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ca := must(pki.NewCA("g2-ready"))
	leaf := must(ca.Client("lab-a"))
	channel := "ch_" + strings.Repeat("b", 26)
	peer := make(chan *websocket.Conn, 1)
	srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, r *http.Request) {
		if r.Header.Get(protocol.HeaderAttachTicket) != "controlled-test-ticket" {
			t.Error("ticket 未保持")
		}
		peer <- c
		_ = c.Write(ctx, websocket.MessageBinary, protocol.ReadyFrame())
		_, _, _ = c.Read(ctx)
	})
	p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
	b := must(p.Arm(g2Match{Role: g2RoleProvider, Direction: g2OISToOIC, Type: "READY", ChannelID: channel}))
	c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/channels/"+channel+"/attach", protocol.TunnelSubprotocol))
	<-peer
	e := must(b.Wait(ctx))
	if e.ChannelID != channel || e.Role != g2RoleProvider || len(e.Raw) != 1 || e.Raw[0] != protocol.FrameReady {
		t.Fatal(e)
	}
	b.Release()
	_, raw, err := c.Read(ctx)
	if err != nil || !bytes.Equal(raw, protocol.ReadyFrame()) {
		t.Fatal(raw, err)
	}
	abort := protocol.AbortFrame(protocol.CodeRevoked, "test injection")
	if err = p.Inject(ctx, e.ConnID, g2OISToOIC, websocket.MessageBinary, abort); err != nil {
		t.Fatal(err)
	}
	_, got, err := c.Read(ctx)
	if err != nil || !bytes.Equal(got, abort) {
		t.Fatal(got, err)
	}
	abort[0] = 0
	s := p.Snapshot()
	last := s.Events[len(s.Events)-1]
	if !last.Injected || last.ReceivedNS != 0 || last.WrittenNS == 0 || last.Type != "ABORT" || last.Raw[0] != protocol.FrameAbort {
		t.Fatal("注入与真实接收混淆", last)
	}
	if err = p.Inject(ctx, 0, g2OISToOIC, websocket.MessageBinary, nil); err == nil {
		t.Fatal("未知连接可注入")
	}
	if err = p.Inject(ctx, e.ConnID, "bad", websocket.MessageBinary, nil); err == nil {
		t.Fatal("非法方向可注入")
	}
	if err = p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.Inject(ctx, e.ConnID, g2OISToOIC, websocket.MessageBinary, nil); err == nil {
		t.Fatal("关闭后注入")
	}
}
func TestG2ProxyIdentityReconnectAndCloseHeld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ca := must(pki.NewCA("g2-identity"))
	leaf := must(ca.Client("lab-a"))
	other := must(ca.Client("lab-b"))
	var accepts atomic.Int32
	srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, r *http.Request) {
		accepts.Add(1)
		for {
			typ, raw, err := c.Read(ctx)
			if err != nil {
				return
			}
			if c.Write(ctx, typ, raw) != nil {
				return
			}
		}
	})
	p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
	if c, err := g2TestDial(t, ctx, p, ca, other, "/v1/access/control", protocol.ControlSubprotocolV2); err == nil {
		c.CloseNow()
		t.Fatal("其他 Access leaf 被代理替换成获准身份")
	}
	if accepts.Load() != 0 || len(p.Snapshot().Connections) != 0 {
		t.Fatal("错误身份抵达上游")
	}
	var previous uint64
	for range 2 {
		c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
		raw := []byte(`{"type":"heartbeat_ack","seq":1,"ack_seq":8}`)
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
		_, got, err := c.Read(ctx)
		if err != nil || !bytes.Equal(raw, got) {
			t.Fatal(err)
		}
		s := p.Snapshot()
		id := s.Connections[len(s.Connections)-1].ID
		if id <= previous {
			t.Fatal("重连复用 ID")
		}
		previous = id
		c.CloseNow()
	}
	b := must(p.Arm(g2Match{Type: "heartbeat_ack", Direction: g2OICToOIS}))
	c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"heartbeat_ack","seq":2}`))
	_ = must(b.Wait(ctx))
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s := p.Snapshot()
	if s.Workers != 0 || len(s.Connections) != 3 {
		t.Fatal(s)
	}
	for _, c := range s.Connections {
		if !c.Closed || c.Workers != 0 {
			t.Fatal(c)
		}
	}
	if _, err := p.Arm(g2Match{Type: "anything"}); err == nil {
		t.Fatal("关闭后建门闩")
	}
}
func TestG2ProxyCapacityAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ca := must(pki.NewCA("g2-capacity"))
	leaf := must(ca.Client("lab-a"))
	srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, r *http.Request) { _, _, _ = c.Read(ctx) })
	p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
	b := must(p.Arm(g2Match{Type: "hello"}))
	b.Cancel()
	if _, err := p.Arm(g2Match{}); err == nil {
		t.Fatal("空 matcher 接受")
	}
	for i := 0; i < g2MaxBarriers; i++ {
		if _, err := p.Arm(g2Match{Type: "never"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Arm(g2Match{Type: "overflow"}); err == nil {
		t.Fatal("门闩容量未限制")
	}
	c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
	// 在精确上限处注入仪器容量条件，验证不截断后继续假通过。
	p.mu.Lock()
	p.bytes = g2MaxRecordedBytes
	p.mu.Unlock()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","seq":1}`)); err != nil {
		t.Fatal(err)
	}
	_, _, _ = c.Read(ctx)
	s := p.Snapshot()
	if !s.Overflow || s.Failure != "event capacity" || len(s.Events) != 0 {
		t.Fatal("溢出未显式失败", s)
	}
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := p.WaitWritten(cancelled, 999); err == nil {
		t.Fatal("无事件仍成功")
	}
	if _, err := b.Wait(cancelled); err == nil || (!errors.Is(err, context.Canceled) && err.Error() != "代理已关闭") {
		t.Fatal(err)
	}
}

func TestG2ProxyBarrierReclamationPreservesHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ca := must(pki.NewCA("g2-reclaim"))
	leaf := must(ca.Client("lab-a"))
	received := make(chan []byte, 1)
	srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, _ *http.Request) {
		for {
			_, raw, err := c.Read(ctx)
			if err != nil {
				return
			}
			received <- raw
		}
	})
	p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
	c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
	raw := []byte(`{"type":"hello","seq":1}`)
	for range g2MaxBarriers + 1 {
		b := must(p.Arm(g2Match{Type: "hello", Direction: g2OICToOIS}))
		unused := must(p.Arm(g2Match{Type: "never"}))
		unused.Cancel()
		if _, err := unused.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter lost its explicit terminal: %v", err)
		}
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
		e := must(b.Wait(ctx))
		select {
		case <-received:
			t.Fatal("reclaiming an unrelated barrier released the live hold")
		default:
		}
		b.Release()
		_ = must(p.WaitWritten(ctx, e.ID))
		select {
		case got := <-received:
			if !bytes.Equal(got, raw) {
				t.Fatal("reclamation changed original wire bytes")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		b.Cancel()
	}
	for range 32 {
		b := must(p.Arm(g2Match{Type: "hello", Direction: g2OICToOIS}))
		b.Drop()
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
		e := must(b.Wait(ctx))
		b.Cancel()
		if actual, err := p.WaitWritten(ctx, e.ID); err == nil || !actual.Dropped || actual.WrittenNS != 0 {
			t.Fatal("completed Drop was silently forwarded")
		}
	}
	s := p.Snapshot()
	if len(s.Events) != g2MaxBarriers+1+32 {
		t.Fatalf("history truncated during reuse: %d", len(s.Events))
	}
	for i, e := range s.Events {
		if e.ID != uint64(i+1) || !bytes.Equal(e.Raw, raw) || e.Injected {
			t.Fatalf("immutable history/ID changed: %+v", e)
		}
	}
	p.mu.Lock()
	remaining := len(p.barriers)
	p.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("completed matchers retained: %d", remaining)
	}
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestG2ProxyHandshakeFailureFidelity(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(map[bool]string{false: "version_unsupported", true: "response_overflow"}[large], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ca := must(pki.NewCA("g2-handshake"))
			leaf := must(ca.Client("lab-a"))
			body := []byte(`{"code":"VersionUnsupported","message":"oi.control.v1 required"}`)
			if large {
				body = bytes.Repeat([]byte("X"), 1025)
			}
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				_, _ = w.Write(body)
			}))
			cert := must(must(ca.Server("g2-reject")).TLS())
			upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.Pool()}
			upstream.StartTLS()
			defer upstream.Close()
			p := newG2Proxy(t, strings.Replace(upstream.URL, "https:", "wss:", 1), ca, leaf)
			clientCert := must(leaf.TLS())
			tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.Pool(), Certificates: []tls.Certificate{clientCert}}}
			defer tr.CloseIdleConnections()
			c, res, err := websocket.Dial(ctx, p.URL()+"/v1/access/control", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}, Subprotocols: []string{protocol.ControlSubprotocolV2}})
			if err == nil {
				c.CloseNow()
				t.Fatal("拒绝握手变成成功")
			}
			if res == nil {
				t.Fatal("未传播HTTP拒绝")
			}
			got, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if !large && (res.StatusCode != 400 || !bytes.Equal(got, body)) {
				t.Fatalf("拒绝语义改变: %d %s", res.StatusCode, got)
			}
			if large && res.StatusCode != 502 {
				t.Fatal("不完整body仍当完整响应", res.StatusCode)
			}
			if err = p.Close(ctx); err != nil {
				t.Fatal(err)
			}
			s := p.Snapshot()
			if s.Overflow != large || s.TransportConnections != 0 || s.Workers != 0 || len(s.Connections) != 1 || s.Connections[0].UpstreamStatus != 400 {
				t.Fatal(s)
			}
			if !large && !bytes.Equal(s.Connections[0].HandshakeBody, body) {
				t.Fatal("未保留完整拒绝原文")
			}
		})
	}
}

func TestG2ProxyTransportBoundBeforeTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	defer ln.Close()
	rejected := make(chan struct{}, 1)
	bounded := &g2CappedListener{Listener: ln, slots: make(chan struct{}, 1), rejected: func() { rejected <- struct{}{} }}
	accepted := make(chan net.Conn, 2)
	go func() {
		for range 2 {
			c, err := bounded.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	a := must(net.Dial("tcp", ln.Addr().String()))
	defer a.Close()
	var first net.Conn
	select {
	case first = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	b := must(net.Dial("tcp", ln.Addr().String()))
	defer b.Close()
	select {
	case <-rejected:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(bounded.slots) != 1 {
		t.Fatal("TLS 前连接未受限")
	}
	_ = b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := b.Read(make([]byte, 1)); err == nil {
		t.Fatal("超限socket未关闭")
	}
	first.Close()
	first.Close()
	if len(bounded.slots) != 0 {
		t.Fatal("双关未释放恰一个槽")
	}
	c := must(net.Dial("tcp", ln.Addr().String()))
	defer c.Close()
	select {
	case last := <-accepted:
		last.Close()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(bounded.slots) != 0 {
		t.Fatal("连接槽泄漏")
	}
}

// 丢响应必须保留完整事实，且不能因后续 Release/Cancel 重新写出。
func TestG2ProxyDropIsExplicitAndOrdered(t *testing.T) {
	for _, beforeHit := range []bool{false, true} {
		name := "after_hit"
		if beforeHit {
			name = "before_hit"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ca := must(pki.NewCA("g2-drop"))
			leaf := must(ca.Client("lab-a"))
			received := make(chan []byte, 2)
			srv := g2TestUpstream(t, ca, leaf, func(c *websocket.Conn, _ *http.Request) {
				for {
					_, raw, err := c.Read(ctx)
					if err != nil {
						return
					}
					received <- raw
				}
			})
			p := newG2Proxy(t, strings.Replace(srv.URL, "https:", "wss:", 1), ca, leaf)
			b := must(p.Arm(g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult}))
			if beforeHit {
				b.Drop()
			}
			c := must(g2TestDial(t, ctx, p, ca, leaf, "/v1/access/control", protocol.ControlSubprotocolV2))
			original := []byte(`{ "type":"admission_result", "seq":7, "opaque":[1, 2] }`)
			later := []byte(`{"type":"heartbeat_ack","seq":8,"ack_seq":1}`)
			if err := c.Write(ctx, websocket.MessageText, original); err != nil {
				t.Fatal(err)
			}
			e := must(b.Wait(ctx))
			b.Drop()
			b.Release()
			b.Cancel()
			if err := c.Write(ctx, websocket.MessageText, later); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-received:
				if !bytes.Equal(got, later) {
					t.Fatalf("丢弃消息被写出或次序改变: %s", got)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			dropped, err := p.WaitWritten(ctx, e.ID)
			if err == nil || !dropped.Dropped || dropped.Injected || dropped.ReceivedNS <= 0 || dropped.WriteStartedNS != 0 || dropped.WrittenNS != 0 || dropped.WriteFailed || !bytes.Equal(dropped.Raw, original) {
				t.Fatalf("丢弃事实不完整: %+v %v", dropped, err)
			}
			injected := []byte(`{"type":"admission_result","seq":99}`)
			if err = p.Inject(ctx, e.ConnID, g2OICToOIS, websocket.MessageText, injected); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-received:
				if !bytes.Equal(got, injected) {
					t.Fatal("注入内容变化")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			s := p.Snapshot()
			last := s.Events[len(s.Events)-1]
			if !last.Injected || last.Dropped || last.ReceivedNS != 0 || last.WrittenNS <= 0 {
				t.Fatalf("注入混淆: %+v", last)
			}
			if err = p.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if s = p.Snapshot(); s.Workers != 0 || s.TransportConnections != 0 || !s.Connections[0].Closed {
				t.Fatalf("丢弃后未join: %+v", s)
			}
		})
	}
}
