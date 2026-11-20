package filesystem

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
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

func TestWindowsLegacyInheritedDACL(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	parentSD, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "legacy")
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		t.Fatal(err)
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: parentSD}
	sa.Length = uint32(unsafe.Sizeof(sa))
	if err := windows.CreateDirectory(name, &sa); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "data")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	const mask = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, mask)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := before.Control()
	if err != nil || control&windows.SE_DACL_AUTO_INHERITED != 0 {
		t.Fatalf("expected a legacy inherited file DACL: %s, %v", before, err)
	}
	ok(t, s, "write_file", Object{"path": "legacy/data", "mode": "replace", "expected_revision": revision([]byte("before")), "content": "after"})
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, mask)
	if err != nil {
		t.Fatal(err)
	}
	if err := after.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatalf("security changed: before %s, after %s", before, after)
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
	if err != nil || tokenErr != nil || c&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("initial staging ACL", sd.String(), err, tokenErr)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	const fileAllAccess = 0x001f01ff // FILE_ALL_ACCESS in winnt.h.
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != fileAllAccess ||
		!u.User.Sid.Equals((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
		t.Fatalf("expected a single full-access ACE for the worker user, got %s", sd)
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
