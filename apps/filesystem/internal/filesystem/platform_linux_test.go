package filesystem

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLinuxLinkAndSpecialFileRules(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("a"), 0755); err != nil {
		t.Fatal(err)
	}
	os.Symlink("real", filepath.Join(dir, "inside"))
	os.Symlink("../outside", filepath.Join(dir, "escape"))
	if r := ok(t, s, "read_file", Object{"path": "inside"}); r["text"] != "a" {
		t.Fatal(r)
	}
	wantError(t, s, "read_file", Object{"path": "escape"}, "PATH_OUTSIDE_ROOT")
	wantError(t, s, "write_file", Object{"path": "inside", "mode": "replace", "expected_revision": revision([]byte("a")), "content": "b"}, "UNSUPPORTED_FILE_ATTRIBUTES")
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	wantError(t, s, "read_file", Object{"path": "pipe"}, "NOT_A_FILE")
	wantError(t, s, "list_directory", Object{"path": "pipe"}, "NOT_A_DIRECTORY")
	ok(t, s, "write_file", Object{"path": "real", "mode": "replace", "expected_revision": revision([]byte("a")), "content": "b"})
	st, err := os.Stat(filepath.Join(dir, "real"))
	if err != nil || st.Mode().Perm() != 0755 {
		t.Fatal(st, err)
	}
}
