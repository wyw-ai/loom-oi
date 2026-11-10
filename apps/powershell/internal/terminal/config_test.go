//go:build windows

package terminal

import (
	"strings"
	"testing"
)

func TestRejectsOtherShellKinds(t *testing.T) {
	for _, kind := range []string{"bash", "cmd", "python"} {
		t.Run(kind, func(t *testing.T) {
			_, err := ParseConfig(map[string]any{"shell_kind": kind, "default_cwd": t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), "requires shell_kind powershell") {
				t.Fatalf("unsupported shell kind was not rejected: %v", err)
			}
		})
	}
}
