package launchpad_test

import (
	"os"
	"strings"
	"testing"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	keeperv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	launchpadv610 "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad/legacy/v610"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

type versionedLaunchpadKeepers struct {
	utils.Keepers
	current *launchpadkeeper.Keeper
	legacy  *keeperv610.Keeper
}

func (k versionedLaunchpadKeepers) LaunchpadK() *launchpadkeeper.Keeper { return k.current }
func (k versionedLaunchpadKeepers) LaunchpadV610K() *keeperv610.Keeper  { return k.legacy }

func TestVersionedLaunchpad(t *testing.T) {
	app := testkeeper.EVMTestApp
	key := sdk.NewKVStoreKey(types.StoreKey)
	tokenFactory := tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper)
	keepers := versionedLaunchpadKeepers{
		Keepers: app.GetPrecompileKeepers(),
		current: launchpadkeeper.NewKeeper(key, app.BankKeeper, tokenFactory, &app.EvmKeeper),
		legacy:  keeperv610.NewKeeper(key, app.BankKeeper, tokenFactory, &app.EvmKeeper),
	}
	current, err := launchpad.NewPrecompile(keepers)
	require.NoError(t, err)
	legacy, err := launchpadv610.NewPrecompile(keepers)
	require.NoError(t, err)
	require.NotEqual(t, current, legacy)

	for _, latest := range []string{"v6.12.0", "v7.0", "v6.10"} {
		t.Run(latest, func(t *testing.T) {
			versioned := launchpad.GetVersioned(latest, keepers)
			require.Contains(t, versioned, "v6.12.0")
			require.Contains(t, versioned, "v6.10")
			require.Equal(t, current, versioned["v6.12.0"])
			require.Equal(t, legacy, versioned["v6.10"])
			if latest == "v7.0" {
				require.Len(t, versioned, 3)
				require.Same(t, versioned["v6.12.0"], versioned[latest])
			} else {
				require.Len(t, versioned, 2)
			}
		})
	}

	t.Run("versions", func(t *testing.T) {
		data, err := os.ReadFile("versions")
		require.NoError(t, err)
		require.Equal(t, []string{"v6.5", "v6.12.0"}, strings.Fields(string(data)))
	})
}
