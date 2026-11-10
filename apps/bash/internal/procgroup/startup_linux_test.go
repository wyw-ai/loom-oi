//go:build linux

package procgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTargetStartupFailureIsNotAnExitResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deleted-shell")
	if e := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(path)
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	g, e := New()
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	if e = g.Start(cmd); e == nil {
		g.Close()
		cmd.Wait()
		t.Fatal("target exec failure was returned as successful startup")
	}
}
