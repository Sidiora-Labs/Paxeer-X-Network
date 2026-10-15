package keeper_test

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// evmLogsSince is the EVM logs emitted on ctx after the first before of them.
func evmLogsSince(ctx sdk.Context, before int) []*ethtypes.Log {
	return ctx.EVMEventManager().Events()[before:]
}

func requireLog(t *testing.T, l *ethtypes.Log, address common.Address, topics []common.Hash, words ...*big.Int) {
	t.Helper()
	require.Equal(t, address, l.Address)
	require.Equal(t, topics, l.Topics)
	var data []byte
	for _, w := range words {
		data = append(data, word(w)...)
	}
	require.Equal(t, data, l.Data)
}

func TestUpdateParamsEmitsConfigUpdated(t *testing.T) {
	e := newEnv(t)
	server := keeper.NewMsgServerImpl(e.k)
	authority := e.k.Authority()
	e.ctx = e.ctx.WithBlockHeight(77)
	params := e.k.GetParams(e.ctx)
	require.Equal(t, uint64(30), params.BaseFeeBps)
	params.BaseFeeBps = 40
	msg := &types.MsgUpdateParams{Authority: authority, Params: params}
	require.NoError(t, msg.ValidateBasic())

	outsider, _ := testkeeper.MockAddressPair()
	before := len(e.ctx.EVMEventManager().Events())
	_, err := server.UpdateParams(sdk.WrapSDKContext(e.ctx), &types.MsgUpdateParams{Authority: outsider.String(), Params: params})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.Equal(t, uint64(30), e.k.GetParams(e.ctx).BaseFeeBps)
	require.Empty(t, evmLogsSince(e.ctx, before))

	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), msg)
	require.NoError(t, err)
	require.Equal(t, uint64(40), e.k.GetParams(e.ctx).BaseFeeBps)
	logs := evmLogsSince(e.ctx, before)
	require.Len(t, logs, 2)
	launchpad := common.HexToAddress(types.LaunchpadAddress)
	key := crypto.Keccak256Hash([]byte("baseFeeBps"))
	require.Equal(t, common.HexToHash("0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e"),
		keeper.ProtocolConfigUpdatedTopic)
	require.Equal(t, common.HexToHash("0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e"),
		keeper.EventEmitterConfigTopic)
	requireLog(t, logs[0], launchpad, []common.Hash{keeper.ProtocolConfigUpdatedTopic, key}, b(30), b(40))
	requireLog(t, logs[1], launchpad, []common.Hash{keeper.EventEmitterConfigTopic, key}, b(30), b(40),
		b(e.ctx.BlockTime().Unix()), b(77))

	before = len(e.ctx.EVMEventManager().Events())
	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), msg)
	require.NoError(t, err)
	require.Empty(t, evmLogsSince(e.ctx, before))

	params.CreationFee = i(9)
	params.ProtocolFeeBps = 2_000
	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), &types.MsgUpdateParams{Authority: authority, Params: params})
	require.NoError(t, err)
	logs = evmLogsSince(e.ctx, before)
	require.Len(t, logs, 4)
	require.Equal(t, crypto.Keccak256Hash([]byte("protocolFeeBps")), logs[0].Topics[1])
	require.Equal(t, crypto.Keccak256Hash([]byte("creationFee")), logs[2].Topics[1])
	requireLog(t, logs[2], launchpad, []common.Hash{keeper.ProtocolConfigUpdatedTopic, logs[2].Topics[1]},
		types.DefaultParams().CreationFee.BigInt(), b(9))
}

