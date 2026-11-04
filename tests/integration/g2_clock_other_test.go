//go:build !linux

package integration

import "testing"

func g2ClockPlatform(t *testing.T) {
	t.Helper()
	t.Fatal("OI_IT_CLOCK requires the dedicated Linux hosted VM")
}
func g2ReadClock(t *testing.T) g2ClockSample {
	t.Helper()
	t.Fatal("real clock unavailable on this platform")
	return g2ClockSample{}
}
