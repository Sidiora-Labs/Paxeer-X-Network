package app

import (
	"testing"

	gigaprecompiles "github.com/Sidiora-Labs/Paxeer-X-Network/engine/executor/precompiles"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGigaCustomPrecompilesSwitchAtHeight(t *testing.T) {
	xwebAddr := common.HexToAddress(xweb.XWebAddress)
	before := gigaCustomPrecompiles(gigaLatePrecompileHeight - 1)
	require.NotContains(t, before, xwebAddr)
	require.Len(t, before, len(gigaprecompiles.FailFastPrecompileAddresses))
	for _, addr := range gigaprecompiles.FailFastPrecompileAddresses {
		require.Contains(t, before, addr)
	}
	at := gigaCustomPrecompiles(gigaLatePrecompileHeight)
	require.Len(t, at, len(gigaprecompiles.FailFastPrecompileAddresses)+len(gigaprecompiles.LateFailFastPrecompileAddresses))
	for _, addr := range gigaprecompiles.LateFailFastPrecompileAddresses {
		require.Contains(t, at, addr)
		require.Same(t, gigaprecompiles.FailFastSingleton, at[addr])
	}
	require.Same(t, gigaprecompiles.FailFastSingleton, at[xwebAddr])
	require.Equal(t, at, gigaCustomPrecompiles(gigaLatePrecompileHeight+1))
}
