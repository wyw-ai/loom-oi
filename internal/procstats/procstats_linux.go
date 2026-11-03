//go:build linux

package procstats

import (
	"os"
	"strconv"
	"strings"
)

// clockTicks is USER_HZ, which is 100 on every Linux ABI Go supports.
const clockTicks = 100

func osCounters() (fds int, rss, cpuMillis int64) {
	return countFDs("/proc/self/fd"), readRSS("/proc/self/statm"), readCPU("/proc/self/stat")
}

func countFDs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return -1
	}
	return len(entries)
}

func readRSS(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return -1
	}
	pages, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return -1
	}
	return pages * int64(os.Getpagesize())
}

// readCPU sums utime and stime (fields 14 and 15 of /proc/self/stat).
func readCPU(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	s := string(b)
	// Fields after the parenthesised command name, which may contain spaces.
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return -1
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 13 {
		return -1
	}
	u, err1 := strconv.ParseInt(f[11], 10, 64)
	k, err2 := strconv.ParseInt(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return -1
	}
	return (u + k) * 1000 / clockTicks
}
