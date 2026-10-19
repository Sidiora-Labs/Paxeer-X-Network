package keeper_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/artifacts"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// shareTokenDenom is the bank denom behind the pool token of the share
// tests: a USDL-like stablecoin of 6 decimals served by its native pointer.
const shareTokenDenom = "ushareusdl"

// shareEnv is the spot keeper on the real bank, EVM and pricefeed keepers of
// the test application, with a registered 6-decimal stablecoin pool token
// whose share token has been created.
type shareEnv struct {
	t       *testing.T
	app     *app.App
	ctx     sdk.Context
	k       *keeper.Keeper
	token   common.Address
	denom   string
	pointer common.Address
}

func shareAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func newShareEnv(t *testing.T) *shareEnv {
	t.Helper()
	testApp := testkeeper.EVMTestApp
	ctx := testApp.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pricefeed, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, testApp.BankKeeper, &testApp.EvmKeeper, pricefeed)
	ctx = launchpadtest.WithStore(ctx, key)
	e := &shareEnv{t: t, app: testApp, ctx: ctx, k: k}

	testApp.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Description: "share test stablecoin",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: shareTokenDenom, Exponent: 0},
			{Denom: "USDL", Exponent: 6},
		},
		Base:    shareTokenDenom,
		Display: "USDL",
		Name:    "USDL",
		Symbol:  "USDL",
	})
	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		token, err := testApp.EvmKeeper.UpsertERCNativePointer(ctx, evm, shareTokenDenom,
			utils.ERCMetadata{Name: "USDL", Symbol: "USDL", Decimals: 6})
		e.token = token
		return err
	})
	registered, _, err := k.RegisterToken(ctx, types.TokenParams{Address: e.token, Kind: types.TokenKindNative,
		IsStablecoin: true, Decimals: 6})
	require.NoError(t, err)
	params := types.DefaultParams()
	params.Tokens = []types.TokenParams{registered}
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	e.denom = k.ShareDenom(e.token)
	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		pointer, err := k.CreateShareToken(ctx, evm, e.token, "USDL", 6)
		e.pointer = pointer
		return err
	})
	return e
}

// inTx runs run as one transaction on a one-off EVM instance over the
// branch of the instance's state and returns the logs it wrote.
func (e *shareEnv) inTx(run func(ctx sdk.Context, evm *vm.EVM) error) []*ethtypes.Log {
	e.t.Helper()
	var logs []*ethtypes.Log
	require.NoError(e.t, e.app.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		db := state.GetDBImpl(evm.StateDB)
		if err := run(db.Ctx(), evm); err != nil {
			return err
		}
		logs = db.GetAllLogs()
		return nil
	}, func(string, string) {}))
	return logs
}

// fund gives holder amount of the pool token denom.
func (e *shareEnv) fund(holder common.Address, amount int64) {
	e.t.Helper()
	coins := sdk.NewCoins(sdk.NewInt64Coin(shareTokenDenom, amount))
	require.NoError(e.t, e.app.BankKeeper.MintCoins(e.ctx, minttypes.ModuleName, coins))
	require.NoError(e.t, e.app.BankKeeper.SendCoinsFromModuleToAccount(e.ctx, minttypes.ModuleName,
		e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), coins))
}

func (e *shareEnv) bankBalance(holder common.Address, denom string) *big.Int {
	return e.app.BankKeeper.GetBalance(e.ctx, e.app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), denom).Amount.BigInt()
}

// pointerView calls a view of the share pointer and returns its one output.
func (e *shareEnv) pointerView(method string, args ...interface{}) interface{} {
	e.t.Helper()
	nativeABI := artifacts.GetParsedABI("native")
	input, err := nativeABI.Pack(method, args...)
	require.NoError(e.t, err)
	ret, err := e.app.EvmKeeper.StaticCallEVM(e.ctx, keeper.CustodyAddress(), &e.pointer, input)
	require.NoError(e.t, err)
	out, err := nativeABI.Unpack(method, ret)
	require.NoError(e.t, err)
	require.Len(e.t, out, 1)
	return out[0]
}

