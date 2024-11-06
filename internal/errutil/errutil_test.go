package errutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSocketInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)

	p := addr.Port
	t.Logf("port: %d", p)

	_, err = net.Listen("tcp", addr.String())
	require.NotNil(t, err)
	require.True(t, IsEADDRINUSE(err), "err should be EADDRINUSE but was (%T) %#v", err, err)
}