func TestRoleAdministrationByGovernance(t *testing.T) {
	e := newEnv(t)
	server := keeper.NewMsgServerImpl(e.k)
	authority := e.k.Authority()
	accumulator := common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")
	require.Equal(t, keeper.Generation2Surfaces[types.RoleFeeAccumulator], accumulator)
	require.NoError(t, e.k.SetSurface(e.ctx, accumulator, keeper.Surface{Role: types.RoleFeeAccumulator,
		Generation: types.Generation2}))
	poolRole := crypto.Keccak256Hash([]byte("POOL_ROLE"))
	account := address("account A")
	sender := testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(e.ctx, sdk.MustAccAddressFromBech32(authority))
	assignment := types.RoleAssignment{Contract: accumulator.Hex(), Role: poolRole.Hex(), Account: account.Hex()}
	params := e.k.GetParams(e.ctx)
	grant := &types.MsgUpdateParams{Authority: authority, Params: params, RoleGrants: []types.RoleAssignment{assignment}}
	revoke := &types.MsgUpdateParams{Authority: authority, Params: params, RoleRevokes: []types.RoleAssignment{assignment}}
	require.NoError(t, grant.ValidateBasic())
	require.NoError(t, revoke.ValidateBasic())
	require.Equal(t, common.HexToHash("0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d"),
		keeper.RoleGrantedTopic)
	require.Equal(t, common.HexToHash("0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b"),
		keeper.RoleRevokedTopic)

	before := len(e.ctx.EVMEventManager().Events())
	_, err := server.UpdateParams(sdk.WrapSDKContext(e.ctx), grant)
	require.NoError(t, err)
	require.True(t, e.k.HasRole(e.ctx, accumulator, poolRole, account))
	logs := evmLogsSince(e.ctx, before)
	require.Len(t, logs, 1)
	requireLog(t, logs[0], accumulator, []common.Hash{keeper.RoleGrantedTopic, poolRole,
		common.BytesToHash(account.Bytes()), common.BytesToHash(sender.Bytes())})

	before = len(e.ctx.EVMEventManager().Events())
	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), grant)
	require.NoError(t, err)
	require.Empty(t, evmLogsSince(e.ctx, before))

	exported := e.k.ExportGenesis(e.ctx)
	require.NoError(t, exported.Validate())
	require.Equal(t, []types.Surface{{Address: accumulator.Hex(), Role: types.RoleFeeAccumulator}}, exported.Surfaces)
	require.Equal(t, []types.RoleMember{{Contract: accumulator.Hex(), Role: poolRole.Hex(), Account: account.Hex()}},
		exported.RoleMembers)
	fresh, ctx := launchpadtest.NewKeeper(testkeeper.EVMTestApp, e.ctx)
	fresh.InitGenesis(ctx, *exported)
	require.True(t, fresh.HasRole(ctx, accumulator, poolRole, account))
	surface, found, err := fresh.GetSurface(ctx, accumulator)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, keeper.Surface{Role: types.RoleFeeAccumulator, Generation: types.Generation2}, surface)
	sameJSON(t, exported, fresh.ExportGenesis(ctx))

	treasury := keeper.Generation2Surfaces[types.RoleTreasury]
	stray := &types.MsgUpdateParams{Authority: authority, Params: params, RoleGrants: []types.RoleAssignment{
		{Contract: treasury.Hex(), Role: poolRole.Hex(), Account: account.Hex()}}}
	before = len(e.ctx.EVMEventManager().Events())
	params.BaseFeeBps = 40
	stray.Params = params
	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), stray)
	require.Error(t, err)
	require.Equal(t, uint64(30), e.k.GetParams(e.ctx).BaseFeeBps)
	require.False(t, e.k.HasRole(e.ctx, treasury, poolRole, account))
	require.Empty(t, evmLogsSince(e.ctx, before))

	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), revoke)
	require.NoError(t, err)
	require.False(t, e.k.HasRole(e.ctx, accumulator, poolRole, account))
	logs = evmLogsSince(e.ctx, before)
	require.Len(t, logs, 1)
	requireLog(t, logs[0], accumulator, []common.Hash{keeper.RoleRevokedTopic, poolRole,
		common.BytesToHash(account.Bytes()), common.BytesToHash(sender.Bytes())})

	before = len(e.ctx.EVMEventManager().Events())
	_, err = server.UpdateParams(sdk.WrapSDKContext(e.ctx), revoke)
	require.NoError(t, err)
	require.Empty(t, evmLogsSince(e.ctx, before))
}

