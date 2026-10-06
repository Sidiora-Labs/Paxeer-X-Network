package keeper_test

import (
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// returns context and an app with updated mint keeper
func createTestApp(t *testing.T, isCheckTx bool) (*app.App, sdk.Context) {
	app := app.Setup(t, isCheckTx, false, false)

	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.MintKeeper.SetParams(ctx, types.DefaultParams())
	app.MintKeeper.SetMinter(ctx, types.DefaultInitialMinter())

	return app, ctx
}
