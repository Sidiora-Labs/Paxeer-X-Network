package keeper

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
)

const (
	// FeeWindowDays is the number of UTC day buckets of the fee window [d-13, d].
	FeeWindowDays = 14

	// FeeTypeTrading is the FeeCollected feeType of a taker or maker fee charged.
	FeeTypeTrading uint8 = 0
	// FeeTypeMakerRebate is the FeeCollected feeType of a maker rebate paid.
	FeeTypeMakerRebate uint8 = 4

	// InsuranceSourceTradingFee is the InsuranceContribution source of the
	// Params.InsuranceSharePpm share of a trading fee.
	InsuranceSourceTradingFee uint8 = 0

	volumeRecordLen      = 64
	venueVolumeRecordLen = 32
)

var ppmDenominator = big.NewInt(types.PpmDenominator)

// AccountFeeTier is the fee schedule of an account on the block's UTC day:
// the volume tier, its taker rate and the maker rate after the rebate tiers.
type AccountFeeTier struct {
	Tier         uint8
	TakerFeePpm  uint64
	MakerFeePpm  int64
	Volume14dUsd uint256.Int
	MakerVolume  uint256.Int
	VenueVolume  uint256.Int
}

// FeeBooking is one fee or rebate booked against an account: FeeUsd is the
// amount in 18-decimal USD, Tokens the USDL units debited (fee) or credited
// (rebate), and Events the FeeCollected and InsuranceContribution records.
type FeeBooking struct {
	FeeType uint8
	RatePpm int64
	FeeUsd  uint256.Int
	Tokens  uint256.Int
	Events  []types.EventRecord
}

func feesAccountAddress() common.Address {
	return common.BytesToAddress(authtypes.NewModuleAddress(types.FeesAccountName))
}

func insuranceAccountAddress() common.Address {
	return common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName))
}

// feeDay is the UTC day d = floor(blockTime / 86400) of the block.
func feeDay(ctx sdk.Context) (uint64, error) {
	now := ctx.BlockTime().Unix()
	if now < 0 {
		return 0, fmt.Errorf("%w: block time %d before the epoch", types.ErrInvalidParams, now)
	}
	return uint64(now) / types.SecondsPerDay, nil
}

func windowStart(day uint64) uint64 {
	if day < FeeWindowDays-1 {
		return 0
	}
	return day - (FeeWindowDays - 1)
}

func addUsd(a, b uint256.Int) (uint256.Int, error) {
	var sum uint256.Int
	if _, overflow := sum.AddOverflow(&a, &b); overflow {
		return uint256.Int{}, fmt.Errorf("%w: USD sum above 2^256-1", types.ErrOverflow)
	}
	return sum, nil
}

func decodeVolume(bz []byte) (taker, maker uint256.Int, err error) {
	if len(bz) != volumeRecordLen {
		return taker, maker, fmt.Errorf("perps: volume record has %d bytes, want %d", len(bz), volumeRecordLen)
	}
	taker.SetBytes32(bz[:32])
	maker.SetBytes32(bz[32:])
	return taker, maker, nil
}

func encodeVolume(taker, maker uint256.Int) []byte {
	takerWord, makerWord := taker.Bytes32(), maker.Bytes32()
	return append(takerWord[:], makerWord[:]...)
}

func decodeVenueVolume(bz []byte) (uint256.Int, error) {
	var volume uint256.Int
	if len(bz) != venueVolumeRecordLen {
		return volume, fmt.Errorf("perps: venue volume record has %d bytes, want %d", len(bz), venueVolumeRecordLen)
	}
	volume.SetBytes32(bz)
	return volume, nil
}

// deleteRange removes every key in [start, end).
func deleteRange(store sdk.KVStore, start, end []byte) {
	iterator := store.Iterator(start, end)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, append([]byte(nil), iterator.Key()...))
	}
	iterator.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}

