//go:build linux

package integration

import (
	"golang.org/x/sys/unix"
	"testing"
)

func g2ClockPlatform(t *testing.T) { t.Helper() }

func g2ReadClock(t *testing.T) g2ClockSample {
	t.Helper()
	var before, real, after unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &before); err != nil {
		t.Fatal(err)
	}
	if err := unix.ClockGettime(unix.CLOCK_REALTIME, &real); err != nil {
		t.Fatal(err)
	}
	resource := resourceNS()
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &after); err != nil {
		t.Fatal(err)
	}
	return g2ClockSample{ResourceNS: resource, RealtimeNS: real.Nano(), MonotonicNS: (before.Nano() + after.Nano()) / 2, MonotonicBeforeNS: before.Nano(), MonotonicAfterNS: after.Nano()}
}
