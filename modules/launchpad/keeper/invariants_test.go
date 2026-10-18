package keeper_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// deployToken deploys a SidioraERC20 pool token whose supply is minted to
// holder.
func (c *custodyEnv) deployToken(name, symbol string, supply int64, holder common.Address) common.Address {
	c.t.Helper()
	evm := &testkeeper.EVMTestApp.EvmKeeper
	deployer := address("pool token deployer")
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(c.t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(c.t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(c.t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack(name, symbol, big.NewInt(supply), holder)
	require.NoError(c.t, err)
	nonce := evm.GetNonce(c.ctx, deployer)
	_, err = evm.CallEVM(c.ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(c.t, err)
	token := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(c.t, evm.GetCode(c.ctx, token))
	return token
}

// sendToken runs token.transfer(to, amount) sent by from.
func (c *custodyEnv) sendToken(token, from, to common.Address, amount int64) {
	c.t.Helper()
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(to.Bytes(), 32)...)
	input = append(input, word(big.NewInt(amount))...)
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(c.ctx, from, &token, nil, input)
	require.NoError(c.t, err)
	require.Equal(c.t, word(big.NewInt(1)), ret)
}

func (c *custodyEnv) move(from, to keeper.QuoteLedger, amount int64) {
	c.t.Helper()
	require.NoError(c.t, c.k.MoveQuote(c.ctx, from, to, sdk.NewInt(amount)))
}

// holds runs invariant and requires it to report no broken ledger.
func (c *custodyEnv) holds(name string, invariant sdk.Invariant) {
	c.t.Helper()
	message, broken := invariant(c.ctx)
	require.False(c.t, broken, message)
	require.Equal(c.t, sdk.FormatInvariant(types.ModuleName, name, ""), message)
}

// breaks runs invariant and requires it to report exactly lines.
func (c *custodyEnv) breaks(name string, invariant sdk.Invariant, lines string) {
	c.t.Helper()
	message, broken := invariant(c.ctx)
	require.True(c.t, broken)
	require.Equal(c.t, sdk.FormatInvariant(types.ModuleName, name, lines), message)
}

func TestSolvencyPerPool(t *testing.T) {
	c := newCustodyEnv(t)
	solvency := keeper.SolvencyInvariant(c.k)
	pool, x := address("pool P1"), address("trader X")
	token := c.deployToken("Kindle Token", "KNDL", 1_000_000, pool)

	market := generation2Market(c.k.Generation2().SidioraNFT, 1, sdk.AccAddress(c.holder.Bytes()))
	market.PoolAddress, market.TokenAddress = pool.Hex(), token.Hex()
	market.TotalSupply, market.TokenReserve, market.RealQuoteBalance = i(1_000_000), i(1_000_000), i(5_000_000)
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	gs.Markets = []types.Market{market}
	c.k.InitGenesis(c.ctx, *gs)

	held, fees := keeper.HeldQuoteLedger(pool), keeper.FeeHeldLedger(c.k.Generation2().FeeAccumulator)
	c.approve(c.holder, 5_000_000)
	c.pull(c.holder, held, 5_000_000)
	c.holds(keeper.SolvencyInvariantName, solvency)

	// realUsdlBalance one above HeldQuote(pool).
	c.move(held, fees, 1)
	require.Equal(t, int64(4_999_999), c.ledger(held))
	c.breaks(keeper.SolvencyInvariantName, solvency,
		fmt.Sprintf("pool %s market %s: held quote 4999999 below real quote balance 5000000\n", pool.Hex(), market.Denom))
	c.move(fees, held, 1)
	c.holds(keeper.SolvencyInvariantName, solvency)

	// USDL at the pool address is not HeldQuote until swept.
	c.transfer(c.holder, pool, 7)
	c.holds(keeper.SolvencyInvariantName, solvency)

	// tokenReserve one above token.balanceOf(pool).
	c.sendToken(token, pool, x, 1)
	c.breaks(keeper.SolvencyInvariantName, solvency,
		fmt.Sprintf("pool %s market %s: token balance 999999 below reserve 1000000\n", pool.Hex(), market.Denom))

	// Both checks fail together and each is reported.
	c.move(held, fees, 2)
	c.breaks(keeper.SolvencyInvariantName, solvency,
		fmt.Sprintf("pool %s market %s: held quote 4999998 below real quote balance 5000000\n", pool.Hex(), market.Denom)+
			fmt.Sprintf("pool %s market %s: token balance 999999 below reserve 1000000\n", pool.Hex(), market.Denom))
	c.move(fees, held, 2)
	c.sendToken(token, x, pool, 1)
	c.holds(keeper.SolvencyInvariantName, solvency)

	// A pool token without code cannot prove the reserve.
	codeless := address("codeless pool token")
	market.TokenAddress = codeless.Hex()
	gs.Markets = []types.Market{market}
	c.k.InitGenesis(c.ctx, *gs)
	message, broken := solvency(c.ctx)
	require.True(t, broken)
	require.Contains(t, message, fmt.Sprintf("pool %s market %s: token balance unreadable: ", pool.Hex(), market.Denom))
	require.NotContains(t, message, "held quote")
}

func TestLiabilitiesBacked(t *testing.T) {
	c := newCustodyEnv(t)
	liabilities, custody := keeper.LiabilitiesInvariant(c.k), keeper.CustodyInvariant(c.k)
	accumulator := c.k.Generation2().FeeAccumulator
	pool, other := address("pool P1"), address("other treasury token")
	held, fees, treasury := keeper.HeldQuoteLedger(pool), keeper.FeeHeldLedger(accumulator), keeper.TreasuryHeldLedger()

	c.approve(c.holder, 2_200)
	c.pull(c.holder, fees, 1_000)
	c.pull(c.holder, treasury, 1_200)
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	gs.FeeAccounting.TotalLiabilities = i(1_000)
	gs.FeeHeld = i(1_000)
	gs.Treasury = []types.TreasuryBalance{
		{Token: c.usdl.Hex(), Ledger: i(1_000), Held: i(1_200)},
		{Token: other.Hex(), Ledger: i(7), Held: i(7)},
	}
	c.k.InitGenesis(c.ctx, *gs)

	total, err := c.k.TotalLiabilities(c.ctx)
	require.NoError(t, err)
	require.Equal(t, "1000", total.String())
	require.Equal(t, int64(1_000), c.ledger(fees))
	require.Equal(t, int64(1_200), c.ledger(treasury))
	c.holds(keeper.LiabilitiesInvariantName, liabilities)
	c.holds(keeper.CustodyInvariantName, custody)

	// totalLiabilities one above FeeHeld.
	c.move(fees, held, 1)
	c.breaks(keeper.LiabilitiesInvariantName, liabilities, "fee held 999 below total liabilities 1000\n")
	c.holds(keeper.CustodyInvariantName, custody)
	c.move(held, fees, 1)
	c.holds(keeper.LiabilitiesInvariantName, liabilities)

	// The Treasury surplus may leave; TreasuryLedger(USDL) one above
	// TreasuryHeld(USDL) may not.
	c.move(treasury, held, 200)
	c.holds(keeper.LiabilitiesInvariantName, liabilities)
	c.move(treasury, held, 1)
	c.breaks(keeper.LiabilitiesInvariantName, liabilities,
		fmt.Sprintf("treasury %s: held 999 below ledger 1000\n", c.usdl.Hex()))

	// Both ledgers uncovered are both reported.
	c.move(fees, held, 1)
	c.breaks(keeper.LiabilitiesInvariantName, liabilities,
		"fee held 999 below total liabilities 1000\n"+fmt.Sprintf("treasury %s: held 999 below ledger 1000\n", c.usdl.Hex()))
	c.holds(keeper.CustodyInvariantName, custody)
}

func TestCustodyInvariantNonQuoteTreasury(t *testing.T) {
	c := newCustodyEnv(t)
	other := address("other treasury token")
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	gs.Treasury = []types.TreasuryBalance{{Token: other.Hex(), Ledger: i(7), Held: i(7)}}
	c.k.InitGenesis(c.ctx, *gs)
	require.Zero(t, c.balanceOf(c.custody))
	c.holds(keeper.CustodyInvariantName, keeper.CustodyInvariant(c.k))
	c.holds(keeper.LiabilitiesInvariantName, keeper.LiabilitiesInvariant(c.k))

	amount, err := i(8).Marshal()
	require.NoError(t, err)
	var key sdk.StoreKey
	for _, registered := range c.ctx.MultiStore().StoreKeys() {
		if registered.Name() == types.StoreKey {
			key = registered
			break
		}
	}
	require.NotNil(t, key)
	c.ctx.KVStore(key).Set(types.TreasuryLedgerKey(other), amount)
	c.breaks(keeper.LiabilitiesInvariantName, keeper.LiabilitiesInvariant(c.k),
		fmt.Sprintf("treasury %s: held 7 below ledger 8\n", other.Hex()))
	c.holds(keeper.CustodyInvariantName, keeper.CustodyInvariant(c.k))
}

func TestCustodyInvariant(t *testing.T) {
	c := newCustodyEnv(t)
	custody := keeper.CustodyInvariant(c.k)
	p1, p2, x := address("pool P1"), address("pool P2"), address("recipient X")
	accumulator := c.k.Generation2().FeeAccumulator
	held1, held2 := keeper.HeldQuoteLedger(p1), keeper.HeldQuoteLedger(p2)
	fees, treasury := keeper.FeeHeldLedger(accumulator), keeper.TreasuryHeldLedger()
	c.holds(keeper.CustodyInvariantName, custody)

	c.approve(c.holder, 6_030_000)
	c.pull(c.holder, held1, 4_000_000)
	c.pull(c.holder, held2, 1_000_000)
	c.pull(c.holder, fees, 30_000)
	c.pull(c.holder, treasury, 1_000_000)
	require.Equal(t, int64(6_030_000), c.balanceOf(c.custody))
	c.holds(keeper.CustodyInvariantName, custody)

	// Ledger moves, sweeps and payouts keep the custody balance covering
	// the ledgers.
	c.move(fees, treasury, 3_000)
	c.move(held1, fees, 6_000)
	c.holds(keeper.CustodyInvariantName, custody)
	c.transfer(c.holder, p2, 500)
	ctx, swept, err := c.k.SweepPool(c.ctx, c.caller, p2)
	require.NoError(t, err)
	c.ctx = ctx
	require.Equal(t, "500", swept.String())
	c.pay(held1, x, 100_000)
	c.holds(keeper.CustodyInvariantName, custody)
	require.Equal(t, int64(5_930_500), c.balanceOf(c.custody))

	// USDL sent straight to custody is surplus.
	c.transfer(c.holder, c.custody, 10)
	c.holds(keeper.CustodyInvariantName, custody)

	// USDL leaving custody without a ledger debit breaks the invariant.
	c.transfer(c.custody, x, 11)
	require.Equal(t, int64(5_930_499), c.balanceOf(c.custody))
	c.breaks(keeper.CustodyInvariantName, custody, fmt.Sprintf(
		"custody %s holds 5930499 of %s, below ledgers 5930500 (held quote 4894500, fee held 33000, treasury held 1003000)\n",
		c.custody.Hex(), c.usdl.Hex()))
	c.transfer(c.holder, c.custody, 1)
	c.holds(keeper.CustodyInvariantName, custody)

	// A quote token without code cannot prove custody.
	params := c.k.GetParams(c.ctx)
	params.QuoteToken = address("codeless token").Hex()
	require.NoError(t, c.k.UpdateParams(c.ctx, c.k.Authority(), params))
	message, broken := custody(c.ctx)
	require.True(t, broken)
	require.Contains(t, message, "custody balance unreadable: ")
}
