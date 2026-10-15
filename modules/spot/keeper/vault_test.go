package keeper_test

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const paxDenom = "uhpx"

var (
	vaultAddr = common.HexToAddress(types.PECORVaultAddress)
	authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
)

func vaultAccount(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

// e6 is n whole units of a six-decimal token.
func e6(n int64) sdk.Int { return sdk.NewInt(n).MulRaw(1_000_000) }

// vaultEnv is the spot keeper over the test application's bank and EVM
// keepers with PAX, the uhpx native pointer, registered as a NATIVE token.
type vaultEnv struct {
	t   *testing.T
	ctx sdk.Context
	key sdk.StoreKey
	k   *keeper.Keeper
	pax common.Address
}

func newVaultEnv(t *testing.T) *vaultEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(9).WithBlockTime(time.Unix(1_800_000_000, 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	ctx, _ = ctx.CacheContext()
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, nil)
	ctx = launchpadtest.WithStore(ctx, key)

	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Base: paxDenom, Display: "pax", Name: "PAX", Symbol: "PAX",
		DenomUnits: []*banktypes.DenomUnit{{Denom: paxDenom, Exponent: 0}, {Denom: "pax", Exponent: 6}},
	})
	pax, _, ok := app.EvmKeeper.GetERC20NativePointer(ctx, paxDenom)
	if !ok {
		pax = vaultAccount("uhpx pointer")
		require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, paxDenom, pax))
	}
	e := &vaultEnv{t: t, ctx: ctx, key: key, k: k, pax: pax}
	e.register(types.TokenParams{Address: pax, Kind: types.TokenKindNative, Decimals: types.OpeningTokenDecimals,
		MarketID: types.PAXMarketID, MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	return e
}

func (e *vaultEnv) register(token types.TokenParams) []*ethtypes.Log {
	e.t.Helper()
	registered, logs, err := e.k.RegisterToken(e.ctx, token)
	require.NoError(e.t, err)
	params := e.k.GetParams(e.ctx)
	params.Tokens = append(params.Tokens, registered)
	require.NoError(e.t, e.k.UpdateParams(e.ctx, authority, params))
	return logs
}

func (e *vaultEnv) bankAddress(address common.Address) sdk.AccAddress {
	return testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(e.ctx, address)
}

// fund mints amount uhpx to the bank account of address.
func (e *vaultEnv) fund(address common.Address, amount sdk.Int) {
	e.t.Helper()
	bank := testkeeper.EVMTestApp.BankKeeper
	coins := sdk.NewCoins(sdk.NewCoin(paxDenom, amount))
	require.NoError(e.t, bank.MintCoins(e.ctx, "evm", coins))
	require.NoError(e.t, bank.SendCoinsFromModuleToAccount(e.ctx, "evm", e.bankAddress(address), coins))
}

// donate sends amount uhpx straight to the spot custody, outside the vault.
func (e *vaultEnv) donate(amount sdk.Int) {
	e.t.Helper()
	donor := vaultAccount("donor")
	e.fund(donor, amount)
	require.NoError(e.t, testkeeper.EVMTestApp.BankKeeper.SendCoins(e.ctx, e.bankAddress(donor), e.k.CustodyAddress(),
		sdk.NewCoins(sdk.NewCoin(paxDenom, amount))))
}

func (e *vaultEnv) setAmount(key []byte, amount sdk.Int) {
	e.t.Helper()
	bz, err := amount.Marshal()
	require.NoError(e.t, err)
	e.ctx.KVStore(e.key).Set(key, bz)
}

func (e *vaultEnv) setEscrow(token common.Address, amount sdk.Int) {
	e.setAmount(append(append([]byte{}, types.EscrowPrefix...), token.Bytes()...), amount)
}

func (e *vaultEnv) setAccrual(rail types.FeeRail, token common.Address, amount sdk.Int) {
	key := append(append([]byte{}, types.FeeAccrualPrefix...), byte(rail))
	e.setAmount(append(key, token.Bytes()...), amount)
}

func (e *vaultEnv) shares(holder common.Address) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(e.ctx, e.bankAddress(holder), e.k.ShareDenom(e.pax)).Amount
}