// shareTransfers are the Transfer logs of logs written at the share pointer.
func (e *shareEnv) shareTransfers(logs []*ethtypes.Log) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range logs {
		if log.Address == e.pointer && len(log.Topics) > 0 && log.Topics[0] == keeper.ShareTransferTopic {
			out = append(out, log)
		}
	}
	return out
}

func requireShareTransfer(t *testing.T, log *ethtypes.Log, pointer, from, to common.Address, amount *big.Int) {
	t.Helper()
	require.Equal(t, pointer, log.Address)
	require.Equal(t, []common.Hash{
		common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
		common.BytesToHash(from.Bytes()),
		common.BytesToHash(to.Bytes()),
	}, log.Topics)
	require.Equal(t, common.LeftPadBytes(amount.Bytes(), 32), log.Data)
}

func TestShareDenomAndPointer(t *testing.T) {
	e := newShareEnv(t)
	require.Equal(t, "spot/share/"+strings.ToLower(e.token.Hex()), e.denom)
	require.Equal(t, e.denom, e.k.ShareDenom(e.token))
	require.NoError(t, sdk.ValidateDenom(e.denom))

	pointer, _, exists := e.app.EvmKeeper.GetERC20NativePointer(e.ctx, e.denom)
	require.True(t, exists)
	require.Equal(t, e.pointer, pointer)
	pointee, _, exists := e.app.EvmKeeper.GetNativePointee(e.ctx, e.pointer.Hex())
	require.True(t, exists)
	require.Equal(t, e.denom, pointee)
	require.NotEmpty(t, e.app.EvmKeeper.GetCode(e.ctx, e.pointer))

	metadata, found := e.app.BankKeeper.GetDenomMetaData(e.ctx, e.denom)
	require.True(t, found)
	require.NoError(t, metadata.Validate())
	require.Equal(t, "Spot Vault Share USDL", metadata.Name)
	require.Equal(t, "sUSDL", metadata.Symbol)
	require.Equal(t, e.denom, metadata.Base)
	require.Equal(t, []*banktypes.DenomUnit{{Denom: e.denom, Exponent: 0}, {Denom: "sUSDL", Exponent: 6}},
		metadata.DenomUnits)

	require.Equal(t, "Spot Vault Share USDL", e.pointerView("name"))
	require.Equal(t, "sUSDL", e.pointerView("symbol"))
	require.Equal(t, uint8(6), e.pointerView("decimals"))
	require.Equal(t, 0, big.NewInt(0).Cmp(e.pointerView("totalSupply").(*big.Int)))

	holder := shareAddress("share holder")
	logs := e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		return e.k.MintShares(ctx, evm, e.denom, holder, sdk.NewInt(5_000_000))
	})
	transfers := e.shareTransfers(logs)
	require.Len(t, transfers, 1)
	requireShareTransfer(t, transfers[0], e.pointer, common.Address{}, holder, big.NewInt(5_000_000))
	require.Equal(t, 0, big.NewInt(5_000_000).Cmp(e.pointerView("balanceOf", holder).(*big.Int)))
	require.Equal(t, 0, big.NewInt(5_000_000).Cmp(e.pointerView("totalSupply").(*big.Int)))
	require.Equal(t, 0, big.NewInt(5_000_000).Cmp(e.bankBalance(holder, e.denom)))

	logs = e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		return e.k.BurnShares(ctx, evm, e.denom, holder, sdk.NewInt(2_000_000))
	})
	transfers = e.shareTransfers(logs)
	require.Len(t, transfers, 1)
	requireShareTransfer(t, transfers[0], e.pointer, holder, common.Address{}, big.NewInt(2_000_000))
	require.Equal(t, 0, big.NewInt(3_000_000).Cmp(e.pointerView("balanceOf", holder).(*big.Int)))
	require.Equal(t, 0, big.NewInt(3_000_000).Cmp(e.pointerView("totalSupply").(*big.Int)))

	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		_, err := e.k.CreateShareToken(ctx, evm, e.token, "USDL", 6)
		require.ErrorIs(t, err, types.ErrTokenAlreadyRegistered)
		_, err = e.k.CreateShareToken(ctx, evm, common.Address{}, "USDL", 6)
		require.ErrorIs(t, err, types.ErrZeroAddress)
		_, err = e.k.CreateShareToken(ctx, evm, shareAddress("unnamed token"), " ", 6)
		require.ErrorIs(t, err, types.ErrUnsupportedToken)
		_, err = e.k.CreateShareToken(ctx, evm, shareAddress("wide token"), "WIDE", types.MaxTokenDecimals+1)
		require.ErrorIs(t, err, types.ErrUnsupportedToken)
		return nil
	})
	_, err := e.k.CreateShareToken(e.ctx, nil, shareAddress("other token"), "OTHER", 6)
	require.Error(t, err)
	_, _, exists = e.app.EvmKeeper.GetERC20NativePointer(e.ctx, e.k.ShareDenom(shareAddress("other token")))
	require.False(t, exists)
}

