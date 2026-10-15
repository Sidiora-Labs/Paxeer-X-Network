package keeper

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

var (
	feeA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	feeB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	feeC = common.HexToAddress("0x00000000000000000000000000000000000000c3")
	feeD = common.HexToAddress("0x00000000000000000000000000000000000000d4")
	feeE = common.HexToAddress("0x00000000000000000000000000000000000000e5")
)

type feeEnv struct {
	t   *testing.T
	k   *Keeper
	ctx sdk.Context
}

func newFeeEnv(t *testing.T) *feeEnv {
	key := sdk.NewKVStoreKey(types.StoreKey)
	db := tmdb.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{}, false)
	e := &feeEnv{t: t, k: &Keeper{storeKey: key}, ctx: ctx}
	e.onDay(100)
	return e
}

func (e *feeEnv) onDay(day int64) {
	e.ctx = e.ctx.WithBlockTime(time.Unix(day*types.SecondsPerDay+3_600, 0).UTC())
}

func (e *feeEnv) store() sdk.KVStore { return e.ctx.KVStore(e.k.storeKey) }

// usd18 is whole USD in 18-decimal fixed point.
func usd18(amount int64) uint256.Int {
	value := uint256.NewInt(uint64(amount))
	return *value.Mul(value, uint256.NewInt(1_000_000_000_000_000_000))
}

func word(dec string) uint256.Int {
	value, err := uint256.FromDecimal(dec)
	if err != nil {
		panic(err)
	}
	return *value
}

func (e *feeEnv) fund(address common.Address, units uint64) {
	account := types.Account{Address: address, Created: true}
	account.Collateral.SetUint64(units)
	e.store().Set(types.AccountKey(address), account.Marshal())
}

func (e *feeEnv) collateral(address common.Address) string {
	bz := e.store().Get(types.AccountKey(address))
	require.NotNil(e.t, bz, "account %s", address)
	var account types.Account
	require.NoError(e.t, account.Unmarshal(bz))
	return account.Collateral.Dec()
}

func (e *feeEnv) residue() string {
	bz := e.store().Get(types.VenueBooksKey)
	if bz == nil {
		return "0"
	}
	var books types.VenueBooks
	require.NoError(e.t, books.Unmarshal(bz))
	return books.ResidueUsd.Dec()
}

func (e *feeEnv) trade(taker, maker common.Address, notional uint256.Int) {
	require.NoError(e.t, e.k.RecordVolume(e.ctx, taker, maker, notional))
}

func argStrings(args []any) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		switch value := arg.(type) {
		case *big.Int:
			out[i] = value.String()
		case common.Address:
			out[i] = value.Hex()
		case uint8:
			out[i] = big.NewInt(int64(value)).String()
		default:
			panic("unexpected event argument type")
		}
	}
	return out
}

func feeCollectedArgs(position uint64, user common.Address, market uint64, feeType uint8, feeUsd, tokens string) []string {
	return []string{
		new(big.Int).SetUint64(position).String(), user.Hex(), new(big.Int).SetUint64(market).String(),
		big.NewInt(int64(feeType)).String(), feeUsd, tokens, common.HexToAddress(types.DefaultCollateralToken).Hex(),
	}
}