// RecordVolume books the fill notional to the taker and maker buckets of the
// block's UTC day d and to the venue bucket of day d; both sides count the
// fill notional. The first write of day d to a bucket series deletes its
// buckets of days before d-13.
func (k *Keeper) RecordVolume(ctx sdk.Context, taker, maker common.Address, notionalUsd uint256.Int) error {
	if notionalUsd.IsZero() {
		return fmt.Errorf("%w: zero fill notional", types.ErrZeroAmount)
	}
	day, err := feeDay(ctx)
	if err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	if err := recordAccountVolume(store, taker, day, notionalUsd, false); err != nil {
		return err
	}
	if err := recordAccountVolume(store, maker, day, notionalUsd, true); err != nil {
		return err
	}
	return recordVenueVolume(store, day, notionalUsd)
}

func recordAccountVolume(store sdk.KVStore, account common.Address, day uint64, notionalUsd uint256.Int, isMaker bool) error {
	key := types.VolumeKey(account, day)
	var takerVolume, makerVolume uint256.Int
	if bz := store.Get(key); bz != nil {
		var err error
		if takerVolume, makerVolume, err = decodeVolume(bz); err != nil {
			return err
		}
	} else {
		deleteRange(store, types.VolumeKey(account, 0), types.VolumeKey(account, windowStart(day)))
	}
	var err error
	if isMaker {
		makerVolume, err = addUsd(makerVolume, notionalUsd)
	} else {
		takerVolume, err = addUsd(takerVolume, notionalUsd)
	}
	if err != nil {
		return err
	}
	store.Set(key, encodeVolume(takerVolume, makerVolume))
	return nil
}

func recordVenueVolume(store sdk.KVStore, day uint64, notionalUsd uint256.Int) error {
	key := types.VenueVolumeKey(day)
	var volume uint256.Int
	if bz := store.Get(key); bz != nil {
		var err error
		if volume, err = decodeVenueVolume(bz); err != nil {
			return err
		}
	} else {
		deleteRange(store, types.VenueVolumeKey(0), types.VenueVolumeKey(windowStart(day)))
	}
	volume, err := addUsd(volume, notionalUsd)
	if err != nil {
		return err
	}
	word := volume.Bytes32()
	store.Set(key, word[:])
	return nil
}

// windowVolume sums an account's taker and maker buckets of days d-13 to d.
func windowVolume(store sdk.KVStore, account common.Address, day uint64) (total, maker uint256.Int, err error) {
	iterator := store.Iterator(types.VolumeKey(account, windowStart(day)), types.VolumeKey(account, day+1))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		takerVolume, makerVolume, err := decodeVolume(iterator.Value())
		if err != nil {
			return uint256.Int{}, uint256.Int{}, err
		}
		if total, err = addUsd(total, takerVolume); err != nil {
			return uint256.Int{}, uint256.Int{}, err
		}
		if total, err = addUsd(total, makerVolume); err != nil {
			return uint256.Int{}, uint256.Int{}, err
		}
		if maker, err = addUsd(maker, makerVolume); err != nil {
			return uint256.Int{}, uint256.Int{}, err
		}
	}
	return total, maker, nil
}

func windowVenueVolume(store sdk.KVStore, day uint64) (uint256.Int, error) {
	iterator := store.Iterator(types.VenueVolumeKey(windowStart(day)), types.VenueVolumeKey(day+1))
	defer iterator.Close()
	var total uint256.Int
	for ; iterator.Valid(); iterator.Next() {
		volume, err := decodeVenueVolume(iterator.Value())
		if err != nil {
			return uint256.Int{}, err
		}
		if total, err = addUsd(total, volume); err != nil {
			return uint256.Int{}, err
		}
	}
	return total, nil
}

