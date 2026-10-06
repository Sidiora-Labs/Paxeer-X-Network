package evmrpc_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/stretchr/testify/require"
)

func TestClientVersion(t *testing.T) {
	w := evmrpc.Web3API{}
	require.NotEmpty(t, w.ClientVersion())
}