func (e *vaultEnv) supply() sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetSupply(e.ctx, e.k.ShareDenom(e.pax)).Amount
}

func (e *vaultEnv) uhpx(holder common.Address) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(e.ctx, e.bankAddress(holder), paxDenom).Amount
}

func (e *vaultEnv) nav() sdk.Int {
	e.t.Helper()
	nav, err := e.k.NAV(e.ctx, e.pax)
	require.NoError(e.t, err)
	return nav
}

func (e *vaultEnv) reserves(token common.Address) sdk.Int {
	e.t.Helper()
	book, err := e.k.GetBook(e.ctx, token)
	require.NoError(e.t, err)
	return book.Reserves
}

func (e *vaultEnv) deposit(depositor common.Address, amount sdk.Int) sdk.Int {
	e.t.Helper()
	e.fund(depositor, amount)
	minted, _, err := e.k.Deposit(e.ctx, depositor, e.pax, amount)
	require.NoError(e.t, err)
	return minted
}

func requireAbiError(t *testing.T, err error, want *types.AbiError) {
	t.Helper()
	require.Error(t, err)
	var got *types.AbiError
	require.True(t, errors.As(err, &got), "%v is not an ABI error", err)
	require.Equal(t, want.Data(), got.Data())
}

func word(value int64) []byte { return common.LeftPadBytes(big.NewInt(value).Bytes(), 32) }

func intWord(value sdk.Int) []byte { return common.LeftPadBytes(value.BigInt().Bytes(), 32) }

func addressTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func requireLog(t *testing.T, log *ethtypes.Log, topic0 string, indexed []common.Address, data ...[]byte) {
	t.Helper()
	require.Equal(t, vaultAddr, log.Address)
	topics := []common.Hash{common.HexToHash(topic0)}
	for _, address := range indexed {
		topics = append(topics, addressTopic(address))
	}
	require.Equal(t, topics, log.Topics)
	var want []byte
	for _, w := range data {
		want = append(want, w...)
	}
	require.Equal(t, want, log.Data)
}

const (
	depositTopic0          = "0xdcbc1c05240f31ff3ad067ef1ee35ce4997762752e3a095284754544f4c709d7"
	lpWithdrawalTopic0     = "0xbacc4c5632df305652cdf9d17913383ae2857b949b623a2ce7b3537458bd0075"
	nativeDepositTopic0    = "0x2d45cf213b0878e6582a0bddc676c202f8f57ed3cd74e8ccf8955ce1359bd427"
	nativeWithdrawalTopic0 = "0x4c556479041161db341af3dc4010b53fa70282739dc38d464ff9628ee9c9d29f"
	reservesSyncTopic0     = "0x055fb2ac115b8a1258ed74cd751869f95f1fb10b2c889147acbaca28ba479e5f"
	reservesUpdatedTopic0  = "0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5"
	withdrawalTopic0       = "0xc2b4a290c20fb28939d29f102514fbffd2b73c059ffba8b78250c94161d5fcc6"
	tokenPauseTopic0       = "0x253b2f7aa4de4ccf23ab8e9c2078ab1cc589649293d26006ecdf802a5d6b451d"
	tokenRegisteredTopic0  = "0x3247537f6efe5e5d3f25242f42522e1bfbabeea906fc7005a128b11bcdb3f99b"
	utilizationCapTopic0   = "0xbcfb6f6e36e3fb105ffb3042d755b572cec796e9c4c634f0bb7ef3c8967331ac"
)