func TestFeesVolumeTier(t *testing.T) {
	e := newFeeEnv(t)
	e.trade(feeA, feeB, usd18(5_000_000))
	tier, err := e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, uint8(0), tier.Tier, "exactly 5,000,000 USD does not exceed the tier 1 threshold")
	require.Equal(t, uint64(450), tier.TakerFeePpm)
	require.Equal(t, int64(150), tier.MakerFeePpm)

	e.onDay(105)
	e.trade(feeB, feeA, usd18(1))
	tier, err = e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, uint8(1), tier.Tier)
	require.Equal(t, usd18(5_000_001), tier.Volume14dUsd)
	require.Equal(t, uint64(400), tier.TakerFeePpm)

	e.fund(feeA, 100_000_000)
	booking, err := e.k.TakerFee(e.ctx, *uint256.NewInt(7), feeA, 0, usd18(10_000))
	require.NoError(t, err)
	require.Equal(t, FeeTypeTrading, booking.FeeType)
	require.Equal(t, int64(400), booking.RatePpm)
	require.Equal(t, usd18(4), booking.FeeUsd)
	require.Equal(t, "4000000", booking.Tokens.Dec())
	require.Equal(t, "96000000", e.collateral(feeA))
	require.Equal(t, "4000000", e.collateral(feesAccountAddress()))
	require.Len(t, booking.Events, 1)
	require.Equal(t, types.EventFeeCollected, booking.Events[0].Name)
	require.Equal(t, feeCollectedArgs(7, feeA, 0, 0, "4000000000000000000", "4000000"), argStrings(booking.Events[0].Args))

	// B's taker fill of 600 USD at tier 0 is charged 0.27 USD = 270,000 units.
	e.fund(feeC, 1_000_000)
	booking, err = e.k.TakerFee(e.ctx, *uint256.NewInt(9), feeC, 0, usd18(600))
	require.NoError(t, err)
	require.Equal(t, feeCollectedArgs(9, feeC, 0, 0, "270000000000000000", "270000"), argStrings(booking.Events[0].Args))
	require.Equal(t, "730000", e.collateral(feeC))

	// 30,000,000 USD of which 600,000 maker against a venue volume of
	// 30,000,000 is tier 2 with the 1.5 percent rebate row.
	e2 := newFeeEnv(t)
	e2.trade(feeA, feeC, usd18(29_400_000))
	e2.trade(feeD, feeA, usd18(600_000))
	tier, err = e2.k.FeeTier(e2.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, uint8(2), tier.Tier)
	require.Equal(t, uint64(350), tier.TakerFeePpm)
	require.Equal(t, int64(-20), tier.MakerFeePpm)
	require.Equal(t, usd18(30_000_000), tier.Volume14dUsd)
	require.Equal(t, usd18(600_000), tier.MakerVolume)
	require.Equal(t, usd18(30_000_000), tier.VenueVolume)

	_, err = e2.k.TakerFee(e2.ctx, *uint256.NewInt(1), feeB, 0, usd18(10))
	require.ErrorIs(t, err, types.ErrNoAccount)
	_, err = e2.k.TakerFee(e2.ctx, *uint256.NewInt(1), feeA, 0, uint256.Int{})
	require.ErrorIs(t, err, types.ErrZeroAmount)
	require.ErrorIs(t, e2.k.RecordVolume(e2.ctx, feeA, feeB, uint256.Int{}), types.ErrZeroAmount)
	e2.fund(feeB, 1)
	_, err = e2.k.TakerFee(e2.ctx, *uint256.NewInt(1), feeB, 0, usd18(10))
	require.ErrorIs(t, err, types.ErrInsufficientCollateral)
	require.Equal(t, "1", e2.collateral(feeB))
}

func TestFeesRoundingAndRebate(t *testing.T) {
	e := newFeeEnv(t)
	// A's maker volume is 1 percent of the venue volume: rebate tier 1.
	e.trade(feeB, feeA, usd18(10_000))
	e.trade(feeB, feeC, usd18(990_000))
	tier, err := e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, int64(-10), tier.MakerFeePpm)

	e.fund(feeA, 0)
	e.fund(feesAccountAddress(), 50_000)
	booking, err := e.k.MakerFee(e.ctx, *uint256.NewInt(3), feeA, 0, usd18(1_000))
	require.NoError(t, err)
	require.Equal(t, FeeTypeMakerRebate, booking.FeeType)
	require.Equal(t, int64(-10), booking.RatePpm)
	require.Equal(t, "10000", booking.Tokens.Dec())
	require.Equal(t, "10000", e.collateral(feeA))
	require.Equal(t, "40000", e.collateral(feesAccountAddress()))
	require.Equal(t, "0", e.residue())
	require.Len(t, booking.Events, 1)
	require.Equal(t, feeCollectedArgs(3, feeA, 0, 4, "10000000000000000", "10000"), argStrings(booking.Events[0].Args))

	// A rebate with fractional units: the maker is credited rounded down,
	// perps_fees debited rounded up, the two remainders to residueUsd.
	booking, err = e.k.MakerFee(e.ctx, *uint256.NewInt(3), feeA, 0, word("12345678901234567890"))
	require.NoError(t, err)
	require.Equal(t, "123456789012345", booking.FeeUsd.Dec())
	require.Equal(t, "123", booking.Tokens.Dec())
	require.Equal(t, "10123", e.collateral(feeA))
	require.Equal(t, "39876", e.collateral(feesAccountAddress()))
	require.Equal(t, "1000000000000", e.residue())

	// A fee with fractional units: the taker is debited rounded up and
	// perps_fees credited rounded down.
	e.fund(feeD, 1_000)
	booking, err = e.k.TakerFee(e.ctx, *uint256.NewInt(4), feeD, 1, word("1000000000000000001"))
	require.NoError(t, err)
	require.Equal(t, "450000000000001", booking.FeeUsd.Dec())
	require.Equal(t, "451", booking.Tokens.Dec())
	require.Equal(t, "549", e.collateral(feeD))
	require.Equal(t, "40326", e.collateral(feesAccountAddress()))
	require.Equal(t, "2000000000000", e.residue())
	require.Equal(t, feeCollectedArgs(4, feeD, 1, 0, "450000000000001", "451"), argStrings(booking.Events[0].Args))

	// A positive maker rate is a fee charged rounded up.
	e.fund(feeE, 1_000_000)
	booking, err = e.k.MakerFee(e.ctx, *uint256.NewInt(5), feeE, 0, usd18(1_000))
	require.NoError(t, err)
	require.Equal(t, FeeTypeTrading, booking.FeeType)
	require.Equal(t, int64(150), booking.RatePpm)
	require.Equal(t, "150000", booking.Tokens.Dec())
	require.Equal(t, "850000", e.collateral(feeE))

	// A rebate above the perps_fees balance is refused and books nothing.
	e.fund(feesAccountAddress(), 1)
	_, err = e.k.MakerFee(e.ctx, *uint256.NewInt(3), feeA, 0, usd18(1_000))
	require.ErrorIs(t, err, types.ErrInsufficientCollateral)
	require.Equal(t, "10123", e.collateral(feeA))
}

