//go:build !linux

package procstats

// osCounters is not implemented outside Linux; B4 records O3 on Linux CI.
func osCounters() (fds int, rss, cpuMillis int64) { return -1, -1, -1 }