func TestNAV(t *testing.T) {
	e := newVaultEnv(t)
	provider := vaultAccount("nav provider")
	e.deposit(provider, e6(100_000))
	require.Equal(t, e6(100_000), e.nav())

	// Contract example: book 100,000e6, escrow 1,000e6 held in custody
	// (custody 101,000e6) and accrual 500e6 give NAV 99,500e6.
	e.donate(e6(1_000))
	e.setEscrow(e.pax, e6(1_000))
	e.setAccrual(types.FeeRailSwap, e.pax, e6(300))
	e.setAccrual(types.FeeRailOrder, e.pax, e6(200))
	require.Equal(t, e6(100_000), e.reserves(e.pax))
	require.Equal(t, e6(99_500), e.nav())
	spendable, err := e.k.Spendable(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(99_500), spendable)
	untracked, err := e.k.UntrackedFunds(e.ctx, e.pax)
	require.NoError(t, err)
	require.True(t, untracked.IsZero())

	// Custody below the book: the minimum of custody - escrow and book.
	e.setEscrow(e.pax, e6(2_000))
	require.Equal(t, e6(98_500), e.nav())
	// Escrow above custody leaves nothing spendable.
	e.setEscrow(e.pax, e6(200_000))
	require.True(t, e.nav().IsZero())
	e.setEscrow(e.pax, e6(1_000))

	// The utilization cap of an opening token is 9000 bps of spendable.
	cap := e6(99_500).MulRaw(9_000).QuoRaw(10_000)
	ok, err := e.k.HasLiquidity(e.ctx, e.pax, cap)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = e.k.HasLiquidity(e.ctx, e.pax, cap.AddRaw(1))
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = e.k.HasLiquidity(e.ctx, vaultAccount("unregistered"), sdk.OneInt())
	require.NoError(t, err)
	require.False(t, ok)

	// A token without its own cap may use the whole spendable balance.
	require.Equal(t, uint64(types.DefaultMaxUtilizationBps), keeper.MaxUtilizationBps(types.TokenParams{}))
	params := e.k.GetParams(e.ctx)
	params.Tokens[0].MaxUtilizationBps = types.DefaultMaxUtilizationBps
	require.NoError(t, e.k.UpdateParams(e.ctx, authority, params))
	ok, err = e.k.HasLiquidity(e.ctx, e.pax, e6(99_500))
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = e.k.HasLiquidity(e.ctx, e.pax, e6(99_500).AddRaw(1))
	require.NoError(t, err)
	require.False(t, ok)

	// A paused token has no liquidity.
	params.Tokens[0].Paused = true
	require.NoError(t, e.k.UpdateParams(e.ctx, authority, params))
	ok, err = e.k.HasLiquidity(e.ctx, e.pax, sdk.OneInt())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestShareMint(t *testing.T) {
	e := newVaultEnv(t)
	first := vaultAccount("first provider")

	e.fund(first, sdk.NewInt(keeper.MinimumShares))
	_, _, err := e.k.Deposit(e.ctx, first, e.pax, sdk.NewInt(keeper.MinimumShares))
	requireAbiError(t, err, types.ErrZeroAmount)
	_, _, err = e.k.Deposit(e.ctx, first, e.pax, sdk.ZeroInt())
	requireAbiError(t, err, types.ErrZeroAmount)
	_, _, err = e.k.Deposit(e.ctx, first, vaultAccount("unregistered"), sdk.OneInt())
	requireAbiError(t, err, types.ErrTokenNotRegistered)

	// Contract example: an empty pool takes 100,000e6 and mints
	// 99,999,999,000 shares to the depositor and 1000 to the module account.
	e.fund(first, e6(100_000).SubRaw(keeper.MinimumShares))
	minted, logs, err := e.k.Deposit(e.ctx, first, e.pax, e6(100_000))
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(99_999_999_000), minted)
	require.Equal(t, sdk.NewInt(99_999_999_000), e.shares(first))
	require.Equal(t, sdk.NewInt(keeper.MinimumShares), e.shares(e.k.CustodyEVMAddress(e.ctx)))
	require.Equal(t, e6(100_000), e.supply())
	require.True(t, e.uhpx(first).IsZero())
	book, err := e.k.GetBook(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(100_000), book.Reserves)
	require.Equal(t, e6(100_000), book.TotalDeposited)
	require.True(t, book.MinimumSharesLocked)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], reservesUpdatedTopic0, []common.Address{e.pax}, word(0), intWord(e6(100_000)))
	requireLog(t, logs[1], depositTopic0, []common.Address{e.pax, first}, intWord(e6(100_000)), intWord(e6(100_000)))

	// At supply 100,000e6 and NAV 110,000e6 a deposit of 11,000e6 mints 10,000e6.
	e.donate(e6(10_000))
	_, err = e.k.SyncReserves(e.ctx, authority, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(110_000), e.nav())
	second := vaultAccount("second provider")
	require.Equal(t, e6(10_000), e.deposit(second, e6(11_000)))
	require.Equal(t, e6(110_000), e.supply())
	require.Equal(t, e6(121_000), e.reserves(e.pax))

	// A later deposit worth less than one share mints none.
	e.fund(second, sdk.OneInt())
	_, _, err = e.k.Deposit(e.ctx, second, e.pax, sdk.OneInt())
	requireAbiError(t, err, types.ErrZeroAmount)

	// Native value enters at value / 1e12 uhpx.
	e.fund(vaultAddr, e6(1_100))
	value := new(big.Int).Mul(e6(1_100).BigInt(), big.NewInt(1_000_000_000_000))
	minted, logs, err = e.k.DepositNative(e.ctx, second, value)
	require.NoError(t, err)
	require.Equal(t, e6(1_000), minted)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], reservesUpdatedTopic0, []common.Address{e.pax}, intWord(e6(121_000)), intWord(e6(122_100)))
	requireLog(t, logs[1], nativeDepositTopic0, []common.Address{second}, intWord(e6(1_100)), intWord(e6(122_100)))

	_, _, err = e.k.DepositBatch(e.ctx, second, []common.Address{e.pax, e.pax}, []sdk.Int{sdk.OneInt()})
	requireAbiError(t, err, types.ErrInvalidArrayLength)
	_, _, err = e.k.DepositBatch(e.ctx, second, nil, nil)
	requireAbiError(t, err, types.ErrInvalidArrayLength)
	e.fund(second, e6(2_200))
	batch, logs, err := e.k.DepositBatch(e.ctx, second, []common.Address{e.pax, e.pax}, []sdk.Int{e6(1_100), e6(1_100)})
	require.NoError(t, err)
	require.Equal(t, []sdk.Int{e6(1_000), e6(1_000)}, batch)
	require.Len(t, logs, 4)
	requireLog(t, logs[3], depositTopic0, []common.Address{e.pax, second}, intWord(e6(1_100)), intWord(e6(124_300)))

	params := e.k.GetParams(e.ctx)
	params.Tokens[0].Paused = true
	require.NoError(t, e.k.UpdateParams(e.ctx, authority, params))
	e.fund(second, e6(1))
	_, _, err = e.k.Deposit(e.ctx, second, e.pax, e6(1))
	requireAbiError(t, err, types.ErrTokenPaused)
}

