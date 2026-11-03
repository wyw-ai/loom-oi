package procstats

import (
	"runtime"
	"testing"
)

func TestRead(t *testing.T) {
	s := Read()
	if s.Goroutines < 1 || s.HeapBytes <= 0 {
		t.Fatalf("%+v", s)
	}
	if runtime.GOOS == "linux" {
		if s.OpenFDs <= 0 || s.RSSBytes <= 0 || s.CPUMillis < 0 {
			t.Fatalf("linux counters missing: %+v", s)
		}
	} else if s.OpenFDs != -1 || s.RSSBytes != -1 || s.CPUMillis != -1 {
		t.Fatalf("unsupported counters must be -1: %+v", s)
	}
}
