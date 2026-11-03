package logx

import (
	"strings"
	"testing"
)

func TestLevels(t *testing.T) {
	for _, s := range []string{"", "debug", "info", "warn", "error", "WARN"} {
		if _, err := ParseLevel(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("loud accepted")
	}
	var b strings.Builder
	New(&b, "warn").Info("hidden")
	New(&b, "bogus").Info("shown")
	if strings.Contains(b.String(), "hidden") || !strings.Contains(b.String(), `"msg":"shown"`) {
		t.Fatal(b.String())
	}
}
