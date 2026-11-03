// Package procstats reports process resource counters for the B4 §4
// resource regression and O3 records. Counters an OS cannot provide
// cheaply are reported as -1.
package procstats

import (
	"runtime"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Read samples the current process.
func Read() protocol.ProcessStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fds, rss, cpu := osCounters()
	return protocol.ProcessStats{
		Goroutines: runtime.NumGoroutine(),
		OpenFDs:    fds,
		RSSBytes:   rss,
		HeapBytes:  int64(m.HeapAlloc),
		CPUMillis:  cpu,
	}
}
