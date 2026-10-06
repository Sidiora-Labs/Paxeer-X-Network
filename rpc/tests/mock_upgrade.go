package tests

import (
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func mockUpgrade(version string, height int64) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(height), version)
	}
}