func TestShareSupplyIsBankSupply(t *testing.T) {
	e := newShareEnv(t)
	depositor := shareAddress("share depositor")
	e.fund(depositor, 100_000_000_000)

	var minted sdk.Int
	logs := e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		shares, _, err := e.k.Deposit(keeper.WithRun(ctx, &keeper.Run{EVM: evm, Gas: 30_000_000}), depositor, e.token, sdk.NewInt(100_000_000_000))
		minted = shares
		return err
	})
	require.True(t, minted.Equal(sdk.NewInt(99_999_999_000)), minted.String())

	supply := e.app.BankKeeper.GetSupply(e.ctx, e.denom).Amount.BigInt()
	require.Equal(t, 0, big.NewInt(100_000_000_000).Cmp(supply))
	require.Equal(t, 0, supply.Cmp(e.pointerView("totalSupply").(*big.Int)))
	require.Equal(t, 0, big.NewInt(99_999_999_000).Cmp(e.pointerView("balanceOf", depositor).(*big.Int)))
	require.Equal(t, 0, big.NewInt(keeper.MinimumShares).Cmp(e.pointerView("balanceOf", e.k.CustodyEVMAddress(e.ctx)).(*big.Int)))
	require.Equal(t, 0, big.NewInt(keeper.MinimumShares).Cmp(e.bankBalance(e.k.CustodyEVMAddress(e.ctx), e.denom)))

	transfers := e.shareTransfers(logs)
	require.Len(t, transfers, 2)
	requireShareTransfer(t, transfers[0], e.pointer, common.Address{}, e.k.CustodyEVMAddress(e.ctx),
		big.NewInt(keeper.MinimumShares))
	requireShareTransfer(t, transfers[1], e.pointer, common.Address{}, depositor, big.NewInt(99_999_999_000))
}

func TestTransferredSharesWithdraw(t *testing.T) {
	e := newShareEnv(t)
	alice, bob := shareAddress("share sender"), shareAddress("share receiver")
	e.fund(alice, 100_000_000_000)
	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		_, _, err := e.k.Deposit(keeper.WithRun(ctx, &keeper.Run{EVM: evm, Gas: 30_000_000}), alice, e.token, sdk.NewInt(100_000_000_000))
		return err
	})

	input, err := artifacts.GetParsedABI("native").Pack("transfer", bob, big.NewInt(10_000_000_000))
	require.NoError(t, err)
	_, err = e.app.EvmKeeper.CallEVM(e.ctx, alice, &e.pointer, nil, input)
	require.NoError(t, err)
	require.Equal(t, 0, big.NewInt(10_000_000_000).Cmp(e.pointerView("balanceOf", bob).(*big.Int)))
	require.Equal(t, 0, big.NewInt(89_999_999_000).Cmp(e.pointerView("balanceOf", alice).(*big.Int)))
	require.Equal(t, 0, big.NewInt(0).Cmp(e.bankBalance(bob, shareTokenDenom)))

	var paid sdk.Int
	logs := e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		amount, _, err := e.k.Withdraw(keeper.WithRun(ctx, &keeper.Run{EVM: evm, Gas: 30_000_000}), bob, e.token, sdk.NewInt(10_000_000_000))
		paid = amount
		return err
	})
	require.True(t, paid.Equal(sdk.NewInt(10_000_000_000)), paid.String())
	require.Equal(t, 0, big.NewInt(10_000_000_000).Cmp(e.bankBalance(bob, shareTokenDenom)))
	require.Equal(t, 0, big.NewInt(0).Cmp(e.pointerView("balanceOf", bob).(*big.Int)))
	require.Equal(t, 0, big.NewInt(89_999_999_000).Cmp(e.pointerView("balanceOf", alice).(*big.Int)))
	supply := e.app.BankKeeper.GetSupply(e.ctx, e.denom).Amount.BigInt()
	require.Equal(t, 0, big.NewInt(90_000_000_000).Cmp(supply))
	require.Equal(t, 0, supply.Cmp(e.pointerView("totalSupply").(*big.Int)))

	transfers := e.shareTransfers(logs)
	require.Len(t, transfers, 1)
	requireShareTransfer(t, transfers[0], e.pointer, bob, common.Address{}, big.NewInt(10_000_000_000))

	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		_, _, err := e.k.Withdraw(keeper.WithRun(ctx, &keeper.Run{EVM: evm, Gas: 30_000_000}), bob, e.token, sdk.NewInt(1))
		require.ErrorIs(t, err, types.ErrERC20InsufficientBalance)
		return nil
	})
}

