package keeper_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	// The curve of the golden vectors: virtual 10000000000 USDL, no real
	// USDL and 1000000000000000 tokens at the pool.
	swapVirtualQuote int64 = 10_000_000_000
	swapTokenReserve int64 = 1_000_000_000_000_000
	// swapSellerTokens are the pool tokens the trader holds for the sells.
	swapSellerTokens int64 = 200_000_000_000
	// swapStartPrice is effectiveQuote*1e18/tokenReserve of that curve.
	swapStartPrice int64 = 10_000_000_000_000
)

// swapEnv is a generation 2 market whose pool token is a SidioraERC20 in the
// test EVM, with the pool tokens at the pool address and USDL in custody.
type swapEnv struct {
	*custodyEnv
	denom       string
	pool        common.Address
	token       common.Address
	accumulator common.Address
	recipient   common.Address
	locks       keeper.TransientStore
}

func newSwapEnv(t *testing.T, adjust func(*types.Market)) *swapEnv {
	t.Helper()
	c := newCustodyEnv(t)
	pool := address("swap pool P1")
	token := c.deployToken("Kindle Token", "KNDL", swapTokenReserve+swapSellerTokens, c.holder)
	c.tokenTransfer(token, c.holder, pool, b(swapTokenReserve))

	market := generation2Market(address("fee right collection"), 1, c.account(0))
	market.PoolAddress = pool.Hex()
	market.TokenAddress = token.Hex()
	market.TotalSupply = i(swapTokenReserve + swapSellerTokens)
	market.VirtualQuoteReserve = i(swapVirtualQuote)
	market.TokenReserve = i(swapTokenReserve)
	market.AccumulatedFees = sdk.ZeroInt()
	market.SnapshotHeights = make([]uint64, types.SnapshotSlots)
	// A mature pool: the age factor is 500/1001 = 0, so the fee is the 30 bps
	// base fee.
	market.CreationTime = genesisTime.Add(-1000 * time.Hour).Unix()
	if adjust != nil {
		adjust(&market)
	}
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	gs.Markets = []types.Market{market}
	require.NoError(t, gs.Validate())
	c.k.InitGenesis(c.ctx, *gs)
	return &swapEnv{custodyEnv: c, denom: market.Denom, pool: pool, token: token,
		accumulator: common.HexToAddress(market.FeeAccumulator), recipient: address("swap recipient R"),
		locks: c.stateDB()}
}

func (c *custodyEnv) tokenBalance(token, holder common.Address) string {
	c.t.Helper()
	input := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(holder.Bytes(), 32)...)
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(c.ctx, c.k.ModuleAddress(), &token, input)
	require.NoError(c.t, err)
	require.Len(c.t, ret, 32)
	return new(big.Int).SetBytes(ret).String()
}

// swap runs Keeper.Swap on a branch of the context and keeps the branch only
// when the swap succeeds, as a reverted precompile run drops its writes.
func (s *swapEnv) swap(isBuy bool, amountIn, minOut *big.Int, recipient common.Address,
	deadline uint64) (keeper.SwapResult, error) {
	s.t.Helper()
	branch, write := s.ctx.CacheContext()
	_, result, err := s.k.Swap(branch, s.caller, s.locks, s.denom, isBuy, amountIn, minOut, s.holder, recipient, deadline)
	if err == nil {
		write()
	}
	return result, err
}

// buy sends amount USDL to the pool address and buys with it.
func (s *swapEnv) buy(amount int64) keeper.SwapResult {
	s.t.Helper()
	s.transfer(s.holder, s.pool, amount)
	result, err := s.swap(true, b(amount), b(1), s.recipient, farDeadline)
	require.NoError(s.t, err)
	return result
}

func requireSwapRevert(t *testing.T, err error, target error, signature string) {
	t.Helper()
	require.ErrorIs(t, err, target)
	requireRevert(t, err, hexutil.Encode(crypto.Keccak256([]byte(signature))[:4]))
}

