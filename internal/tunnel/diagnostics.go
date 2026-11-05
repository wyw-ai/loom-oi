package tunnel

import (
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// Diagnostics is a bounded, best-effort snapshot of first I/O boundaries.
// Relative times use this bridge's monotonic clock, not another process's clock.
// Zero means the boundary has not been observed at the snapshot time. Counters
// and protocol decisions do not depend on these diagnostic fields.
type Diagnostics struct {
	StartedAt            time.Time           `json:"started_at"`
	SampledNS            int64               `json:"sampled_ns"`
	LocalAddress         string              `json:"local_address"`
	PeerAddress          string              `json:"peer_address"`
	TargetReadStartedNS  int64               `json:"target_read_started_ns"`
	TargetReadReturnedNS int64               `json:"target_read_returned_ns"`
	TargetReadBytes      int64               `json:"target_read_bytes"`
	TargetReadResult     string              `json:"target_read_result"`
	LegWriteStartedNS    int64               `json:"leg_write_started_ns"`
	LegWriteReturnedNS   int64               `json:"leg_write_returned_ns"`
	LegWriteResult       string              `json:"leg_write_result"`
	FINReceivedNS        int64               `json:"fin_received_ns"`
	CloseWriteStartedNS  int64               `json:"close_write_started_ns"`
	CloseWriteReturnedNS int64               `json:"close_write_returned_ns"`
	CloseWriteResult     string              `json:"close_write_result"`
	Terminal             *TerminalDiagnostic `json:"terminal"`
}

// TerminalDiagnostic describes the winning local decision before its cleanup.
// It is neither the first network failure nor a cross-process clock witness.
type TerminalDiagnostic struct {
	AtNS         int64         `json:"at_ns"`
	Operation    string        `json:"operation"`
	ErrorClass   string        `json:"error_class"`
	ContextState string        `json:"context_state"`
	Code         protocol.Code `json:"code"`
	Remote       bool          `json:"remote"`
	FINSendState string        `json:"fin_send_state"`
	FINReceived  bool          `json:"fin_received"`
	BytesUp      int64         `json:"bytes_up"`
	BytesDown    int64         `json:"bytes_down"`
}

type boundaryDiagnostics struct {
	started                                                           time.Time
	local, peer                                                       string
	readStarted, readReturned, readBytes, writeStarted, writeReturned atomic.Int64
	finReceived, closeStarted, closeReturned                          atomic.Int64
	readResult, writeResult, closeResult                              atomic.Uint32
	terminal                                                          atomic.Pointer[TerminalDiagnostic]
}

// recordTerminal is called only in finish's winning once closure, after its
// original final license check and before any potentially blocking cleanup.
func (b *Bridge) recordTerminal(operation string, err error, code protocol.Code, remote bool) {
	state := "not_started"
	switch b.finSend.Load() {
	case finWriting:
		state = "writing"
	case finWritten:
		state = "written"
	case finFailed:
		state = "failed"
	}
	b.diagnostics.terminal.Store(&TerminalDiagnostic{
		AtNS: b.diagnostics.elapsed(), Operation: operation, ErrorClass: wsio.ErrorClass(err),
		ContextState: wsio.ContextState(b.ctx), Code: code, Remote: remote,
		FINSendState: state, FINReceived: b.finRecv.Load(), BytesUp: b.up.Load(), BytesDown: b.down.Load(),
	})
}

func (d *boundaryDiagnostics) elapsed() int64 { return max(1, time.Since(d.started).Nanoseconds()) }

func diagnosticResult(err error) uint32 {
	switch {
	case err == nil:
		return 1
	case errors.Is(err, io.EOF):
		return 2
	default:
		return 3
	}
}

func diagnosticName(v uint32) string {
	switch v {
	case 1:
		return "ok"
	case 2:
		return "eof"
	case 3:
		return "error"
	default:
		return "pending"
	}
}

// Diagnostics reports only addresses and finite boundary outcomes, never bytes,
// tickets, credentials or arbitrary network error text.
func (b *Bridge) Diagnostics() Diagnostics {
	d := &b.diagnostics
	out := Diagnostics{
		StartedAt: d.started, LocalAddress: d.local, PeerAddress: d.peer,
		TargetReadStartedNS: d.readStarted.Load(), TargetReadReturnedNS: d.readReturned.Load(), TargetReadBytes: d.readBytes.Load(), TargetReadResult: diagnosticName(d.readResult.Load()),
		LegWriteStartedNS: d.writeStarted.Load(), LegWriteReturnedNS: d.writeReturned.Load(), LegWriteResult: diagnosticName(d.writeResult.Load()),
		FINReceivedNS: d.finReceived.Load(), CloseWriteStartedNS: d.closeStarted.Load(), CloseWriteReturnedNS: d.closeReturned.Load(), CloseWriteResult: diagnosticName(d.closeResult.Load()),
	}
	if terminal := d.terminal.Load(); terminal != nil {
		copy := *terminal
		out.Terminal = &copy
	}
	out.SampledNS = d.elapsed()
	return out
}
