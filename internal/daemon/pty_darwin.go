//go:build darwin

package daemon

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

const (
	TIOCPTYGNAME = 0x40807453
	TIOCPTYGRANT = 0x20007454
	TIOCPTYUNLK  = 0x20007452
)

func unsafePointer(v interface{}) unsafe.Pointer {
	switch val := v.(type) {
	case *struct {
		Rows    uint16
		Cols    uint16
		Xpixels uint16
		Ypixels uint16
	}:
		return unsafe.Pointer(val)
	default:
		return nil
	}
}

func ptsname(f *os.File) (string, error) {
	var buf [128]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), TIOCPTYGNAME, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", errno
	}
	n := bytes.IndexByte(buf[:], 0)
	if n == -1 {
		n = len(buf)
	}
	return string(buf[:n]), nil
}

func grantpt(f *os.File) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), TIOCPTYGRANT, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func unlockpt(f *os.File) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), TIOCPTYUNLK, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
