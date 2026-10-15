package abi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

func TestABIEmbedLoads(t *testing.T) {
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	require.Len(t, parsed.Methods, 67)
	require.Len(t, parsed.Events, 30)
	require.Len(t, parsed.Errors, 44)
	for _, name := range []string{"getBalance", "getBalance0", "getLockedBalance", "getLockedBalance0", "getAccountSummary", "getAccountSummary0", "getMarginMode", "getMarginMode0"} {
		require.Contains(t, parsed.Methods, name)
	}
	paused := parsed.Errors["PerpsPaused"].ID
	require.Equal(t, []byte{0xe0, 0x75, 0x36, 0x33}, paused[:4])
}