func TestTreasuryWithdrawByGovernance(t *testing.T) {
	c := newCustodyEnv(t)
	server := keeper.NewMsgServerImpl(c.k)
	authority := c.k.Authority()
	treasury := common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723")
	require.Equal(t, treasury, c.k.TreasuryContract())
	c.transfer(c.holder, c.custody, 1200)
	gs := c.k.ExportGenesis(c.ctx)
	gs.Treasury = []types.TreasuryBalance{{Token: c.usdl.Hex(), Ledger: i(1000), Held: i(1200)}}
	require.NoError(t, gs.Validate())
	c.k.InitGenesis(c.ctx, *gs)
	treasuryState := func() []types.TreasuryBalance { return c.k.ExportGenesis(c.ctx).Treasury }
	require.Equal(t, gs.Treasury, treasuryState())

	account := address("account A")
	withdraw := func(from string, amount int64, surplus bool) error {
		msg := &types.MsgTreasuryWithdraw{Authority: from, Token: c.usdl.Hex(), To: account.Hex(), Amount: i(amount),
			Surplus: surplus}
		_, err := server.TreasuryWithdraw(sdk.WrapSDKContext(c.ctx), msg)
		return err
	}
	require.Equal(t, common.HexToHash("0xd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb"),
		keeper.WithdrawnTopic)
	require.Equal(t, common.HexToHash("0x00c571bcd43634ce4043e2bd7088ab9a1d36bd5dc21f5f4246e9eddda3c222f6"),
		keeper.SurplusRecoveredTopic)
	topics := func(topic common.Hash) []common.Hash {
		return []common.Hash{topic, common.BytesToHash(c.usdl.Bytes()), common.BytesToHash(account.Bytes())}
	}

	outsider, _ := testkeeper.MockAddressPair()
	before := len(c.ctx.EVMEventManager().Events())
	require.ErrorIs(t, withdraw(outsider.String(), 1000, false), types.ErrUnauthorized)
	require.Empty(t, evmLogsSince(c.ctx, before))
	require.Error(t, (&types.MsgTreasuryWithdraw{Authority: authority, Token: c.usdl.Hex(), To: account.Hex(),
		Amount: i(0)}).ValidateBasic())

	require.NoError(t, withdraw(authority, 1000, false))
	require.Equal(t, int64(1000), c.balanceOf(account))
	require.Equal(t, int64(200), c.balanceOf(c.custody))
	require.Equal(t, []types.TreasuryBalance{{Token: c.usdl.Hex(), Ledger: i(0), Held: i(200)}}, treasuryState())
	logs := evmLogsSince(c.ctx, before)
	require.Len(t, logs, 1)
	requireLog(t, logs[0], treasury, topics(keeper.WithdrawnTopic), b(1000))

	before = len(c.ctx.EVMEventManager().Events())
	require.NoError(t, withdraw(authority, 200, true))
	require.Equal(t, int64(1200), c.balanceOf(account))
	require.Equal(t, int64(0), c.balanceOf(c.custody))
	require.Equal(t, []types.TreasuryBalance{{Token: c.usdl.Hex(), Ledger: i(0), Held: i(0)}}, treasuryState())
	logs = evmLogsSince(c.ctx, before)
	require.Len(t, logs, 1)
	requireLog(t, logs[0], treasury, topics(keeper.SurplusRecoveredTopic), b(200))

	before = len(c.ctx.EVMEventManager().Events())
	require.ErrorIs(t, withdraw(authority, 1, false), types.ErrInsufficientBalance)
	require.Equal(t, int64(1200), c.balanceOf(account))
	require.Empty(t, evmLogsSince(c.ctx, before))
}
