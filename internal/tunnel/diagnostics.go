package tunnel

import (
	"errors"
	"io"
	"sync/atomic"
	"time"
)

// Diagnostics is a bounded, best-effort snapshot of first I/O boundaries.
// Relative times use this bridge's monotonic clock, not another process's clock.
// Zero means the boundary has not been observed at the snapshot time. Counters
// and protocol decisions do not depend on these diagnostic fields.
type Diagnostics struct {
	StartedAt            time.Time `json:"started_at"`
	SampledNS            int64     `json:"sampled_ns"`
	LocalAddress         string    `json:"local_address"`
	PeerAddress          string    `json:"peer_address"`
	TargetReadStartedNS  int64     `json:"target_read_started_ns"`
	TargetReadReturnedNS int64     `json:"target_read_returned_ns"`
	TargetReadBytes      int64     `json:"target_read_bytes"`
	TargetReadResult     string    `json:"target_read_result"`
	LegWriteStartedNS    int64     `json:"leg_write_started_ns"`
	LegWriteReturnedNS   int64     `json:"leg_write_returned_ns"`
	LegWriteResult       string    `json:"leg_write_result"`
	FINReceivedNS        int64     `json:"fin_received_ns"`
	CloseWriteStartedNS  int64     `json:"close_write_started_ns"`
	CloseWriteReturnedNS int64     `json:"close_write_returned_ns"`
	CloseWriteResult     string    `json:"close_write_result"`
}

type boundaryDiagnostics struct {
	started                                                           time.Time
	local, peer                                                       string
	readStarted, readReturned, readBytes, writeStarted, writeReturned atomic.Int64
	finReceived, closeStarted, closeReturned                          atomic.Int64
	readResult, writeResult, closeResult                              atomic.Uint32
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
	out.SampledNS = d.elapsed()
	return out
}