func TestShareBurn(t *testing.T) {
	e := newVaultEnv(t)
	provider := vaultAccount("burn provider")
	e.deposit(provider, e6(100_000))
	e.donate(e6(10_000))
	_, err := e.k.SyncReserves(e.ctx, authority, e.pax)
	require.NoError(t, err)
	e.deposit(provider, e6(11_000))
	require.Equal(t, e6(110_000), e.supply())
	require.Equal(t, e6(121_000), e.nav())

	_, _, err = e.k.Withdraw(e.ctx, provider, e.pax, sdk.ZeroInt())
	requireAbiError(t, err, types.ErrZeroAmount)
	// More shares than the supply ask more than the spendable reserves.
	_, _, err = e.k.Withdraw(e.ctx, provider, e.pax, e6(110_001))
	requireAbiError(t, err, types.ErrInsufficientLiquidity)

	// Contract example: at supply 110,000e6 and NAV 121,000e6 withdrawing
	// 10,000e6 shares pays 11,000e6.
	before := e.shares(provider)
	paid, logs, err := e.k.Withdraw(e.ctx, provider, e.pax, e6(10_000))
	require.NoError(t, err)
	require.Equal(t, e6(11_000), paid)
	require.Equal(t, e6(11_000), e.uhpx(provider))
	require.Equal(t, before.Sub(e6(10_000)), e.shares(provider))
	require.Equal(t, e6(100_000), e.supply())
	book, err := e.k.GetBook(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(110_000), book.Reserves)
	require.Equal(t, e6(11_000), book.TotalWithdrawn)
	require.Len(t, logs, 3)
	requireLog(t, logs[0], reservesUpdatedTopic0, []common.Address{e.pax}, intWord(e6(121_000)), intWord(e6(110_000)))
	requireLog(t, logs[1], lpWithdrawalTopic0, []common.Address{e.pax, provider}, intWord(e6(10_000)), intWord(e6(11_000)))
	requireLog(t, logs[2], withdrawalTopic0, []common.Address{e.pax, provider}, intWord(e6(11_000)), intWord(e6(110_000)))

	// Withdraw rounds the payout down: 1 share at 1.1 pays 1.
	paid, _, err = e.k.Withdraw(e.ctx, provider, e.pax, sdk.OneInt())
	require.NoError(t, err)
	require.Equal(t, sdk.OneInt(), paid)

	// withdrawNative burns amount x supply / NAV rounded up.
	e2 := newVaultEnv(t)
	e2.deposit(provider, e6(100_000))
	e2.donate(e6(10_000))
	_, err = e2.k.SyncReserves(e2.ctx, authority, e2.pax)
	require.NoError(t, err)
	recipient := vaultAccount("native recipient")
	_, _, err = e2.k.WithdrawNative(e2.ctx, provider, sdk.OneInt(), common.Address{})
	requireAbiError(t, err, types.ErrZeroAddress)
	_, _, err = e2.k.WithdrawNative(e2.ctx, provider, e6(110_000).AddRaw(1), recipient)
	requireAbiError(t, err, types.ErrInsufficientLiquidity)
	for _, c := range []struct{ amount, burned int64 }{{11, 10}, {12, 11}, {1, 1}} {
		supply, shares := e2.supply(), e2.shares(provider)
		burned, logs, err := e2.k.WithdrawNative(e2.ctx, provider, sdk.NewInt(c.amount), recipient)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(c.burned), burned, "amount %d", c.amount)
		require.Equal(t, supply.SubRaw(c.burned), e2.supply())
		require.Equal(t, shares.SubRaw(c.burned), e2.shares(provider))
		require.Len(t, logs, 2)
		reserves := e2.reserves(e2.pax)
		requireLog(t, logs[1], nativeWithdrawalTopic0, []common.Address{recipient}, word(c.amount), intWord(reserves))
	}
	require.Equal(t, sdk.NewInt(24), e2.uhpx(recipient))
}

