package keeper_test

import (
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/suite"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/apptesting"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient     types.QueryClient
	bankQueryClient banktypes.QueryClient
	msgServer       types.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom string
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.Setup()

	suite.SetupTokenFactory()

	suite.queryClient = types.NewQueryClient(suite.QueryHelper)
	suite.bankQueryClient = banktypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.TokenFactoryKeeper)
}

func (suite *KeeperTestSuite) CreateDefaultDenom() {
	res, _ := suite.msgServer.CreateDenom(sdk.WrapSDKContext(suite.Ctx), types.NewMsgCreateDenom(suite.TestAccs[0].String(), "bitcoin"))
	suite.defaultDenom = res.GetNewTokenDenom()
}

func (suite *KeeperTestSuite) TestCreateModuleAccount() {
	app := suite.App

	// remove module account
	tokenfactoryModuleAccount := app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	app.AccountKeeper.RemoveAccount(suite.Ctx, tokenfactoryModuleAccount)

	// ensure module account was removed
	suite.Ctx = app.BaseApp.NewContext(false, tmproto.Header{})
	tokenfactoryModuleAccount = app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	suite.Require().Nil(tokenfactoryModuleAccount)

	// create module account
	app.TokenFactoryKeeper.CreateModuleAccount(suite.Ctx)

	// check that the module account is now initialized
	tokenfactoryModuleAccount = app.AccountKeeper.GetAccount(suite.Ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	suite.Require().NotNil(tokenfactoryModuleAccount)
}
