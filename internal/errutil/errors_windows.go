//go:build windows

package errutil

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func isErrAddrInUse(errno syscall.Errno) bool {
	return errno == windows.WSAEADDRINUSE
}