func TestTradeUpdatesReserves(t *testing.T) {
	e := newVaultEnv(t)
	e.deposit(vaultAccount("trade provider"), e6(100_000))
	usdl := common.HexToAddress(types.USDLAddress)
	require.NoError(t, e.k.SetBook(e.ctx, types.TokenBook{Token: usdl, Reserves: e6(50_000),
		TotalDeposited: e6(50_000), TotalWithdrawn: sdk.ZeroInt(), Decimals: types.OpeningTokenDecimals}))

	// marketBuy 1,000e6 USDL to PAX at fee 6e6: gross 2,000e6, net 1,994e6;
	// the retained 6e6 stays in the PAX book.
	logs, err := e.k.ApplyTrade(e.ctx, usdl, e6(1_000), e.pax, sdk.NewInt(1_994_000_000))
	require.NoError(t, err)
	require.Equal(t, e6(51_000), e.reserves(usdl))
	require.Equal(t, sdk.NewInt(98_006_000_000), e.reserves(e.pax))
	require.Len(t, logs, 2)
	requireLog(t, logs[0], reservesUpdatedTopic0, []common.Address{usdl}, intWord(e6(50_000)), intWord(e6(51_000)))
	requireLog(t, logs[1], reservesUpdatedTopic0, []common.Address{e.pax}, intWord(e6(100_000)),
		intWord(sdk.NewInt(98_006_000_000)))

	_, err = e.k.ApplyTrade(e.ctx, usdl, e6(1), e.pax, e6(98_007))
	requireAbiError(t, err, types.ErrInsufficientLiquidity)
	_, err = e.k.ApplyTrade(e.ctx, e.pax, e6(1), e.pax, e6(1))
	requireAbiError(t, err, types.ErrSameToken)
	_, err = e.k.ApplyTrade(e.ctx, usdl, sdk.ZeroInt(), e.pax, e6(1))
	requireAbiError(t, err, types.ErrZeroAmount)
	require.Equal(t, e6(51_000), e.reserves(usdl))
	require.Equal(t, sdk.NewInt(98_006_000_000), e.reserves(e.pax))
}

