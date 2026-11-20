package filesystem

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

const readFlags = syscall.O_NONBLOCK

func platformError(err error) string {
	if errors.Is(err, syscall.ENOTDIR) {
		return "NOT_A_DIRECTORY"
	}
	if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EXDEV) {
		return "UNSUPPORTED_FILESYSTEM"
	}
	return "IO_ERROR"
}
func observation(f *os.File, st os.FileInfo) (string, *Failure) {
	s := st.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d|%d:%d", s.Dev, s.Ino, st.Size(), st.ModTime().UnixNano()), nil
}
func checkVolume(*os.File) *Failure { return nil }

func openStaging(root *os.Root, name string, replacing bool) (*os.File, error) {
	mode := os.FileMode(0666)
	if replacing {
		mode = 0600
	}
	return root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
}

func attributeKey(f *os.File) (string, *Failure) {
	st, err := f.Stat()
	if err != nil {
		return "", ioError(err)
	}
	attr := st.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d:%o", attr.Uid, attr.Gid, st.Mode().Perm()), nil
}

func publishError(err error, target *os.File) *Failure { return ioError(err) }

func finishStaging(*os.Root, string, os.FileInfo, *os.File) (func() *Failure, func(), *Failure) {
	return nil, nil, nil
}

func syncDirectory(root *os.Root, dir string) error {
	f, err := root.OpenFile(dir, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func validateSource(_ *os.File, st os.FileInfo) *Failure {
	if st.Sys().(*syscall.Stat_t).Nlink != 1 || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "unsupported hard links or special permission bits")
	}
	return nil
}
func copyAttributes(src, dst *os.File) *Failure {
	a, err := src.Stat()
	if err != nil {
		return ioError(err)
	}
	b, err := dst.Stat()
	if err != nil {
		return ioError(err)
	}
	x, y := a.Sys().(*syscall.Stat_t), b.Sys().(*syscall.Stat_t)
	if x.Uid != y.Uid || x.Gid != y.Gid {
		return fail("UNSUPPORTED_FILE_ATTRIBUTES", "ownership differs")
	}
	if err := dst.Chmod(a.Mode().Perm()); err != nil {
		return ioError(err)
	}
	return nil
}
