package filesystem

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsNTFSModificationChecks(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	p := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	input := Object{"path": "target.txt", "mode": "replace", "content": "new", "expected_revision": revision([]byte("old"))}
	if err := os.WriteFile(p+":alternate", []byte("preserve"), 0600); err != nil {
		t.Skipf("named NTFS streams unavailable: %v", err)
	}
	wantError(t, s, "write_file", input, "UNSUPPORTED_FILE_ATTRIBUTES")
	if err := os.Remove(p + ":alternate"); err != nil {
		t.Fatal(err)
	}
	ptr, _ := windows.UTF16PtrFromString(p)
	const attrs = windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_ARCHIVE
	if err := windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	wantError(t, s, "write_file", input, "UNSUPPORTED_FILE_ATTRIBUTES")
	if err := windows.SetFileAttributes(ptr, attrs); err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, failure := run(t, s, "write_file", input)
	windows.CloseHandle(h)
	if failure == nil || failure.Code != "FILE_BUSY" || failure.Details["mutation_state"] != "not_applied" {
		t.Fatalf("expected sharing failure: %#v", failure)
	}
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	ok(t, s, "write_file", input)
	after, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("DACL changed", before, after, err)
	}
	got, err := windows.GetFileAttributes(ptr)
	if err != nil || got != attrs {
		t.Fatal(got, err)
	}
}

func TestWindowsPrivateStagingAndNormalAttribute(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	f, err := openStaging(s.Root, "private-stage", true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal(acl, err)
	}
	c, _, err := sd.Control()
	u, tokenErr := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || tokenErr != nil || c&windows.SE_DACL_PROTECTED == 0 || !strings.Contains(sd.String(), u.User.Sid.String()) {
		t.Fatal("initial staging ACL", sd.String(), err, tokenErr)
	}
	p := filepath.Join(dir, "normal.txt")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	ptr, _ := windows.UTF16PtrFromString(p)
	if err := windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_NORMAL); err != nil {
		t.Fatal(err)
	}
	ok(t, s, "write_file", Object{"path": "normal.txt", "mode": "replace", "expected_revision": revision([]byte("old")), "content": "new"})
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil || attrs != windows.FILE_ATTRIBUTE_NORMAL {
		t.Fatalf("got attrs %#x, err %v", attrs, err)
	}
}
