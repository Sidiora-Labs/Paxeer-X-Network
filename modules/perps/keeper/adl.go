package keeper

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const adlCounterpartiesPerBlock = 64

func ADLRankScore(position types.Position, mark, accountValue uint256.Int) (uint256.Int, bool, error) {
	if mark.IsZero() || position.EntryPrice.IsZero() {
		return uint256.Int{}, false, types.ErrPriceStale
	}
	profitable := adlProfitable(position, mark)
	if !profitable || position.SizeBase.IsZero() {
		return uint256.Int{}, false, nil
	}
	if accountValue.IsZero() {
		return uint256.Int{}, true, types.ErrInsufficientMargin
	}
	scale := *uint256.NewInt(1_000_000_000_000_000_000)
	numerator, denominator := mark, position.EntryPrice
	if !position.IsLong {
		numerator, denominator = denominator, numerator
	}
	ratio, err := adlMulDiv(numerator, scale, denominator)
	if err != nil {
		return uint256.Int{}, true, err
	}
	notional, err := adlMulDiv(position.SizeBase, mark, scale)
	if err != nil {
		return uint256.Int{}, true, err
	}
	leverage, err := adlMulDiv(notional, scale, accountValue)
	if err != nil {
		return uint256.Int{}, true, err
	}
	score, err := adlMulDiv(ratio, leverage, scale)
	return score, true, err
}

func (k *Keeper) RankADL(ctx sdk.Context, deficitID, previousMark uint256.Int, limit uint32) ([]types.Position, error) {
	if previousMark.IsZero() {
		return nil, types.ErrPriceStale
	}
	if limit > adlCounterpartiesPerBlock {
		return nil, types.ErrInvalidParams
	}
	store := ctx.KVStore(k.storeKey)
	deficit, err := adlPosition(store, deficitID)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		return nil, nil
	}
	prefix := types.AdlRankSidePrefix(deficit.MarketID, types.SideOf(!deficit.IsLong))
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()
	ranked := make([]types.Position, 0, limit)
	for ; iterator.Valid() && len(ranked) < int(limit); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+64 || len(iterator.Value()) != 0 {
			return nil, fmt.Errorf("%w: malformed ADL index entry", types.ErrInvalidParams)
		}
		var id uint256.Int
		id.SetBytes(key[len(key)-32:])
		position, err := adlPosition(store, id)
		if err != nil {
			return nil, err
		}
		expected := types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID)
		if !bytes.Equal(key, expected) || position.SizeBase.IsZero() {
			return nil, fmt.Errorf("%w: inconsistent ADL index entry", types.ErrInvalidParams)
		}
		if position.Account != deficit.Account && adlProfitable(position, previousMark) {
			ranked = append(ranked, position)
		}
	}
	return ranked, nil
}

func (k *Keeper) ExecuteADL(ctx sdk.Context, deficitIDs []uint256.Int, previousMarks map[uint64]uint256.Int) ([]types.EventRecord, error) {
	for i := 1; i < len(deficitIDs); i++ {
		if deficitIDs[i-1].Cmp(&deficitIDs[i]) >= 0 {
			return nil, types.ErrInvalidParams
		}
	}
	cached, write := ctx.CacheContext()
	store := cached.KVStore(k.storeKey)
	insuranceAddress := common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName))
	insurance, err := adlAccount(store, insuranceAddress)
	if err != nil {
		return nil, err
	}
	if !insurance.Collateral.IsZero() {
		return nil, nil
	}
	remaining := uint32(adlCounterpartiesPerBlock)
	var events []types.EventRecord
	for _, id := range deficitIDs {
		if remaining == 0 {
			break
		}
		deficit, err := adlPosition(store, id)
		if err != nil {
			return nil, err
		}
		account, err := adlAccount(store, deficit.Account)
		if err != nil {
			return nil, err
		}
		if account.DeficitUsd.IsZero() {
			continue
		}
		price, found := previousMarks[deficit.MarketID]
		if !found || price.IsZero() {
			return nil, types.ErrPriceStale
		}
		ranked, err := k.RankADL(cached, id, price, remaining)
		if err != nil {
			return nil, err
		}
		var total uint256.Int
		for _, counterparty := range ranked {
			account, err = adlAccount(store, deficit.Account)
			if err != nil {
				return nil, err
			}
			if deficit.SizeBase.IsZero() {
				break
			}
			size := deficit.SizeBase
			if counterparty.SizeBase.Lt(&size) {
				size = counterparty.SizeBase
			}
			notional, err := adlMulDiv(size, price, *uint256.NewInt(1_000_000_000_000_000_000))
			if err != nil {
				return nil, err
			}
			for _, position := range []types.Position{counterparty, deficit} {
				applied, err := k.ApplyFill(cached, PositionFill{
					Account: position.Account, MarketID: position.MarketID, IsLong: !position.IsLong, SizeBase: size,
					Price: price, Leverage: position.Leverage, Kind: FillKindADL,
				})
				if err != nil {
					return nil, err
				}
				events = append(events, applied.Events...)
				events = append(events, applied.TradeSettledEvents(new(big.Int), new(big.Int))...)
			}
			if err := adlCoverDeficit(store, deficit.Account, counterparty, size, price); err != nil {
				return nil, err
			}
			oiEvent, err := k.CheckOpenInterest(cached, OpenInterestFill{
				MarketID: deficit.MarketID, SizeBase: size, PriceUsd: price,
			})
			if err != nil {
				return nil, err
			}
			if oiEvent != nil {
				events = append(events, *oiEvent)
			}
			if _, overflow := total.AddOverflow(&total, &notional); overflow {
				return nil, types.ErrOverflow
			}
			events = append(events, types.EventRecord{
				Name: types.EventADLMatched,
				Args: []any{new(big.Int).SetUint64(deficit.MarketID), counterparty.ID.ToBig(), counterparty.Account,
					notional.ToBig(), price.ToBig(), counterparty.RankScore.ToBig()},
			})
			remaining--
			deficit.SizeBase.Sub(&deficit.SizeBase, &size)
			if !deficit.SizeBase.IsZero() {
				deficit, err = adlPosition(store, id)
				if err != nil {
					return nil, err
				}
			}
		}
		if !total.IsZero() {
			events = append(events, types.EventRecord{
				Name: types.EventADLExecuted, Args: []any{id.ToBig(), total.ToBig()},
			})
		}
	}
	write()
	return events, nil
}