func TestDepositRefusedAtZeroNAV(t *testing.T) {
	e := newVaultEnv(t)
	provider := vaultAccount("zero nav provider")
	e.deposit(provider, e6(100_000))
	// Accruals equal to the book leave NAV 0 with the supply above zero.
	e.setAccrual(types.FeeRailSwap, e.pax, e6(100_000))
	require.True(t, e.nav().IsZero())
	require.Equal(t, e6(100_000), e.supply())

	late := vaultAccount("late provider")
	e.fund(late, e6(5_000))
	_, _, err := e.k.Deposit(e.ctx, late, e.pax, e6(5_000))
	requireAbiError(t, err, types.ErrInsufficientLiquidity)
	require.Equal(t, e6(5_000), e.uhpx(late))

	e.fund(vaultAddr, e6(5_000))
	_, _, err = e.k.DepositNative(e.ctx, late, new(big.Int).Mul(e6(5_000).BigInt(), big.NewInt(1_000_000_000_000)))
	requireAbiError(t, err, types.ErrInsufficientLiquidity)
	require.True(t, e.shares(late).IsZero())
	require.Equal(t, e6(100_000), e.reserves(e.pax))

	// A withdraw at NAV 0 pays nothing.
	_, _, err = e.k.Withdraw(e.ctx, provider, e.pax, e6(1))
	requireAbiError(t, err, types.ErrZeroAmount)
	_, _, err = e.k.WithdrawNative(e.ctx, provider, e6(1), provider)
	requireAbiError(t, err, types.ErrInsufficientLiquidity)
}

func TestDonationDoesNotInflateShares(t *testing.T) {
	e := newVaultEnv(t)
	e.deposit(vaultAccount("honest provider"), e6(100_000))
	e.donate(e6(50_000))

	require.Equal(t, e6(100_000), e.nav())
	untracked, err := e.k.UntrackedFunds(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(50_000), untracked)
	late := vaultAccount("late provider")
	require.Equal(t, e6(10_000), e.deposit(late, e6(10_000)))

	logs, err := e.k.SyncReserves(e.ctx, vaultAccount("not governance").Hex(), e.pax)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Nil(t, logs)
	require.Equal(t, e6(110_000), e.nav())

	logs, err = e.k.SyncReserves(e.ctx, authority, e.pax)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], reservesSyncTopic0, []common.Address{e.pax}, intWord(e6(110_000)), intWord(e6(160_000)),
		intWord(e6(50_000)))
	requireLog(t, logs[1], reservesUpdatedTopic0, []common.Address{e.pax}, intWord(e6(110_000)), intWord(e6(160_000)))
	require.Equal(t, e6(160_000), e.nav())
	book, err := e.k.GetBook(e.ctx, e.pax)
	require.NoError(t, err)
	require.Equal(t, e6(160_000), book.TotalDeposited)

	logs, err = e.k.SyncReserves(e.ctx, authority, e.pax)
	require.NoError(t, err)
	require.Empty(t, logs)

	// A deficit lowers the book to custody - escrow and recovers 0.
	e.setEscrow(e.pax, e6(10_000))
	logs, err = e.k.SyncReserves(e.ctx, authority, e.pax)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], reservesSyncTopic0, []common.Address{e.pax}, intWord(e6(160_000)), intWord(e6(150_000)),
		word(0))
	require.Equal(t, e6(150_000), e.reserves(e.pax))

	_, err = e.k.SyncReserves(e.ctx, authority, vaultAccount("unregistered"))
	requireAbiError(t, err, types.ErrTokenNotRegistered)
}

