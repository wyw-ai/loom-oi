package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Set the receive capacity before connect advertises the initial TCP window.
// Shrinking it only after the handshake permits Linux to negotiate a much
// larger initial window and grow the peer's send buffer before the first
// supposedly slow response. The HTTP request/byte limits remain unchanged.
func g2CompetitionDialer() *net.Dialer {
	return &net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		if err := raw.Control(func(fd uintptr) { socketErr = g2CompetitionSocketBuffer(fd) }); err != nil {
			return err
		}
		return socketErr
	}}
}

func TestG2CompetitionQueryPhase(t *testing.T) {
	now := int64(time.Second)
	events := []g2WireEvent{}
	for i := range 8 {
		id := fmt.Sprintf("%032x", i+1)
		raw, err := json.Marshal(protocol.ObservationQuery{QueryID: id, DeadlineMS: 499,
			Binding: protocol.ObservationBinding{ExposureID: fmt.Sprintf("lab-a/%d", i)}})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, g2WireEvent{Type: protocol.MsgObservationQuery, Direction: g2OISToOIC,
			Raw: raw, ReceivedNS: now - int64(50*time.Millisecond), WrittenNS: now - int64(49*time.Millisecond)})
	}
	if !g2CompetitionYoungQueries(events, now, 200*time.Millisecond) {
		t.Fatal("actual young batch rejected")
	}
	for _, change := range []string{"old", "duplicate", "injected", "unwritten", "future", "spent_deadline", "same_source"} {
		t.Run(change, func(t *testing.T) {
			bad := append([]g2WireEvent(nil), events...)
			switch change {
			case "old":
				bad[0].ReceivedNS = now - int64(500*time.Millisecond)
			case "duplicate":
				bad[0].Raw = bad[1].Raw
			case "injected":
				bad[0].Injected = true
			case "unwritten":
				bad[0].WrittenNS = 0
			case "future":
				bad[0].ReceivedNS = now + 1
			case "spent_deadline", "same_source":
				var q protocol.ObservationQuery
				if err := json.Unmarshal(bad[0].Raw, &q); err != nil {
					t.Fatal(err)
				}
				if change == "spent_deadline" {
					q.DeadlineMS = 100
				} else {
					q.Binding.ExposureID = "lab-a/1"
				}
				bad[0].Raw = must(json.Marshal(q))
			}
			if g2CompetitionYoungQueries(bad, now, 200*time.Millisecond) {
				t.Fatal("invalid input was counted as an actual young query")
			}
		})
	}
}

func TestG2CompetitionSlowInput(t *testing.T) {
	registry := &metrics.Registry{}
	server := httptest.NewServer(metrics.NewHandler(registry))
	defer server.Close()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: transport}
	addr := server.Listener.Addr().String()
	before := g2ExporterRead(t, hc, addr)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	slow, err := g2BeginSlowScrapesWithDialer(ctx, addr, g2CompetitionDialer())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := slow.Close(); err != nil {
			t.Error(err)
		}
	}()
	rejected, failed := false, false
	for ctx.Err() == nil {
		x, err := g2ReadExposition(ctx, hc, addr)
		if err != nil {
			t.Fatal(err)
		}
		switch x.Status {
		case http.StatusTooManyRequests:
			rejected = true
		case http.StatusOK:
			if g2CompetitionExportFailures(x)-g2CompetitionExportFailures(before) >= 2 {
				failed = true
			}
		default:
			t.Fatalf("unexpected actual exporter response %d", x.Status)
		}
		if rejected && failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !rejected || !failed {
		t.Fatalf("bounded input did not establish two real blocked exporters before close: rejected=%v failures=%v", rejected, failed)
	}
	writes := slow.Snapshot()
	if len(writes) != 2 {
		t.Fatalf("slow sockets=%d", len(writes))
	}
	for _, w := range writes {
		if w.Error != "" || w.WrittenBytes != w.PlannedBytes || w.PlannedBytes <= 0 || w.CompletedNS < w.StartedNS || w.CompletedNS-w.StartedNS > int64(time.Second) {
			t.Fatalf("bounded request stream not actually sent: %+v", w)
		}
	}
	if err := slow.Close(); err != nil {
		t.Fatal(err)
	}
	_ = g2ExporterRead(t, hc, addr)
}
