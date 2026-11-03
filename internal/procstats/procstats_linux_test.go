//go:build linux

package procstats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxParsersRejectBadInput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if countFDs(filepath.Join(dir, "missing")) != -1 {
		t.Error("missing fd dir")
	}
	for _, body := range []string{"", "1", "1 x"} {
		if readRSS(write("statm", body)) != -1 {
			t.Errorf("statm %q", body)
		}
	}
	if readRSS(filepath.Join(dir, "missing")) != -1 {
		t.Error("missing statm")
	}
	for _, body := range []string{"no paren", "1 (x) S 1", "1 (a b) S 1 2 3 4 5 6 7 8 9 10 x y"} {
		if readCPU(write("stat", body)) != -1 {
			t.Errorf("stat %q", body)
		}
	}
	if readCPU(filepath.Join(dir, "missing")) != -1 {
		t.Error("missing stat")
	}
	if got := readCPU(write("stat", "1 (a b) S 1 2 3 4 5 6 7 8 9 10 250 50 0")); got != 3000 {
		t.Errorf("cpu %d", got)
	}
}