// adlCoverDeficit debits from the counterparty collateral the least of the
// profit its reduced size realized at the previous mark, its collateral and the
// holder deficit, rounding the USDL debit up into residueUsd, and subtracts the
// covered USD from the holder deficit and the venue deficit sum.
func adlCoverDeficit(store sdk.KVStore, holder common.Address, counterparty types.Position, size, price uint256.Int) error {
	account, err := adlAccount(store, holder)
	if err != nil {
		return err
	}
	if account.DeficitUsd.IsZero() {
		return nil
	}
	payer, err := adlAccount(store, counterparty.Account)
	if err != nil {
		return err
	}
	var books types.VenueBooks
	if raw := store.Get(types.VenueBooksKey); raw != nil {
		if err := books.Unmarshal(raw); err != nil {
			return fmt.Errorf("%w: venue books: %v", types.ErrInvalidParams, err)
		}
	}
	move := new(big.Int).Sub(price.ToBig(), counterparty.EntryPrice.ToBig())
	covered := new(big.Int).Quo(new(big.Int).Mul(size.ToBig(), move.Abs(move)), big.NewInt(1_000_000_000_000_000_000))
	if balance := new(big.Int).Mul(payer.Collateral.ToBig(), insuranceUsdPerUsdl); balance.Cmp(covered) < 0 {
		covered = balance
	}
	if deficit := account.DeficitUsd.ToBig(); deficit.Cmp(covered) < 0 {
		covered = deficit
	}
	if covered.Sign() == 0 {
		return nil
	}
	debit := insuranceCeilDiv(covered, insuranceUsdPerUsdl)
	residue := new(big.Int).Sub(new(big.Int).Mul(debit, insuranceUsdPerUsdl), covered)
	if books.ResidueUsd, err = insuranceAdd(books.ResidueUsd, residue); err != nil {
		return err
	}
	if books.DeficitSumUsd.ToBig().Cmp(covered) < 0 {
		return fmt.Errorf("%w: deficit sum %s below the ADL cover %s of account %s",
			types.ErrInvalidParams, books.DeficitSumUsd.Dec(), covered, holder)
	}
	books.DeficitSumUsd.SetFromBig(new(big.Int).Sub(books.DeficitSumUsd.ToBig(), covered))
	payer.Collateral.SetFromBig(new(big.Int).Sub(payer.Collateral.ToBig(), debit))
	account.DeficitUsd.SetFromBig(new(big.Int).Sub(account.DeficitUsd.ToBig(), covered))
	store.Set(types.AccountKey(counterparty.Account), payer.Marshal())
	store.Set(types.AccountKey(holder), account.Marshal())
	store.Set(types.VenueBooksKey, books.Marshal())
	return nil
}

func adlPosition(store sdk.KVStore, id uint256.Int) (types.Position, error) {
	var position types.Position
	raw := store.Get(types.PositionKey(id))
	if raw == nil {
		return position, types.ErrUnknownPosition
	}
	if err := position.Unmarshal(raw); err != nil {
		return position, fmt.Errorf("%w: %v", types.ErrInvalidParams, err)
	}
	if position.ID != id || position.SizeBase.IsZero() {
		return position, types.ErrInvalidParams
	}
	return position, nil
}

func adlAccount(store sdk.KVStore, address common.Address) (types.Account, error) {
	var account types.Account
	raw := store.Get(types.AccountKey(address))
	if raw == nil {
		return account, types.ErrNoAccount
	}
	if err := account.Unmarshal(raw); err != nil {
		return account, fmt.Errorf("%w: %v", types.ErrInvalidParams, err)
	}
	if account.Address != address || !account.Created {
		return account, types.ErrNoAccount
	}
	return account, nil
}

func adlProfitable(position types.Position, mark uint256.Int) bool {
	if position.EntryPrice.IsZero() || mark.IsZero() {
		return false
	}
	if position.IsLong {
		return mark.Gt(&position.EntryPrice)
	}
	return mark.Lt(&position.EntryPrice)
}

func adlMulDiv(a, b, denominator uint256.Int) (uint256.Int, error) {
	if denominator.IsZero() {
		return uint256.Int{}, types.ErrInvalidParams
	}
	var product uint256.Int
	if _, overflow := product.MulOverflow(&a, &b); overflow {
		return uint256.Int{}, types.ErrOverflow
	}
	product.Div(&product, &denominator)
	return product, nil
}