func TestTokenParamsEmitLogs(t *testing.T) {
	for topic, signature := range map[common.Hash]string{
		keeper.DepositTopic:               depositTopic0,
		keeper.LPWithdrawalTopic:          lpWithdrawalTopic0,
		keeper.NativeDepositTopic:         nativeDepositTopic0,
		keeper.NativeWithdrawalTopic:      nativeWithdrawalTopic0,
		keeper.ReservesSyncTopic:          reservesSyncTopic0,
		keeper.ReservesUpdatedTopic:       reservesUpdatedTopic0,
		keeper.WithdrawalTopic:            withdrawalTopic0,
		keeper.TokenPauseUpdatedTopic:     tokenPauseTopic0,
		keeper.TokenRegisteredTopic:       tokenRegisteredTopic0,
		keeper.UtilizationCapUpdatedTopic: utilizationCapTopic0,
	} {
		require.Equal(t, common.HexToHash(signature), topic)
	}

	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(9)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	ctx, _ = ctx.CacheContext()
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, nil)
	ctx = launchpadtest.WithStore(ctx, key)
	e := &vaultEnv{t: t, ctx: ctx, key: key, k: k}

	const denom = "usidtest"
	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Base: denom, Display: "sidtest", Name: "SIDTEST", Symbol: "SIDTEST",
		DenomUnits: []*banktypes.DenomUnit{{Denom: denom, Exponent: 0}, {Denom: "sidtest", Exponent: 6}},
	})
	sid := vaultAccount("sid test pointer")
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, denom, sid))

	// A NATIVE token whose metadata exponent differs from the pinned decimals.
	_, _, err := k.RegisterToken(ctx, types.TokenParams{Address: sid, Kind: types.TokenKindNative, Decimals: 8,
		MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	requireAbiError(t, err, types.ErrUnsupportedToken)
	// A NATIVE token that is not a native pointer.
	_, _, err = k.RegisterToken(ctx, types.TokenParams{Address: vaultAccount("no pointer"), Kind: types.TokenKindNative,
		MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	requireAbiError(t, err, types.ErrUnsupportedToken)
	// An ERC20 token without code has no decimals().
	_, _, err = k.RegisterToken(ctx, types.TokenParams{Address: vaultAccount("no code"), Kind: types.TokenKindERC20,
		MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	requireAbiError(t, err, types.ErrUnsupportedToken)

	logs := e.register(types.TokenParams{Address: sid, Kind: types.TokenKindNative, Decimals: 6,
		MarketID: types.SIDMarketID, MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	require.Len(t, logs, 2)
	requireLog(t, logs[0], tokenRegisteredTopic0, []common.Address{sid}, word(6), word(0))
	requireLog(t, logs[1], utilizationCapTopic0, []common.Address{sid}, word(types.OpeningTokenMaxUtilizationBps))
	book, err := k.GetBook(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, uint8(6), book.Decimals)
	require.Equal(t, k.ShareDenom(sid), book.ShareDenom)
	require.True(t, book.Reserves.IsZero())

	_, _, err = k.RegisterToken(ctx, types.TokenParams{Address: sid, Kind: types.TokenKindNative,
		MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps})
	requireAbiError(t, err, types.ErrTokenAlreadyRegistered)

	before := k.GetParams(ctx).Tokens
	after := append([]types.TokenParams{}, before...)
	require.Empty(t, keeper.TokenParamLogs(ctx, before, after))

	after[0].Paused = true
	after[0].MaxUtilizationBps = 7_500
	logs = keeper.TokenParamLogs(ctx, before, after)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], tokenPauseTopic0, []common.Address{sid}, word(1))
	requireLog(t, logs[1], utilizationCapTopic0, []common.Address{sid}, word(7_500))

	unpaused := append([]types.TokenParams{}, after...)
	unpaused[0].Paused = false
	logs = keeper.TokenParamLogs(ctx, after, unpaused)
	require.Len(t, logs, 1)
	requireLog(t, logs[0], tokenPauseTopic0, []common.Address{sid}, word(0))
}
