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
	require.Equal(t, gigaV6120FailFast, at)
	for _, addr := range gigaprecompiles.FailFastPrecompileAddresses {
		require.Contains(t, at, addr)
	}
	for _, addr := range gigaprecompiles.LateFailFastPrecompileAddresses {
		require.Contains(t, at, addr)
		require.Same(t, gigaprecompiles.FailFastSingleton, at[addr])
	}
	require.Same(t, gigaprecompiles.FailFastSingleton, at[xwebAddr])
	require.Equal(t, at, gigaCustomPrecompiles(gigaLatePrecompileHeight+1))

	require.Len(t, gigaV6120FailFast, len(gigaprecompiles.AllCustomPrecompilesFailFastLate)+6)
	for addr, contract := range gigaprecompiles.AllCustomPrecompilesFailFastLate {
		require.Contains(t, gigaV6120FailFast, addr)
		require.Same(t, contract, gigaV6120FailFast[addr])
	}
	for _, addr := range []string{
		"0x0000000000000000000000000000000000001015",
		"0x0000000000000000000000000000000000001017",
		"0x0000000000000000000000000000000000001020",
		"0x0000000000000000000000000000000000001021",
		"0x0000000000000000000000000000000000001022",
		"0x0000000000000000000000000000000000001023",
		"0x0000000000000000000000000000000000001024",
		"0x0000000000000000000000000000000000001025",
	} {
		require.Contains(t, gigaV6120FailFast, common.HexToAddress(addr))
		require.Same(t, gigaprecompiles.FailFastSingleton, gigaV6120FailFast[common.HexToAddress(addr)])
	}
	require.NotContains(t, gigaV6120FailFast, common.HexToAddress("0x0000000000000000000000000000000000001026"))
	require.NotContains(t, gigaV6120FailFast, common.HexToAddress("0x0000000000000000000000000000000000001027"))
}
