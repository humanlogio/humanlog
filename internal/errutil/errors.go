package errutil

import (
	"net"
	"os"
	"syscall"
)

func IsEADDRINUSE(err error) bool {
	nerr, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	nserr, ok := nerr.Err.(*os.SyscallError)
	if !ok {
		return false
	}
	if nserr.Syscall != "bind" {
		return false
	}
	nserrno, ok := nserr.Err.(syscall.Errno)
	if !ok {
		return false
	}
	return isErrAddrInUse(nserrno)
}