func TestFeesBucketRollOff(t *testing.T) {
	e := newFeeEnv(t)
	e.trade(feeA, feeB, usd18(6_000_000))

	e.onDay(113)
	tier, err := e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, uint8(1), tier.Tier, "the window [d-13, d] of day 113 holds day 100")
	require.Equal(t, usd18(6_000_000), tier.Volume14dUsd)

	e.onDay(114)
	tier, err = e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, uint8(0), tier.Tier)
	require.True(t, tier.Volume14dUsd.IsZero())
	require.True(t, e.store().Has(types.VolumeKey(feeA, 100)), "buckets are deleted only when day d first writes")

	e.trade(feeA, feeB, usd18(100))
	require.False(t, e.store().Has(types.VolumeKey(feeA, 100)))
	require.False(t, e.store().Has(types.VolumeKey(feeB, 100)))
	require.False(t, e.store().Has(types.VenueVolumeKey(100)))
	require.True(t, e.store().Has(types.VolumeKey(feeA, 114)))
	require.Equal(t, encodeVolume(usd18(100), uint256.Int{}), e.store().Get(types.VolumeKey(feeA, 114)))
	require.Equal(t, encodeVolume(uint256.Int{}, usd18(100)), e.store().Get(types.VolumeKey(feeB, 114)))
	venue := usd18(100)
	venueWord := venue.Bytes32()
	require.Equal(t, venueWord[:], e.store().Get(types.VenueVolumeKey(114)))

	tier, err = e.k.FeeTier(e.ctx, feeA)
	require.NoError(t, err)
	require.Equal(t, usd18(100), tier.Volume14dUsd)
}

func TestFeesNoLiquidationFee(t *testing.T) {
	e := newFeeEnv(t)
	e.fund(feeA, 10_000_000)
	// The liquidation fill is an ordinary taker fill at the account's rate.
	booking, err := e.k.TakerFee(e.ctx, *uint256.NewInt(7), feeA, 0, usd18(10_000))
	require.NoError(t, err)
	require.Len(t, booking.Events, 1)
	require.Equal(t, types.EventFeeCollected, booking.Events[0].Name)
	require.Equal(t, feeCollectedArgs(7, feeA, 0, 0, "4500000000000000000", "4500000"), argStrings(booking.Events[0].Args))
	require.Equal(t, "5500000", e.collateral(feeA))
	require.Equal(t, "4500000", e.collateral(feesAccountAddress()))
	require.False(t, e.store().Has(types.AccountKey(insuranceAccountAddress())))
	require.False(t, e.store().Has(types.VenueBooksKey))

	iterator := e.store().Iterator(types.AccountPrefix, []byte{types.AccountPrefix[0] + 1})
	defer iterator.Close()
	var accounts int
	for ; iterator.Valid(); iterator.Next() {
		accounts++
	}
	require.Equal(t, 2, accounts, "only the liquidated account and perps_fees are written")
}