// FeeTier is the account's tier on the block's UTC day d: the highest tier
// whose threshold the 14-day volume of days d-13 to d exceeds, with the maker
// rate replaced by the highest rebate tier whose share of the venue's 14-day
// volume the account's 14-day maker volume exceeds.
func (k *Keeper) FeeTier(ctx sdk.Context, account common.Address) (AccountFeeTier, error) {
	day, err := feeDay(ctx)
	if err != nil {
		return AccountFeeTier{}, err
	}
	params := k.GetParams(ctx)
	if len(params.FeeTiers) == 0 {
		return AccountFeeTier{}, fmt.Errorf("%w: no fee tiers", types.ErrInvalidParams)
	}
	store := ctx.KVStore(k.storeKey)
	total, makerVolume, err := windowVolume(store, account, day)
	if err != nil {
		return AccountFeeTier{}, err
	}
	venueVolume, err := windowVenueVolume(store, day)
	if err != nil {
		return AccountFeeTier{}, err
	}
	totalBig := total.ToBig()
	tier := 0
	for i, row := range params.FeeTiers {
		if i == 0 || totalBig.Cmp(row.MinVolumeUsd.BigInt()) > 0 {
			tier = i
		}
	}
	result := AccountFeeTier{
		Tier:         uint8(tier),
		TakerFeePpm:  params.FeeTiers[tier].TakerFeePpm,
		MakerFeePpm:  params.FeeTiers[tier].MakerFeePpm,
		Volume14dUsd: total,
		MakerVolume:  makerVolume,
		VenueVolume:  venueVolume,
	}
	if venueVolume.IsZero() {
		return result, nil
	}
	share := new(big.Int).Mul(makerVolume.ToBig(), ppmDenominator)
	venue := venueVolume.ToBig()
	for _, row := range params.RebateTiers {
		if share.Cmp(new(big.Int).Mul(venue, new(big.Int).SetUint64(row.MinMakerSharePpm))) > 0 {
			result.MakerFeePpm = row.MakerFeePpm
		}
	}
	return result, nil
}

// TakerFee charges the taker fee of a fill at the account's taker rate,
// rounded up to the USDL unit. A liquidation fill is an ordinary taker fill:
// no liquidation fee and no liquidator share is written.
func (k *Keeper) TakerFee(ctx sdk.Context, positionID uint256.Int, account common.Address, marketID uint64,
	notionalUsd uint256.Int) (FeeBooking, error) {
	tier, err := k.FeeTier(ctx, account)
	if err != nil {
		return FeeBooking{}, err
	}
	return k.chargeFee(ctx, positionID, account, marketID, notionalUsd, tier.TakerFeePpm)
}

// MakerFee charges the maker fee of a fill rounded up, or for a negative
// maker rate pays the rebate rounded down from perps_fees.
func (k *Keeper) MakerFee(ctx sdk.Context, positionID uint256.Int, account common.Address, marketID uint64,
	notionalUsd uint256.Int) (FeeBooking, error) {
	tier, err := k.FeeTier(ctx, account)
	if err != nil {
		return FeeBooking{}, err
	}
	if tier.MakerFeePpm < 0 {
		return k.payRebate(ctx, positionID, account, marketID, notionalUsd, uint64(-tier.MakerFeePpm))
	}
	return k.chargeFee(ctx, positionID, account, marketID, notionalUsd, uint64(tier.MakerFeePpm))
}

func feeUsdOf(notionalUsd uint256.Int, ratePpm uint64, roundUp bool) sdk.Int {
	product := new(big.Int).Mul(notionalUsd.ToBig(), new(big.Int).SetUint64(ratePpm))
	quotient, remainder := new(big.Int).QuoRem(product, ppmDenominator, new(big.Int))
	if roundUp && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return sdk.NewIntFromBigInt(quotient)
}

func toWord(value sdk.Int) (uint256.Int, error) {
	word, overflow := uint256.FromBig(value.BigInt())
	if overflow || value.IsNegative() {
		return uint256.Int{}, fmt.Errorf("%w: amount %s outside 0 to 2^256-1", types.ErrOverflow, value)
	}
	return *word, nil
}