func TestShareMintScope(t *testing.T) {
	e := newShareEnv(t)
	holder := shareAddress("scope holder")
	e.fund(holder, 1_000_000)
	uhpxBefore := e.app.BankKeeper.GetSupply(e.ctx, evmkeeper.BaseDenom)
	tokenBefore := e.app.BankKeeper.GetSupply(e.ctx, shareTokenDenom)

	e.inTx(func(ctx sdk.Context, evm *vm.EVM) error {
		for _, denom := range []string{
			evmkeeper.BaseDenom,
			shareTokenDenom,
			"spot/share/",
			"spot/share/0x1",
			"spot/share/" + e.token.Hex()[2:],
			"spot/share/0x" + strings.ToUpper(e.token.Hex()[2:]),
			e.denom + "/x",
		} {
			require.ErrorIs(t, e.k.MintShares(ctx, evm, denom, holder, sdk.NewInt(1)), types.ErrUnsupportedToken, denom)
			require.ErrorIs(t, e.k.BurnShares(ctx, evm, denom, holder, sdk.NewInt(1)), types.ErrUnsupportedToken, denom)
		}
		unregistered := e.k.ShareDenom(shareAddress("unregistered token"))
		require.ErrorIs(t, e.k.MintShares(ctx, evm, unregistered, holder, sdk.NewInt(1)), types.ErrTokenNotRegistered)
		require.ErrorIs(t, e.k.MintShares(ctx, evm, e.denom, holder, sdk.ZeroInt()), types.ErrZeroAmount)
		require.ErrorIs(t, e.k.MintShares(ctx, evm, e.denom, common.Address{}, sdk.NewInt(1)),
			types.ErrERC20InvalidReceiver)
		require.ErrorIs(t, e.k.BurnShares(ctx, evm, e.denom, common.Address{}, sdk.NewInt(1)),
			types.ErrERC20InvalidSender)
		require.ErrorIs(t, e.k.BurnShares(ctx, evm, e.denom, holder, sdk.NewInt(1)), types.ErrERC20InsufficientBalance)
		require.Empty(t, e.shareTransfers(state.GetDBImpl(evm.StateDB).GetAllLogs()))
		return nil
	})
	require.Error(t, e.k.MintShares(e.ctx, nil, e.denom, holder, sdk.NewInt(1)))

	require.Equal(t, uhpxBefore, e.app.BankKeeper.GetSupply(e.ctx, evmkeeper.BaseDenom))
	require.Equal(t, tokenBefore, e.app.BankKeeper.GetSupply(e.ctx, shareTokenDenom))
	require.True(t, e.app.BankKeeper.GetSupply(e.ctx, e.denom).Amount.IsZero())
	require.Equal(t, 0, big.NewInt(1_000_000).Cmp(e.bankBalance(holder, shareTokenDenom)))
}
