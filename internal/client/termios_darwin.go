//go:build darwin

package client

import (
	"syscall"
	"unsafe"
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

func getTermios(fd int, state *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGETA, uintptr(unsafe.Pointer(state)))
	if errno != 0 {
		return errno
	}
	return nil
}

func setTermios(fd int, state *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSETA, uintptr(unsafe.Pointer(state)))
	if errno != 0 {
		return errno
	}
	return nil
}