// chargeFee debits the account the fee in USDL rounded up and credits the
// Params.InsuranceSharePpm share to perps_insurance and the rest to
// perps_fees, each credit rounded down, the remainders to residueUsd.
func (k *Keeper) chargeFee(ctx sdk.Context, positionID uint256.Int, account common.Address, marketID uint64,
	notionalUsd uint256.Int, ratePpm uint64) (FeeBooking, error) {
	if notionalUsd.IsZero() {
		return FeeBooking{}, fmt.Errorf("%w: zero fill notional", types.ErrZeroAmount)
	}
	booking := FeeBooking{FeeType: FeeTypeTrading, RatePpm: int64(ratePpm)}
	if ratePpm == 0 {
		return booking, nil
	}
	params := k.GetParams(ctx)
	feeUsd := feeUsdOf(notionalUsd, ratePpm, true)
	debit, debitResidue, err := types.UsdToUsdlUp(feeUsd)
	if err != nil {
		return FeeBooking{}, err
	}
	insuranceUsd := sdk.NewIntFromBigInt(new(big.Int).Quo(
		new(big.Int).Mul(feeUsd.BigInt(), new(big.Int).SetUint64(params.InsuranceSharePpm)), ppmDenominator))
	insuranceCredit, insuranceResidue, err := types.UsdToUsdlDown(insuranceUsd)
	if err != nil {
		return FeeBooking{}, err
	}
	feesCredit, feesResidue, err := types.UsdToUsdlDown(feeUsd.Sub(insuranceUsd))
	if err != nil {
		return FeeBooking{}, err
	}
	store := ctx.KVStore(k.storeKey)
	if err := debitBookedAccount(store, account, debit, false); err != nil {
		return FeeBooking{}, err
	}
	if err := creditBookedAccount(store, feesAccountAddress(), feesCredit); err != nil {
		return FeeBooking{}, err
	}
	if err := creditBookedAccount(store, insuranceAccountAddress(), insuranceCredit); err != nil {
		return FeeBooking{}, err
	}
	if err := addResidue(store, debitResidue.Add(insuranceResidue).Add(feesResidue)); err != nil {
		return FeeBooking{}, err
	}
	if booking.FeeUsd, err = toWord(feeUsd); err != nil {
		return FeeBooking{}, err
	}
	if booking.Tokens, err = toWord(debit); err != nil {
		return FeeBooking{}, err
	}
	booking.Events = append(booking.Events, feeCollected(positionID, account, marketID, FeeTypeTrading, feeUsd, debit,
		params.CollateralToken))
	if insuranceCredit.IsPositive() {
		booking.Events = append(booking.Events, types.EventRecord{
			Name: types.EventInsuranceContribution,
			Args: []any{params.CollateralToken, insuranceCredit.BigInt(), InsuranceSourceTradingFee},
		})
	}
	return booking, nil
}

// payRebate credits the maker the rebate in USDL rounded down and debits
// perps_fees rounded up, the remainders to residueUsd.
func (k *Keeper) payRebate(ctx sdk.Context, positionID uint256.Int, account common.Address, marketID uint64,
	notionalUsd uint256.Int, rebatePpm uint64) (FeeBooking, error) {
	if notionalUsd.IsZero() {
		return FeeBooking{}, fmt.Errorf("%w: zero fill notional", types.ErrZeroAmount)
	}
	params := k.GetParams(ctx)
	rebateUsd := feeUsdOf(notionalUsd, rebatePpm, false)
	credit, creditResidue, err := types.UsdToUsdlDown(rebateUsd)
	if err != nil {
		return FeeBooking{}, err
	}
	debit, debitResidue, err := types.UsdToUsdlUp(rebateUsd)
	if err != nil {
		return FeeBooking{}, err
	}
	booking := FeeBooking{FeeType: FeeTypeMakerRebate, RatePpm: -int64(rebatePpm)}
	if rebateUsd.IsZero() {
		return booking, nil
	}
	store := ctx.KVStore(k.storeKey)
	if err := debitBookedAccount(store, feesAccountAddress(), debit, true); err != nil {
		return FeeBooking{}, err
	}
	if err := creditBookedAccount(store, account, credit); err != nil {
		return FeeBooking{}, err
	}
	if err := addResidue(store, creditResidue.Add(debitResidue)); err != nil {
		return FeeBooking{}, err
	}
	if booking.FeeUsd, err = toWord(rebateUsd); err != nil {
		return FeeBooking{}, err
	}
	if booking.Tokens, err = toWord(credit); err != nil {
		return FeeBooking{}, err
	}
	booking.Events = append(booking.Events, feeCollected(positionID, account, marketID, FeeTypeMakerRebate, rebateUsd,
		credit, params.CollateralToken))
	return booking, nil
}

