package keeper_test

import (
	gocontext "context"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/keeper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/node"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types" // TODO: Replace this with pax-chain. Leaving it for now otherwise tests fail
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type MintTestSuite struct {
	suite.Suite

	app         *app.App
	ctx         sdk.Context
	queryClient types.QueryClient
}

func (suite *MintTestSuite) SetupTest() {
	app := app.Setup(suite.T(), false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, app.InterfaceRegistry())

	types.RegisterQueryServer(queryHelper, keeper.NewQuerier(app.MintKeeper))
	queryClient := types.NewQueryClient(queryHelper)

	suite.app = app
	suite.ctx = ctx

	suite.queryClient = queryClient
}

func (suite *MintTestSuite) TestGRPCParams() {
	queryClient := suite.queryClient

	_, err := queryClient.Params(gocontext.Background(), &types.QueryParamsRequest{})
	suite.Require().NoError(err)

	_, err = queryClient.Minter(gocontext.Background(), &types.QueryMinterRequest{})
	suite.Require().NoError(err)
}

func TestMintTestSuite(t *testing.T) {
	suite.Run(t, new(MintTestSuite))
}
