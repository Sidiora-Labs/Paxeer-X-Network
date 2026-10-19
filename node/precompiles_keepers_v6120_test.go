package app

import (
	"testing"

	perpsprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	pricefeedprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	spotprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/stretchr/testify/require"
)

func TestPrecompileKeepersV6120Accessors(t *testing.T) {
	a := Setup(t, false, false, false)
	keepers := a.GetPrecompileKeepers()

	perps, ok := keepers.(perpsprecompile.PerpsKeepers)
	require.True(t, ok)
	require.NotNil(t, perps.PerpsK())
	require.Same(t, a.PerpsKeeper, perps.PerpsK())

	spot, ok := keepers.(spotprecompile.SpotKeepers)
	require.True(t, ok)
	require.NotNil(t, spot.SpotK())
	require.Same(t, a.SpotKeeper, spot.SpotK())

	priceFeed, ok := keepers.(putils.PriceFeedKeepers)
	require.True(t, ok)
	require.NotNil(t, priceFeed.PriceFeedK())
	require.Same(t, &a.PricefeedKeeper, priceFeed.PriceFeedK())

	precompilePriceFeed, ok := keepers.(pricefeedprecompile.PriceFeedKeepers)
	require.True(t, ok)
	require.Same(t, &a.PricefeedKeeper, precompilePriceFeed.PriceFeedK())
}
