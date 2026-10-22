package app

import (
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/eip712"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestMergePrecompileGate(t *testing.T) {
	wrapper := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := wrapper.App, wrapper.Ctx
	contract, err := eip712.NewPrecompile(&a.UpgradeKeeper, MergeUpgrade, 256, 256)
	require.NoError(t, err)
	all := make(map[common.Address]putils.VersionedPrecompiles, len(precompiles.MergeAddressTable))
	for _, entry := range precompiles.MergeAddressTable {
		all[entry.Address] = putils.VersionedPrecompiles{LatestUpgrade: contract, MergeUpgrade: contract}
	}
	a.EvmKeeper.SetCustomPrecompiles(all, LatestUpgrade)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, MergeUpgrade))
	for _, tracing := range []bool{false, true} {
		before := ctx.WithBlockHeight(99).WithIsTracing(tracing)
		for _, entry := range precompiles.MergeAddressTable {
			require.NotContains(t, a.EvmKeeper.CustomPrecompiles(before), entry.Address)
			require.NotContains(t, a.EvmKeeper.GetCustomPrecompilesVersions(before), entry.Address)
		}
	}
	a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(100), MergeUpgrade)
	for _, tracing := range []bool{false, true} {
		for _, height := range []int64{99, 100, 101} {
			at := ctx.WithBlockHeight(height).WithIsTracing(tracing)
			served := a.EvmKeeper.CustomPrecompiles(at)
			for _, entry := range precompiles.MergeAddressTable {
				if height < 100 {
					require.NotContains(t, served, entry.Address)
				} else {
					require.Same(t, contract, served[entry.Address])
				}
			}
		}
	}
}
