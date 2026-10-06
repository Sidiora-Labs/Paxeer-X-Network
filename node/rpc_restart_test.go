package app

import (
	"fmt"
	"net"
	"testing"
	"time"

	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func newRestartTestHTTPServer(t *testing.T, port int) *evmrpc.HTTPServer {
	t.Helper()
	srv := evmrpc.NewHTTPServer(rpc.HTTPTimeouts{})
	require.NoError(t, srv.SetListenAddr("127.0.0.1", port))
	require.NoError(t, srv.EnableRPC([]rpc.API{{Namespace: "echo", Service: evmrpc.NewEchoAPI()}}, evmrpc.HTTPConfig{Vhosts: []string{"*"}}))
	return srv
}

func newRestartTestWSServer(t *testing.T, port int) *evmrpc.HTTPServer {
	t.Helper()
	srv := evmrpc.NewHTTPServer(rpc.HTTPTimeouts{})
	require.NoError(t, srv.SetListenAddr("127.0.0.1", port))
	require.NoError(t, srv.EnableWS([]rpc.API{{Namespace: "echo", Service: evmrpc.NewEchoAPI()}}, evmrpc.WsConfig{Origins: []string{"*"}}))
	return srv
}

func portAccepts(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func TestAppCloseStopsEVMRPCServers(t *testing.T) {
	httpPort := freeTCPPort(t)
	wsPort := freeTCPPort(t)

	first := Setup(t, false, false, false)
	first.serveEVMServers(newRestartTestHTTPServer(t, httpPort), newRestartTestWSServer(t, wsPort))
	first.sendEVMServerStartSignals()
	require.Eventually(t, func() bool { return portAccepts(httpPort) }, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { return portAccepts(wsPort) }, 5*time.Second, 20*time.Millisecond)

	require.NoError(t, first.Close())
	require.True(t, portFree(httpPort))
	require.True(t, portFree(wsPort))

	second := Setup(t, false, false, false)
	second.serveEVMServers(newRestartTestHTTPServer(t, httpPort), newRestartTestWSServer(t, wsPort))
	second.sendEVMServerStartSignals()
	require.Eventually(t, func() bool { return portAccepts(httpPort) }, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { return portAccepts(wsPort) }, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, second.Close())
	require.True(t, portFree(httpPort))
	require.True(t, portFree(wsPort))

	third := Setup(t, false, false, false)
	third.serveEVMServers(newRestartTestHTTPServer(t, httpPort), newRestartTestWSServer(t, wsPort))
	require.NoError(t, third.Close())
	third.sendEVMServerStartSignals()
	require.Never(t, func() bool { return portAccepts(httpPort) || portAccepts(wsPort) }, 500*time.Millisecond, 20*time.Millisecond)
	require.True(t, portFree(httpPort))
	require.True(t, portFree(wsPort))
}