func requireResult(t *testing.T, result keeper.SwapResult, amountIn, amountOut, feeBps, fee, protocolCut, poolCut string) {
	t.Helper()
	require.Equal(t, []string{amountIn, amountOut, feeBps, fee, protocolCut, poolCut},
		[]string{result.AmountIn.String(), result.AmountOut.String(), result.FeeBps.String(), result.FeeAmount.String(),
			result.ProtocolCut.String(), result.PoolCut.String()})
}

func requireBook(t *testing.T, m types.Market, real, tokenReserve, quoteFees, tokenFees, volume, buyVolume, sellVolume string) {
	t.Helper()
	require.Equal(t, []string{real, tokenReserve, quoteFees, tokenFees, volume, buyVolume, sellVolume},
		[]string{m.RealQuoteBalance.String(), m.TokenReserve.String(), m.AccumulatedQuoteFees.String(),
			m.AccumulatedTokenFees.String(), m.CumulativeVolume.String(), m.CumulativeBuyQuoteVolume.String(),
			m.CumulativeSellQuoteVolume.String()})
}

func TestSwapGoldenVectors(t *testing.T) {
	s := newSwapEnv(t, nil)
	held, feeHeld := keeper.HeldQuoteLedger(s.pool), keeper.FeeHeldLedger(s.accumulator)

	t.Run("refusals in contract order", func(t *testing.T) {
		expired := uint64(genesisTime.Unix() - 1)
		_, err := s.swap(true, b(0), b(0), common.Address{}, expired)
		requireSwapRevert(t, err, types.ErrDeadlineExpired, "DeadlineExpired()")
		_, err = s.swap(true, b(0), b(0), common.Address{}, farDeadline)
		requireSwapRevert(t, err, types.ErrInsufficientInput, "InsufficientInput()")
		_, err = s.swap(true, b(1_000_000), b(0), common.Address{}, farDeadline)
		requireSwapRevert(t, err, types.ErrZeroAddress, "ZeroAddress()")
		// Nothing was paid to the pool: the prepaid input check refuses.
		_, err = s.swap(true, b(1_000_000), b(0), s.recipient, farDeadline)
		requireSwapRevert(t, err, types.ErrInsufficientInput, "InsufficientInput()")
		_, err = s.swap(false, b(1_000_000), b(0), s.recipient, farDeadline)
		requireSwapRevert(t, err, types.ErrInsufficientInput, "InsufficientInput()")

		s.transfer(s.holder, s.pool, 1_000_000)
		_, err = s.swap(true, b(1_000_000), b(99_690_060_901), s.recipient, farDeadline)
		requireSwapRevert(t, err, types.ErrSlippageExceeded, "SlippageExceeded()")
		// The pool lock refuses a second entry while a swap of the pool runs.
		release, err := s.k.EnterLock(s.locks, keeper.PoolLock(s.pool))
		require.NoError(t, err)
		_, err = s.swap(true, b(1_000_000), b(0), s.recipient, farDeadline)
		requireSwapRevert(t, err, types.ErrReentrancyGuardReentrantCall, "ReentrancyGuardReentrantCall()")
		release()
		// Every refusal left the book, the ledgers and the USDL where they were.
		requireBook(t, s.market(s.denom), "0", "1000000000000000", "0", "0", "0", "0", "0")
		require.Equal(t, int64(1_000_000), s.balanceOf(s.pool))
		require.Zero(t, s.ledger(held))
	})

	t.Run("buy_1", func(t *testing.T) {
		result := s.buy(1_000_000)
		requireResult(t, result, "1000000", "99690060900", "30", "3000", "300", "2700")
		m := s.market(s.denom)
		requireBook(t, m, "997000", "999900309939100", "3000", "0", "1000000", "1000000", "0")
		require.Zero(t, s.balanceOf(s.pool))
		require.Equal(t, int64(1_000_000), s.balanceOf(s.custody))
		require.Equal(t, int64(997_000), s.ledger(held))
		require.Equal(t, int64(3000), s.ledger(feeHeld)+s.ledger(keeper.TreasuryHeldLedger()))
		require.Equal(t, int64(300), s.ledger(keeper.TreasuryHeldLedger()))
		require.Equal(t, "99690060900", s.tokenBalance(s.token, s.recipient))
		require.Equal(t, m.TokenReserve.String(), s.tokenBalance(s.token, s.pool))
	})

	t.Run("sell_floor", func(t *testing.T) {
		before := s.market(s.denom)
		s.tokenTransfer(s.token, s.holder, s.pool, b(99_990_131_294))
		_, err := s.swap(false, b(99_990_131_294), b(0), s.recipient, farDeadline)
		requireSwapRevert(t, err, types.ErrVirtualFloorBreached, "VirtualFloorBreached()")
		sameJSON(t, before, s.market(s.denom))
		require.Equal(t, int64(997_000), s.ledger(held))
		require.Zero(t, s.balanceOf(s.recipient))
		require.Equal(t, "1000000300070394", s.tokenBalance(s.token, s.pool))
	})

	t.Run("sell to the floor", func(t *testing.T) {
		result, err := s.swap(false, b(99_990_131_293), b(997_000), s.recipient, farDeadline)
		require.NoError(t, err)
		requireResult(t, result, "99990131293", "997000", "30", "299970393", "0", "0")
		requireBook(t, s.market(s.denom), "0", "1000000300070393", "3000", "299970393", "1997000", "1000000", "997000")
		require.Equal(t, int64(997_000), s.balanceOf(s.recipient))
		require.Zero(t, s.ledger(held))
		require.Equal(t, int64(3000), s.balanceOf(s.custody))
		require.Equal(t, "1000000300070394", s.tokenBalance(s.token, s.pool))
	})
}

