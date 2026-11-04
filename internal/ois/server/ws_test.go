package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// A peer that never answers the closing handshake still loses its leg
// within T_close (B1 §5, TV-B4-03). coder/websocket alone would hold the
// connection for 5 s and ignore the T_close force-close meanwhile.
func TestUncooperativeLegDroppedWithinTClose(t *testing.T) {
	w := newWorld(t)
	o, prep := w.open()
	rawc := make(chan net.Conn, 1)
	dialer := &tls.Dialer{Config: &tls.Config{RootCAs: w.ca.Pool(), Certificates: []tls.Certificate{must(w.leaf.TLS())}}}
	client := &http.Client{Transport: &http.Transport{DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			rawc <- c
		}
		return c, err
	}}}
	h := http.Header{protocol.HeaderAttachTicket: {prep.ProviderTicket}}
	prov, _, err := w.dialWS(client, "wss://"+w.srv.AccessAddr()+"/v1/channels/"+o.ChannelID+"/attach", h, protocol.TunnelSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	defer prov.CloseNow()
	raw := <-rawc
	cons := w.attachConsumer(o)
	defer cons.CloseNow()
	if frame(t, cons).Type != protocol.FrameReady {
		t.Fatal("READY")
	}
	// The consumer aborts. The provider never reads through its WebSocket,
	// so it never answers OIS's close frame.
	sendFrame(t, cons, protocol.AbortFrame(protocol.CodeClosedByClient, ""))
	start := time.Now()
	bound := limits.D(w.cfg.Limits.CloseMS) + time.Second
	_ = raw.SetReadDeadline(start.Add(4 * time.Second))
	buf := make([]byte, 4096)
	for {
		_, err := raw.Read(buf)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("OIS kept the leg of an unresponsive peer")
		}
		if err != nil {
			break
		}
	}
	if d := time.Since(start); d > bound {
		t.Fatalf("leg dropped after %v, want within T_close+1s (%v)", d, bound)
	}
}
