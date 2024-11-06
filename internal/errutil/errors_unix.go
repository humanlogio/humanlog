//go:build !windows

package errutil

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func isErrAddrInUse(errno syscall.Errno) bool {
	return errno == unix.EADDRINUSE
}
