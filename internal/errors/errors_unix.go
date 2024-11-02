//go:build !windows

package errors

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func IsSocketInUse(errno syscall.Errno) bool {
	return errno == unix.EADDRINUSE
}
