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
	if l.NOIS != 64 || l.NAccess != 32 || l.NSubject != 16 || l.DataMax != 32768 || l.PrepareMS != 10000 ||
		l.HeartbeatMS != 10000 || l.OfflineMisses != 3 || l.LicenseTTLMS != 30000 || l.LicenseMarginMS != 2000 ||
		l.IdleMS != 600000 || l.TokenTTLMS != int64(30*day/time.Millisecond) || l.WriteQueue != 64 {
		t.Fatalf("defaults drifted from B4 §3: %+v", l)
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