func TestSnapshotGate(t *testing.T) {
	now := genesisTime.Unix()
	ring := func(count, index uint64, last int64) func(*types.Market) {
		return func(m *types.Market) {
			for n := uint64(0); n < count; n++ {
				m.PriceSnapshots[n] = i(swapStartPrice)
			}
			m.SnapshotCount, m.SnapshotIndex, m.LastSnapshotTimestamp = count, index, last
		}
	}

	t.Run("snap_skip and snap_take", func(t *testing.T) {
		s := newSwapEnv(t, ring(1, 1, now-61))
		s.ctx = s.ctx.WithBlockHeight(7)
		// 1000000 is below 1 bps of the effective quote after the buy
		// (10000997000 / 10000 = 1000099).
		s.buy(1_000_000)
		m := s.market(s.denom)
		require.Equal(t, []uint64{1, 1}, []uint64{m.SnapshotCount, m.SnapshotIndex})
		require.Equal(t, now-61, m.LastSnapshotTimestamp)
		require.Equal(t, "0", m.PriceSnapshots[1].String())

		// 61 seconds after the last snapshot a buy of 2000000 clears the
		// 1000299 volume floor and takes slot 1.
		s.buy(2_000_000)
		m = s.market(s.denom)
		require.Equal(t, []uint64{2, 2}, []uint64{m.SnapshotCount, m.SnapshotIndex})
		require.Equal(t, now, m.LastSnapshotTimestamp)
		price, err := m.Price()
		require.NoError(t, err)
		require.Equal(t, price.String(), m.PriceSnapshots[1].String())
		require.Equal(t, uint64(7), m.SnapshotHeights[1])

		// Inside the 60 second interval the ring stays.
		s.ctx = s.ctx.WithBlockTime(genesisTime.Add(59 * time.Second))
		s.buy(2_000_000)
		require.Equal(t, uint64(2), s.market(s.denom).SnapshotIndex)
		require.Equal(t, now, s.market(s.denom).LastSnapshotTimestamp)

		// At exactly 60 seconds the next slot is taken.
		s.ctx = s.ctx.WithBlockTime(genesisTime.Add(60 * time.Second))
		s.buy(2_000_000)
		m = s.market(s.denom)
		require.Equal(t, []uint64{3, 3}, []uint64{m.SnapshotCount, m.SnapshotIndex})
		require.Equal(t, now+60, m.LastSnapshotTimestamp)
	})

	t.Run("first snapshot ignores the interval", func(t *testing.T) {
		s := newSwapEnv(t, ring(0, 0, now))
		s.buy(2_000_000)
		m := s.market(s.denom)
		require.Equal(t, []uint64{1, 1}, []uint64{m.SnapshotCount, m.SnapshotIndex})
		price, err := m.Price()
		require.NoError(t, err)
		require.Equal(t, "10003988397603", price.String())
		require.Equal(t, price.String(), m.PriceSnapshots[0].String())
	})

	t.Run("ring wraps modulo 8", func(t *testing.T) {
		s := newSwapEnv(t, ring(types.SnapshotSlots, types.SnapshotSlots-1, now-61))
		s.buy(2_000_000)
		m := s.market(s.denom)
		require.Equal(t, []uint64{types.SnapshotSlots, 0}, []uint64{m.SnapshotCount, m.SnapshotIndex})
		require.Equal(t, "10003988397603", m.PriceSnapshots[types.SnapshotSlots-1].String())
		require.Equal(t, i(swapStartPrice).String(), m.PriceSnapshots[0].String())
	})

	t.Run("sell volume is the USDL paid out", func(t *testing.T) {
		s := newSwapEnv(t, ring(1, 1, now-61))
		s.buy(4_000_000)
		require.Equal(t, uint64(2), s.market(s.denom).SnapshotCount)
		s.ctx = s.ctx.WithBlockTime(genesisTime.Add(61 * time.Second))
		// The sell pays out less than 1 bps of the effective quote although
		// its token input is large, so no snapshot is taken.
		s.tokenTransfer(s.token, s.holder, s.pool, b(1_000_000))
		result, err := s.swap(false, b(1_000_000), b(1), s.recipient, farDeadline)
		require.NoError(t, err)
		m := s.market(s.denom)
		require.Equal(t, uint64(2), m.SnapshotCount)
		require.Equal(t, result.AmountOut.String(), m.CumulativeSellQuoteVolume.String())
		require.Equal(t, new(big.Int).Add(b(4_000_000), result.AmountOut).String(), m.CumulativeVolume.String())
	})
}

