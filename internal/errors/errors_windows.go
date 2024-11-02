//go:build windows

package errors

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func IsSocketInUse(errno syscall.Errno) bool {
	return errno == windows.WSAEADDRINUSE
}
