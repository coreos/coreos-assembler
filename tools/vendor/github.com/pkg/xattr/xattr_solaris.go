// +build solaris

package xattr

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// XATTR_SUPPORTED will be true if the current platform is supported
	XATTR_SUPPORTED = true

	XATTR_CREATE  = 0x1
	XATTR_REPLACE = 0x2

	// ENOATTR is not exported by the syscall package on Linux, because it is
	// an alias for ENODATA. We export it here so it is available on all
	// our supported platforms.
	ENOATTR = syscall.ENODATA
)

func getxattr(path string, name string, data []byte) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	return fgetxattr(os.NewFile(uintptr(fd), path), name, data)
}

func lgetxattr(path string, name string, data []byte) (int, error) {
	return 0, unix.ENOTSUP
}

func fgetxattr(f *os.File, name string, data []byte) (int, error) {
	fd, err := unix.Openat(int(f.Fd()), name, unix.O_RDONLY|unix.O_XATTR, 0)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	return unix.Read(fd, data)
}

func setxattr(path string, name string, data []byte, flags int) error {
	fd, err := unix.Open(path, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	if err = fsetxattr(os.NewFile(uintptr(fd), path), name, data, flags); err != nil {
		_ = unix.Close(fd)
		return err
	}
	return unix.Close(fd)
}

func lsetxattr(path string, name string, data []byte, flags int) error {
	return unix.ENOTSUP
}

func fsetxattr(f *os.File, name string, data []byte, flags int) error {
	mode := unix.O_WRONLY | unix.O_XATTR
	if flags&XATTR_REPLACE != 0 {
		mode |= unix.O_TRUNC
	} else if flags&XATTR_CREATE != 0 {
		mode |= unix.O_CREAT | unix.O_EXCL
	} else {
		mode |= unix.O_CREAT | unix.O_TRUNC
	}
	fd, err := unix.Openat(int(f.Fd()), name, mode, 0666)
	if err != nil {
		return err
	}
	if _, err = unix.Write(fd, data); err != nil {
		_ = unix.Close(fd)
		return err
	}
	return unix.Close(fd)
}

func removexattr(path string, name string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_XATTR, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	return fremovexattr(os.NewFile(uintptr(fd), path), name)
}

func lremovexattr(path string, name string) error {
	return unix.ENOTSUP
}

func fremovexattr(f *os.File, name string) error {
	fd, err := unix.Openat(int(f.Fd()), ".", unix.O_XATTR, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	return unix.Unlinkat(fd, name, 0)
}

func listxattr(path string, data []byte) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	return flistxattr(os.NewFile(uintptr(fd), path), data)
}

func llistxattr(path string, data []byte) (int, error) {
	return 0, unix.ENOTSUP
}

func flistxattr(f *os.File, data []byte) (int, error) {
	fd, err := unix.Openat(int(f.Fd()), ".", unix.O_RDONLY|unix.O_XATTR, 0)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = unix.Close(fd)
	}()
	names, err := os.NewFile(uintptr(fd), f.Name()).Readdirnames(-1)
	if err != nil {
		return 0, err
	}
	var buf []byte
	for _, name := range names {
		buf = append(buf, append([]byte(name), '\000')...)
	}
	if data == nil {
		return len(buf), nil
	}
	return copy(data, buf), nil
}

// stringsFromByteSlice converts a sequence of attributes to a []string.
// On Darwin and Linux, each entry is a NULL-terminated string.
func stringsFromByteSlice(buf []byte) (result []string) {
	offset := 0
	for index, b := range buf {
		if b == 0 {
			result = append(result, string(buf[offset:index]))
			offset = index + 1
		}
	}
	return
}