func TestDynamicFeeVectors(t *testing.T) {
	for name, tc := range map[string]struct {
		age       time.Duration
		snapshots []int64
		fee       string
		feeAmount string
	}{
		"fee_age_0":    {0, nil, "300", "30000"},
		"fee_age_3600": {time.Hour, nil, "280", "28000"},
		// Two snapshots a doubling apart: volatility 1e6, factor 100.
		"fee_vol_high": {1000 * time.Hour, []int64{swapStartPrice, 2 * swapStartPrice}, "130", "13000"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSwapEnv(t, func(m *types.Market) {
				m.CreationTime = genesisTime.Add(-tc.age).Unix()
				for n, price := range tc.snapshots {
					m.PriceSnapshots[n] = i(price)
				}
				m.SnapshotCount = uint64(len(tc.snapshots))
				m.SnapshotIndex = uint64(len(tc.snapshots))
				m.LastSnapshotTimestamp = genesisTime.Unix()
			})
			fee, err := s.k.FeeBps(s.ctx, s.market(s.denom))
			require.NoError(t, err)
			require.Equal(t, tc.fee, fee.String())
			result := s.buy(1_000_000)
			require.Equal(t, tc.fee, result.FeeBps.String())
			require.Equal(t, tc.feeAmount, result.FeeAmount.String())
			require.Equal(t, tc.feeAmount, s.market(s.denom).AccumulatedQuoteFees.String())
		})
	}
}
