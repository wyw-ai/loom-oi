package relay

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
)

type diagnosticAgentLeg struct {
	closed chan struct{}
	once   sync.Once
}

func (l *diagnosticAgentLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-l.closed:
		return 0, nil, net.ErrClosed
	}
}
func (l *diagnosticAgentLeg) Write(context.Context, websocket.MessageType, []byte) error { return nil }
func (l *diagnosticAgentLeg) Close(websocket.StatusCode, string) error                   { return l.CloseNow() }
func (l *diagnosticAgentLeg) CloseNow() error                                            { l.once.Do(func() { close(l.closed) }); return nil }

func TestRejectedBindKeepsActualBridgeDiagnostic(t *testing.T) {
	target := newTarget(t)
	conn, err := net.DialTCP("tcp", nil, target.ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	peer := target.next(t)
	leg := &diagnosticAgentLeg{closed: make(chan struct{})}
	defer leg.CloseNow()
	bridge := tunnel.New(context.Background(), leg, conn, 32768, 200*time.Millisecond, nil)
	log := &syncBuffer{}
	ch := &channel{id: "rejected-bind", exposureID: "lab-a/http", aborted: protocol.CodeRevoked}
	a := &Agent{channels: map[string]*channel{ch.id: ch}, closed: map[string]int64{}, log: slog.New(slog.NewJSONHandler(log, nil))}
	code := a.bind(ch, bridge)
	if code != protocol.CodeRevoked || ch.bridge != nil {
		t.Fatal("rejected bridge was bound")
	}
	bridge.Abort(code)
	a.done(ch, bridge.Result(), bridge.Diagnostics())
	expectRST(t, peer)
	var record struct {
		Message    string              `json:"msg"`
		Channel    string              `json:"channel"`
		Code       protocol.Code       `json:"code"`
		Boundaries *tunnel.Diagnostics `json:"io_boundaries"`
	}
	if err := json.Unmarshal([]byte(log.String()), &record); err != nil {
		t.Fatal(err)
	}
	if record.Message != "channel ended" || record.Channel != ch.id || record.Code != code || record.Boundaries == nil || record.Boundaries.Terminal == nil {
		t.Fatalf("rejected bind lost actual terminal: %+v", record)
	}
	terminal := record.Boundaries.Terminal
	if terminal.Operation != "external_abort" || terminal.Code != code || terminal.Remote || terminal.FINSendState != "not_started" || terminal.AtNS <= 0 || record.Boundaries.LocalAddress != conn.LocalAddr().String() {
		t.Fatalf("wrong bridge snapshot: %+v", record.Boundaries)
	}
	if ch.bridge != nil || len(a.channels) != 0 || a.closed[string(code)] != 1 {
		t.Fatal("diagnostic changed rejection/accounting")
	}
}
