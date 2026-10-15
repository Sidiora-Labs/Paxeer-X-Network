package app

import (
	"testing"
	"time"

	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestV6120IsAKnownPlanWithAHandler(t *testing.T) {
	require.Equal(t, "v6.12.0", V6120Upgrade)
	require.Equal(t, []string{ActivationUpgrade, V610Upgrade, V6120Upgrade, MergeUpgrade}, knownUpgradePlans())
	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	require.True(t, a.UpgradeKeeper.HasHandler("v6.12.0"))
	require.False(t, a.UpgradeKeeper.HasHandler("v6.11"))
}

func TestV6120RefusesAnUnboundHook(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx := w.App, w.Ctx
	names := make([]string, 0, 5)
	for _, hook := range a.v6120Hooks() {
		require.Nil(t, hook.run, hook.name)
		names = append(names, hook.name)
	}
	require.Equal(t, []string{"pointers", "launchpad", "pricefeed", "perps", "spot"}, names)

	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	adminBefore, err := a.TokenFactoryKeeper.GetAuthorityMetadata(ctx, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	migratedBefore := a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot())
	require.Equal(t, common.Hash{}, migratedBefore)
	newVM, err := a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, versions)
	require.EqualError(t, err, "upgrade v6.12.0: hook pointers is not bound")
	require.Nil(t, newVM)

	// With every hook but perps bound, the handler names perps and runs none.
	ran := make([]string, 0, 5)
	a.v6120HookTable = make([]v6120Hook, 0, len(names))
	for _, name := range names {
		hook := v6120Hook{name: name}
		if name != "perps" {
			hook.run = func(sdk.Context) error {
				ran = append(ran, name)
				return nil
			}
		}
		a.v6120HookTable = append(a.v6120HookTable, hook)
	}
	newVM, err = a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, versions)
	require.EqualError(t, err, "upgrade v6.12.0: hook perps is not bound")
	require.Nil(t, newVM)
	require.Empty(t, ran)

	// No state was written: the version map, the usid authority and the SID
	// migration record are as the handler found them.
	require.Equal(t, versions, a.UpgradeKeeper.GetModuleVersionMap(ctx))
	admin, err := a.TokenFactoryKeeper.GetAuthorityMetadata(ctx, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	require.Equal(t, adminBefore, admin)
	require.Equal(t, migratedBefore, a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()))
}