func feeCollected(positionID uint256.Int, account common.Address, marketID uint64, feeType uint8, feeUsd,
	tokens sdk.Int, token common.Address) types.EventRecord {
	return types.EventRecord{
		Name: types.EventFeeCollected,
		Args: []any{positionID.ToBig(), account, new(big.Int).SetUint64(marketID), feeType, feeUsd.BigInt(),
			tokens.BigInt(), token},
	}
}

func loadBookedAccount(store sdk.KVStore, address common.Address) (types.Account, bool, error) {
	bz := store.Get(types.AccountKey(address))
	if bz == nil {
		return types.Account{Address: address}, false, nil
	}
	var account types.Account
	if err := account.Unmarshal(bz); err != nil {
		return types.Account{}, false, err
	}
	return account, true, nil
}

// creditBookedAccount adds amount USDL units to the account's store balance,
// creating the record of a store account on its first credit.
func creditBookedAccount(store sdk.KVStore, address common.Address, amount sdk.Int) error {
	if amount.IsZero() {
		return nil
	}
	word, err := toWord(amount)
	if err != nil {
		return err
	}
	account, exists, err := loadBookedAccount(store, address)
	if err != nil {
		return err
	}
	if !exists {
		if address != feesAccountAddress() && address != insuranceAccountAddress() {
			return fmt.Errorf("%w: %s", types.ErrNoAccount, address)
		}
		account.Created = true
	}
	if _, overflow := account.Collateral.AddOverflow(&account.Collateral, &word); overflow {
		return fmt.Errorf("%w: collateral of %s above 2^256-1", types.ErrOverflow, address)
	}
	store.Set(types.AccountKey(address), account.Marshal())
	return nil
}

// debitBookedAccount removes amount USDL units from the account's store
// balance, refusing a debit above it with ErrInsufficientCollateral.
func debitBookedAccount(store sdk.KVStore, address common.Address, amount sdk.Int, storeAccount bool) error {
	word, err := toWord(amount)
	if err != nil {
		return err
	}
	account, exists, err := loadBookedAccount(store, address)
	if err != nil {
		return err
	}
	if !exists && !storeAccount {
		return fmt.Errorf("%w: %s", types.ErrNoAccount, address)
	}
	if account.Collateral.Lt(&word) {
		return fmt.Errorf("%w: %s holds %s USDL units, %s due", types.ErrInsufficientCollateral, address,
			account.Collateral.Dec(), word.Dec())
	}
	account.Collateral.Sub(&account.Collateral, &word)
	store.Set(types.AccountKey(address), account.Marshal())
	return nil
}

func addResidue(store sdk.KVStore, residueUsd sdk.Int) error {
	if residueUsd.IsZero() {
		return nil
	}
	word, err := toWord(residueUsd)
	if err != nil {
		return err
	}
	var books types.VenueBooks
	if bz := store.Get(types.VenueBooksKey); bz != nil {
		if err := books.Unmarshal(bz); err != nil {
			return err
		}
	}
	if _, overflow := books.ResidueUsd.AddOverflow(&books.ResidueUsd, &word); overflow {
		return fmt.Errorf("%w: residueUsd above 2^256-1", types.ErrOverflow)
	}
	store.Set(types.VenueBooksKey, books.Marshal())
	return nil
}
