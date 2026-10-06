package precompiles_test

import (
	"testing"

	gigaprecompiles "github.com/Sidiora-Labs/Paxeer-X-Network/engine/executor/precompiles"
	gigautils "github.com/Sidiora-Labs/Paxeer-X-Network/engine/executor/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestSelfDestructAbortError(t *testing.T) {
	abortErr, ok := gigaprecompiles.ErrSelfDestructUnsupported.(vm.AbortError)
	require.True(t, ok, "ErrSelfDestructUnsupported must implement vm.AbortError")
	require.True(t, abortErr.IsAbortError())
	require.NotEmpty(t, gigaprecompiles.ErrSelfDestructUnsupported.Error())

	// ShouldExecutionAbort must recognize it so the giga executor falls back to v2.
	require.True(t, gigautils.ShouldExecutionAbort(gigaprecompiles.ErrSelfDestructUnsupported))
}

func TestLateFailFastPrecompiles(t *testing.T) {
	late := gigaprecompiles.LateFailFastPrecompileAddresses
	require.Len(t, late, 5)
	for _, addr := range gigaprecompiles.FailFastPrecompileAddresses {
		require.Same(t, gigaprecompiles.FailFastSingleton, gigaprecompiles.AllCustomPrecompilesFailFast[addr])
		require.Same(t, gigaprecompiles.FailFastSingleton, gigaprecompiles.AllCustomPrecompilesFailFastLate[addr])
		require.NotContains(t, late, addr)
	}
	for _, addr := range late {
		require.Same(t, gigaprecompiles.FailFastSingleton, gigaprecompiles.AllCustomPrecompilesFailFastLate[addr])
		require.NotContains(t, gigaprecompiles.AllCustomPrecompilesFailFast, addr)
	}
	require.Len(t, gigaprecompiles.AllCustomPrecompilesFailFastLate, len(gigaprecompiles.FailFastPrecompileAddresses)+len(late))
	for _, hex := range []string{
		"0x0000000000000000000000000000000000001015",
		"0x0000000000000000000000000000000000001016",
		"0x0000000000000000000000000000000000001017",
		"0x0000000000000000000000000000000000001018",
		"0x0000000000000000000000000000000000001019",
	} {
		require.Contains(t, late, common.HexToAddress(hex))
	}
	_, err := gigaprecompiles.FailFastSingleton.Run(nil, common.Address{}, common.Address{}, nil, nil, false, false, nil)
	require.True(t, gigautils.ShouldExecutionAbort(err))
}
