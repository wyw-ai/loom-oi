package limits

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultsMatchB4(t *testing.T) {
	l := Defaults()
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	// Every value of the B4 §3 table, written out independently.
	want := Limits{
		NOIS: 64, NAccess: 32, NSubject: 16, MaxLocalChannels: 32,
		DataMax: 32 * 1024, HTTPMax: 64 * 1024, CtrlMax: 256 * 1024,
		PageMax: 100, PageDefault: 50, MaxExposures: 128, DescMax: 2 * 1024,
		PrepareMS: 10_000, DialMS: 5_000, CloseMS: 5_000, RetainMS: 60_000,
		HeartbeatMS: 10_000, OfflineMisses: 3, LicenseTTLMS: 30_000, LicenseMarginMS: 2_000,
		PingMS: 15_000, IdleMS: 10 * 60_000,
		TokenTTLMS: 30 * 86_400_000, TokenTTLMaxMS: 90 * 86_400_000,
		WriteQueue: 64, SQLiteBusyMS: 5_000, StoreProbeMS: 5_000,
	}
	if l != want {
		t.Fatalf("defaults drifted from B4 §3:\n got %+v\nwant %+v", l, want)
	}
	if D(1500) != 1500*time.Millisecond {
		t.Fatal("D")
	}
}

func TestValidateRejects(t *testing.T) {
	mut := []func(*Limits){
		func(l *Limits) { l.NOIS = 0 },
		func(l *Limits) { l.PrepareMS = -1 },
		func(l *Limits) { l.LicenseMarginMS = l.LicenseTTLMS },
		func(l *Limits) { l.LicenseMarginMS = -1 },
		func(l *Limits) { l.PageDefault = l.PageMax + 1 },
		func(l *Limits) { l.TokenTTLMS = l.TokenTTLMaxMS + 1 },
		func(l *Limits) { l.DataMax = 2 << 20 },
	}
	for i, m := range mut {
		l := Defaults()
		m(&l)
		if err := l.Validate(); err == nil || !strings.Contains(err.Error(), "limits.") {
			t.Errorf("case %d: %v", i, err)
		}
	}
}
