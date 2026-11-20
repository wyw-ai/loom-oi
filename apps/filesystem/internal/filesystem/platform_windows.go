package filesystem

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const readFlags = 0

var reopenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func openStaging(root *os.Root, name string, replacing bool) (*os.File, error) {
	if !replacing {
		return root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	}
	// Protect the staging name at creation. A permissive parent DACL must never
	// let another account acquire a handle before the source DACL is installed.
	parent, err := root.Open(path.Dir(name))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	base, err := windows.NewNTUnicodeString(path.Base(name))
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(parent.Fd()), ObjectName: base,
		Attributes: windows.OBJ_CASE_INSENSITIVE, SecurityDescriptor: sd,
	}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var h windows.Handle
	const access = windows.FILE_GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.WRITE_DAC
	const options = windows.FILE_NON_DIRECTORY_FILE | windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT
	err = windows.NtCreateFile(&h, access, &oa, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_CREATE, options, 0, 0)
	if status, ok := err.(windows.NTStatus); ok {
		return nil, status.Errno()
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

func publishError(err error, target *os.File) *Failure {
	// NTFS replace may return ACCESS_DENIED when another handle denies delete sharing.
	// Probe the same target handle, preserving genuine permission failures.
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		h, _, reason := reopenFile.Call(target.Fd(), windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
		if windows.Handle(h) != windows.InvalidHandle {
			windows.CloseHandle(windows.Handle(h))
		} else if errors.Is(reason, windows.ERROR_SHARING_VIOLATION) {
			return fail("FILE_BUSY", "target is open without delete sharing")
		}
	}
	return ioError(err)
}

// Windows does not expose a portable equivalent of syncing a parent directory.
func syncDirectory(*os.Root, string) error { return nil }

func platformError(err error) string {
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return "FILE_BUSY"
	case errors.Is(err, windows.ERROR_NOT_SUPPORTED), errors.Is(err, windows.ERROR_INVALID_FUNCTION):
		return "UNSUPPORTED_FILESYSTEM"
	case errors.Is(err, windows.ERROR_DIRECTORY):
		return "NOT_A_DIRECTORY"
	}
	return "IO_ERROR"
}
func getInfo(f *os.File) (windows.ByHandleFileInformation, error) {
	var i windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &i)
	return i, err
}
func observation(f *os.File, st os.FileInfo) (string, *Failure) {
	i, e := getInfo(f)
	if e != nil {
		return "", ioError(e)
	}
	return fmt.Sprintf("%d:%d:%d|%d:%d", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow, st.Size(), st.ModTime().UnixNano()), nil
}
func checkVolume(f *os.File) *Failure {
	var name [32]uint16
	if e := windows.GetVolumeInformationByHandle(windows.Handle(f.Fd()), nil, 0, nil, nil, nil, &name[0], uint32(len(name))); e != nil {
		return ioError(e)
	}
	if !strings.EqualFold(windows.UTF16ToString(name[:]), "NTFS") {
		return fail("UNSUPPORTED_FILESYSTEM", "mutations require local NTFS")
	}
	var path [32768]uint16
	n, e := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &path[0], uint32(len(path)), 0)
	if e != nil {
		return ioError(e)
	}
	if n >= uint32(len(path)) || strings.HasPrefix(strings.ToLower(windows.UTF16ToString(path[:])), `\\?\unc\`) {
		return fail("UNSUPPORTED_FILESYSTEM", "mutations require a local volume")
	}
	return nil
}
func validateSource(f *os.File, st os.FileInfo) *Failure {
	if e := checkVolume(f); e != nil {
		return e
	}
	i, err := getInfo(f)
	if err != nil {
		return ioError(err)
	}
	const supported = windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_ATTRIBUTE_ARCHIVE | windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_SYSTEM
	if i.NumberOfLinks != 1 || i.FileAttributes & ^uint32(supported) != 0 {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "unsupported attributes, reparse point or multiple links")
	}
	// FILE_STREAM_INFO starts with NextEntryOffset/StreamNameLength, then two LARGE_INTEGERs.
	b := make([]byte, 65536)
	err = windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileStreamInfo, &b[0], uint32(len(b)))
	if err != nil {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "unable to inspect alternate streams")
	}
	n := binary.LittleEndian.Uint32(b[4:])
	if binary.LittleEndian.Uint32(b[:4]) != 0 || n != 14 || windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&b[24])), int(n/2))) != "::$DATA" {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "named data streams are unsupported")
	}
	return nil
}
func fileSecurity(f *os.File) (*windows.SECURITY_DESCRIPTOR, error) {
	const mask = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, mask)
	if err != nil {
		return nil, err
	}
	// SetSecurityInfo may mark a legacy ACL as converted to automatic
	// inheritance. Clear only this bookkeeping bit on our in-memory copy.
	// The DACL protection bit and the ordered ACEs (including their inheritance
	// flags), owner and group must still match at verification and publication.
	if err := sd.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
		return nil, err
	}
	return sd, nil
}

func copyAttributes(src, dst *os.File) *Failure {
	unsupported := func() *Failure {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "could not preserve owner, group, DACL or regular attributes")
	}
	source, err := fileSecurity(src)
	if err != nil {
		return unsupported()
	}
	target, err := fileSecurity(dst)
	if err != nil {
		return unsupported()
	}
	so, _, e1 := source.Owner()
	to, _, e2 := target.Owner()
	sg, _, e3 := source.Group()
	tg, _, e4 := target.Group()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !windows.EqualSid(so, to) || !windows.EqualSid(sg, tg) {
		return unsupported()
	}
	sourceACL, targetACL := source.String(), target.String()
	if sourceACL == "" || targetACL == "" {
		return unsupported()
	}
	if sourceACL != targetACL {
		h := windows.Handle(dst.Fd())
		acl, _, err := source.DACL()
		if err != nil {
			return unsupported()
		}
		control, _, err := source.Control()
		if err != nil {
			return unsupported()
		}
		flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
		if control&windows.SE_DACL_PROTECTED != 0 {
			flags |= windows.PROTECTED_DACL_SECURITY_INFORMATION
		} else {
			flags |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
		}
		if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil); err != nil {
			return unsupported()
		}
		got, err := fileSecurity(dst)
		if err != nil || got.String() != sourceACL {
			return unsupported()
		}
	}
	return copyFileAttributes(src, dst)
}

func copyFileAttributes(src, dst *os.File) *Failure {
	a, err := getInfo(src)
	if err != nil {
		return ioError(err)
	}
	return setFileAttributes(dst, a.FileAttributes)
}

func setFileAttributes(dst *os.File, want uint32) *Failure {
	unsupported := func() *Failure {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "could not preserve regular file attributes")
	}
	b, err := getInfo(dst)
	if err != nil {
		return unsupported()
	}
	if want != b.FileAttributes {
		basic := struct {
			Times      [4]int64
			Attrs, Pad uint32
		}{Attrs: want}
		if windows.SetFileInformationByHandle(windows.Handle(dst.Fd()), windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))) != nil {
			return unsupported()
		}
		b, err = getInfo(dst)
		if err != nil || b.FileAttributes != want {
			return unsupported()
		}
	}
	return nil
}

func finishStaging(root *os.Root, name string, identity os.FileInfo, source *os.File) (func() *Failure, func(), *Failure) {
	// NTFS may set ARCHIVE on writer close or rename. Retain an attributes-only
	// handle to the verified staging identity across the commit point.
	staged, err := root.Open(name)
	if err != nil {
		return nil, nil, ioError(err)
	}
	defer staged.Close()
	st, err := staged.Stat()
	if err != nil || !os.SameFile(st, identity) {
		return nil, nil, fail("REVISION_CONFLICT", "staging file identity changed")
	}
	want, err := getInfo(source)
	if err != nil {
		return nil, nil, ioError(err)
	}
	h, _, reason := reopenFile.Call(staged.Fd(), windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, nil, ioError(reason)
	}
	attributes := os.NewFile(h, name)
	restore := func() *Failure { return setFileAttributes(attributes, want.FileAttributes) }
	if e := restore(); e != nil {
		attributes.Close()
		return nil, nil, e
	}
	return restore, func() { attributes.Close() }, nil
}

func attributeKey(file *os.File) (string, *Failure) {
	sd, err := fileSecurity(file)
	if err != nil || sd.String() == "" {
		return "", fail("UNSUPPORTED_FILE_ATTRIBUTES", "could not recheck the target security descriptor")
	}
	info, err := getInfo(file)
	if err != nil {
		return "", ioError(err)
	}
	return fmt.Sprintf("%08x:%s", info.FileAttributes, sd.String()), nil
}
