package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TradeSettled tradeType values of contracts/perps.md section 2.
const (
	TradeTypeOpen        uint8 = 0
	TradeTypeClose       uint8 = 1
	TradeTypeLiquidation uint8 = 2
	TradeTypeReduce      uint8 = 3
	TradeTypeIncrease    uint8 = 4
	TradeTypeADL         uint8 = 5
	TradeTypeBackstop    uint8 = 6
)

// FillKind says which step a fill comes from: a book trade, whose tradeType
// follows the netting, or a liquidation, ADL or backstop step, whose
// tradeType is that of the step.
type FillKind uint8

const (
	FillKindTrade       FillKind = 0
	FillKindLiquidation FillKind = 1
	FillKindADL         FillKind = 2
	FillKindBackstop    FillKind = 3
)

var (
	positionWad      = big.NewInt(1_000_000_000_000_000_000)
	positionUsdlUnit = big.NewInt(1_000_000_000_000)
	positionMaxWord  = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

// PositionFill is one side of a fill: the account buys (IsLong) or sells
// SizeBase in base units at Price; Leverage is that of the order and becomes
// the leverage of a position the fill opens or flips.
type PositionFill struct {
	Account  common.Address
	MarketID uint64
	IsLong   bool
	SizeBase uint256.Int
	Price    uint256.Int
	Leverage uint256.Int
	Kind     FillKind
}

// TradeSettlement is what one fill settled into one position: the fill
// notional of that part, the realised PnL and the funding received (negative
// when paid) in USD, and the change of the account's collateral in USDL units.
type TradeSettlement struct {
	PositionID      uint256.Int
	Account         common.Address
	MarketID        uint64
	TradeType       uint8
	SizeUsd         uint256.Int
	Price           uint256.Int
	GrossPnlUsd     *big.Int
	FundingUsd      *big.Int
	CollateralDelta *big.Int
}

// PositionFillResult is the outcome of ApplyFill: the account's position in
// the market after the fill (the closed one when the fill closed it), the
// funding and position events in emission order, and one settlement per
// position the fill touched, the closed part of a flip first.
type PositionFillResult struct {
	PositionID uint256.Int
	Events     []types.EventRecord
	Trades     []TradeSettlement
}

// TradeSettledEvents returns the TradeSettled records of the fill net of its
// fee: feeUsd and feeTokens are the fee booked on the fill, negative for a
// maker rebate, and belong to the last settlement, the position the fee is
// charged to. netPayoutTokens is the collateral the fill credited net of the
// fee, 0 when it debited.
func (r PositionFillResult) TradeSettledEvents(feeUsd, feeTokens *big.Int) []types.EventRecord {
	events := make([]types.EventRecord, 0, len(r.Trades))
	for i, trade := range r.Trades {
		fee, tokens := new(big.Int), new(big.Int)
		if i == len(r.Trades)-1 {
			fee.Set(feeUsd)
			tokens.Set(feeTokens)
		}
		if fee.Sign() < 0 {
			fee.SetInt64(0)
		}
		net := new(big.Int).Sub(trade.CollateralDelta, tokens)
		if net.Sign() < 0 {
			net.SetInt64(0)
		}
		events = append(events, types.EventRecord{Name: types.EventTradeSettled, Args: []any{
			trade.PositionID.ToBig(), trade.Account, new(big.Int).SetUint64(trade.MarketID), trade.TradeType,
			trade.SizeUsd.ToBig(), trade.Price.ToBig(), new(big.Int).Set(trade.GrossPnlUsd), fee,
			new(big.Int).Set(trade.FundingUsd), net,
		}})
	}
	return events
}

// ApplyFill nets one side of a fill into the account's single position in the
// market, the netting of execute_position_open and execute_position_close: a
// held position is first settled at the fill price, realising
// side x (size x price / 1e18 - cost basis) and the funding since its funding
// basis at the market's cumulative index into collateral and resetting its
// reference price; then a fill on its side increases it with the size-weighted
// entry rounded half up, and an opposite fill reduces, closes or flips it.
// Both parties of a fill book the same fill notional, size x price / 1e18
// rounded down, and the same funding basis, size x index / 1e18 rounded down,
// so the solvency identity of contracts/perps.md section 24 holds exactly;
// the rounding a closed position leaves goes to residueUsd. Every position
// write rewrites its AdlRankIndex entry and the account's MarginRatioIndex
// entry. Fees are booked by the caller, which builds TradeSettled through
// TradeSettledEvents. A refused fill writes nothing.
func (k *Keeper) ApplyFill(ctx sdk.Context, fill PositionFill) (PositionFillResult, error) {
	cacheCtx, write := ctx.CacheContext()
	result, err := k.applyFill(cacheCtx, fill)
	if err != nil {
		return PositionFillResult{}, err
	}
	write()
	return result, nil
}

func (k *Keeper) applyFill(ctx sdk.Context, fill PositionFill) (PositionFillResult, error) {
	if fill.SizeBase.IsZero() || fill.Price.IsZero() {
		return PositionFillResult{}, fmt.Errorf("%w: fill of %s base at %s", types.ErrZeroAmount, fill.SizeBase.Dec(),
			fill.Price.Dec())
	}
	if fill.Kind > FillKindBackstop {
		return PositionFillResult{}, fmt.Errorf("%w: fill kind %d", types.ErrInvalidParams, fill.Kind)
	}
	market, err := k.GetMarket(ctx, fill.MarketID)
	if err != nil {
		return PositionFillResult{}, err
	}
	if err := positionCheckLot(market, fill.SizeBase); err != nil {
		return PositionFillResult{}, err
	}
	state, err := k.GetMarketState(ctx, fill.MarketID)
	if err != nil {
		return PositionFillResult{}, err
	}
	account, err := k.positionAccount(ctx, fill.Account)
	if err != nil {
		return PositionFillResult{}, err
	}
	position, exists, err := k.positionOf(ctx, fill.Account, fill.MarketID)
	if err != nil {
		return PositionFillResult{}, err
	}
	books, err := k.positionVenueBooks(ctx)
	if err != nil {
		return PositionFillResult{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return PositionFillResult{}, err
	}

	size := fill.SizeBase.ToBig()
	price := fill.Price.ToBig()
	index := signedBig(state.FundingIndex)
	notional := new(big.Int).Div(new(big.Int).Mul(size, price), positionWad)
	fundingShare := new(big.Int).Div(new(big.Int).Mul(size, index), positionWad)

	var result PositionFillResult
	modified := false
	if !exists {
		opened, err := k.positionOpen(ctx, &account, fill, params, fill.SizeBase, notional, fundingShare, &result)
		if err != nil {
			return PositionFillResult{}, err
		}
		position = opened
	} else {
		settled, err := positionSettle(&account, &position, fill.Price, state.FundingIndex, &books, &result)
		if err != nil {
			return PositionFillResult{}, err
		}
		held := position.SizeBase.ToBig()
		switch {
		case position.IsLong == fill.IsLong:
			if err := positionIncrease(&position, size, price, notional, fundingShare, &result); err != nil {
				return PositionFillResult{}, err
			}
			settled.TradeType = TradeTypeIncrease
			settled.SizeUsd.SetFromBig(notional)
			result.Trades = append(result.Trades, settled)
			modified = true
		case size.Cmp(held) < 0:
			if err := positionReduce(&position, size, notional, fundingShare); err != nil {
				return PositionFillResult{}, err
			}
			result.Events = append(result.Events, positionClosedEvent(position, size, fill.Price, notional, false))
			settled.TradeType = TradeTypeReduce
			settled.SizeUsd.SetFromBig(notional)
			result.Trades = append(result.Trades, settled)
			modified = true
		default:
			closeNotional := new(big.Int).Div(new(big.Int).Mul(held, price), positionWad)
			closeFunding := new(big.Int).Div(new(big.Int).Mul(held, index), positionWad)
			result.Events = append(result.Events, positionClosedEvent(position, held, fill.Price, closeNotional, true))
			if err := k.positionRemove(ctx, &account, position, closeNotional, closeFunding, &books); err != nil {
				return PositionFillResult{}, err
			}
			settled.TradeType = TradeTypeClose
			settled.SizeUsd.SetFromBig(closeNotional)
			result.Trades = append(result.Trades, settled)
			result.PositionID = position.ID
			if size.Cmp(held) == 0 {
				position = types.Position{}
				break
			}
			var rest uint256.Int
			rest.SetFromBig(new(big.Int).Sub(size, held))
			opened, err := k.positionOpen(ctx, &account, fill, params, rest, new(big.Int).Sub(notional, closeNotional),
				new(big.Int).Sub(fundingShare, closeFunding), &result)
			if err != nil {
				return PositionFillResult{}, err
			}
			position = opened
		}
	}
	for i := range result.Trades {
		result.Trades[i].Account = fill.Account
		result.Trades[i].MarketID = fill.MarketID
		result.Trades[i].Price = fill.Price
		if tradeType, forced := positionKindTradeType(fill.Kind); forced {
			result.Trades[i].TradeType = tradeType
		}
	}

	store := k.store(ctx)
	store.Set(types.VenueBooksKey, books.Marshal())
	store.Set(types.AccountKey(account.Address), account.Marshal())
	if !position.ID.IsZero() {
		store.Set(types.PositionKey(position.ID), position.Marshal())
		result.PositionID = position.ID
	}
	summary, err := k.RefreshMarginRatio(ctx, fill.Account)
	if err != nil {
		return PositionFillResult{}, err
	}
	equity := summary.Equity.BigInt()
	if !position.ID.IsZero() {
		store.Delete(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID))
		position.RankScore = positionRank(position, equity)
		store.Set(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID),
			[]byte{})
		store.Set(types.PositionKey(position.ID), position.Marshal())
	}
	if modified {
		event, err := k.positionModifiedEvent(ctx, fill.Account, position, equity)
		if err != nil {
			return PositionFillResult{}, err
		}
		result.Events = append(result.Events, event)
	}
	return result, nil
}

// positionKindTradeType is the tradeType a liquidation, ADL or backstop fill
// carries whatever its netting.
func positionKindTradeType(kind FillKind) (uint8, bool) {
	switch kind {
	case FillKindLiquidation:
		return TradeTypeLiquidation, true
	case FillKindADL:
		return TradeTypeADL, true
	case FillKindBackstop:
		return TradeTypeBackstop, true
	default:
		return 0, false
	}
}

// positionSettle settles the position at price: the funding since its funding
// basis at the cumulative index, then side x (size x price / 1e18 - cost
// basis), the new cost basis rounded down for a long and up for a short and
// the new funding basis up for a long and down for a short, so each rounds
// toward the venue. The reference price becomes price.
func positionSettle(account *types.Account, position *types.Position, price, fundingIndex uint256.Int,
	books *types.VenueBooks, result *PositionFillResult) (TradeSettlement, error) {
	before := account.Collateral.ToBig()
	size := position.SizeBase.ToBig()

	received, funding, err := positionSettleFunding(account, position, fundingIndex, books)
	if err != nil {
		return TradeSettlement{}, err
	}
	if funding != nil {
		result.Events = append(result.Events, *funding)
	}

	cost := positionRound(new(big.Int).Mul(size, price.ToBig()), !position.IsLong)
	pnl := new(big.Int).Sub(cost, position.CostBasisUsd.ToBig())
	if !position.IsLong {
		pnl.Neg(pnl)
	}
	if err := positionBook(account, books, pnl); err != nil {
		return TradeSettlement{}, err
	}
	if position.CostBasisUsd, err = positionWord(cost); err != nil {
		return TradeSettlement{}, err
	}
	position.ReferencePrice = price
	return TradeSettlement{
		PositionID:      position.ID,
		GrossPnlUsd:     pnl,
		FundingUsd:      received,
		CollateralDelta: new(big.Int).Sub(account.Collateral.ToBig(), before),
	}, nil
}

// positionSettleFunding books the funding of the position since its funding
// basis at the cumulative index into the account's collateral, the new
// funding basis rounded up for a long and down for a short, toward the venue.
// It returns the funding received (negative when paid) and
// PositionFundingApplied when that is non-zero.
func positionSettleFunding(account *types.Account, position *types.Position, fundingIndex uint256.Int,
	books *types.VenueBooks) (*big.Int, *types.EventRecord, error) {
	basis := positionRound(new(big.Int).Mul(position.SizeBase.ToBig(), signedBig(fundingIndex)), position.IsLong)
	received := new(big.Int).Sub(basis, signedBig(position.FundingBasis))
	if position.IsLong {
		received.Neg(received)
	}
	if err := positionBook(account, books, received); err != nil {
		return nil, nil, err
	}
	fundingBasis, err := positionSignedWord(basis)
	if err != nil {
		return nil, nil, err
	}
	position.FundingBasis = fundingBasis
	if received.Sign() == 0 {
		return received, nil, nil
	}
	return received, &types.EventRecord{Name: types.EventPositionFundingApplied, Args: []any{
		position.ID.ToBig(), position.Account, new(big.Int).SetUint64(position.MarketID), new(big.Int).Set(received),
		new(big.Int).Mul(account.Collateral.ToBig(), positionUsdlUnit), account.Collateral.ToBig(),
	}}, nil
}

// positionBook books amountUsd into the account's collateral: a credit
// rounded down and a debit rounded up to the USDL unit, the remainder to
// residueUsd; a debit above the collateral leaves it at 0 and records the
// rest as the account's deficit.
func positionBook(account *types.Account, books *types.VenueBooks, amountUsd *big.Int) error {
	collateral := account.Collateral.ToBig()
	residue := new(big.Int)
	switch amountUsd.Sign() {
	case 0:
		return nil
	case 1:
		units, remainder := new(big.Int).QuoRem(amountUsd, positionUsdlUnit, new(big.Int))
		collateral.Add(collateral, units)
		residue.Set(remainder)
	default:
		loss := new(big.Int).Neg(amountUsd)
		units := positionCeilDiv(loss, positionUsdlUnit)
		residue.Sub(new(big.Int).Mul(units, positionUsdlUnit), loss)
		if collateral.Cmp(units) >= 0 {
			collateral.Sub(collateral, units)
		} else {
			shortfall := new(big.Int).Mul(new(big.Int).Sub(units, collateral), positionUsdlUnit)
			collateral.SetInt64(0)
			deficit, err := positionWord(new(big.Int).Add(account.DeficitUsd.ToBig(), shortfall))
			if err != nil {
				return err
			}
			deficitSum, err := positionWord(new(big.Int).Add(books.DeficitSumUsd.ToBig(), shortfall))
			if err != nil {
				return err
			}
			account.DeficitUsd, books.DeficitSumUsd = deficit, deficitSum
		}
	}
	updated, err := positionWord(collateral)
	if err != nil {
		return err
	}
	residueSum, err := positionWord(new(big.Int).Add(books.ResidueUsd.ToBig(), residue))
	if err != nil {
		return err
	}
	account.Collateral, books.ResidueUsd = updated, residueSum
	return nil
}

// positionOpen opens a position of size for the fill at the fill price, with
// cost basis notional and funding basis fundingShare, and records it in the
// account's market index.
func (k *Keeper) positionOpen(ctx sdk.Context, account *types.Account, fill PositionFill, params types.Params,
	size uint256.Int, notional, fundingShare *big.Int, result *PositionFillResult) (types.Position, error) {
	if fill.Leverage.IsZero() {
		return types.Position{}, fmt.Errorf("%w: zero leverage opening a position", types.ErrZeroAmount)
	}
	id, err := k.NextPositionID(ctx)
	if err != nil {
		return types.Position{}, err
	}
	now, err := blockTimeSeconds(ctx)
	if err != nil {
		return types.Position{}, err
	}
	cost, err := positionWord(notional)
	if err != nil {
		return types.Position{}, err
	}
	fundingBasis, err := positionSignedWord(fundingShare)
	if err != nil {
		return types.Position{}, err
	}
	position := types.Position{
		ID:             id,
		Account:        fill.Account,
		MarketID:       fill.MarketID,
		IsLong:         fill.IsLong,
		SizeBase:       size,
		ReferencePrice: fill.Price,
		CostBasisUsd:   cost,
		FundingBasis:   fundingBasis,
		EntryPrice:     fill.Price,
		Leverage:       fill.Leverage,
		OpenedAt:       now,
	}
	account.PositionCount++
	idBytes := id.Bytes32()
	k.store(ctx).Set(types.AccountMarketPositionKey(fill.Account, fill.MarketID), idBytes[:])
	result.Events = append(result.Events, types.EventRecord{Name: types.EventPositionOpened, Args: []any{
		id.ToBig(), fill.Account, new(big.Int).SetUint64(fill.MarketID), fill.IsLong, new(big.Int).Set(notional),
		fill.Leverage.ToBig(), fill.Price.ToBig(), params.CollateralToken, account.Collateral.ToBig(),
	}})
	result.Trades = append(result.Trades, TradeSettlement{
		PositionID:      id,
		TradeType:       TradeTypeOpen,
		SizeUsd:         cost,
		GrossPnlUsd:     new(big.Int),
		FundingUsd:      new(big.Int),
		CollateralDelta: new(big.Int),
	})
	return position, nil
}

// positionIncrease adds size at price to a settled position: the cost basis
// and the funding basis take the fill's shares and the displayed entry
// becomes the size-weighted average rounded half up.
func positionIncrease(position *types.Position, size, price, notional, fundingShare *big.Int,
	result *PositionFillResult) error {
	held := position.SizeBase.ToBig()
	total := new(big.Int).Add(held, size)
	weighted := new(big.Int).Mul(held, position.EntryPrice.ToBig())
	weighted.Add(weighted, new(big.Int).Mul(size, price))
	weighted.Mul(weighted, big.NewInt(2))
	weighted.Add(weighted, total)
	entry := weighted.Quo(weighted, new(big.Int).Mul(total, big.NewInt(2)))

	newSize, err := positionWord(total)
	if err != nil {
		return err
	}
	cost, err := positionWord(new(big.Int).Add(position.CostBasisUsd.ToBig(), notional))
	if err != nil {
		return err
	}
	fundingBasis, err := positionSignedWord(new(big.Int).Add(signedBig(position.FundingBasis), fundingShare))
	if err != nil {
		return err
	}
	entryWord, err := positionWord(entry)
	if err != nil {
		return err
	}
	result.Events = append(result.Events, types.EventRecord{Name: types.EventPositionEntryPriceChanged, Args: []any{
		position.ID.ToBig(), position.EntryPrice.ToBig(), entry,
	}})
	position.SizeBase, position.CostBasisUsd, position.FundingBasis, position.EntryPrice =
		newSize, cost, fundingBasis, entryWord
	return nil
}

// positionReduce takes size off a settled position with the fill's shares of
// the cost basis and the funding basis; the displayed entry is kept.
func positionReduce(position *types.Position, size, notional, fundingShare *big.Int) error {
	newSize, err := positionWord(new(big.Int).Sub(position.SizeBase.ToBig(), size))
	if err != nil {
		return err
	}
	cost, err := positionWord(new(big.Int).Sub(position.CostBasisUsd.ToBig(), notional))
	if err != nil {
		return err
	}
	fundingBasis, err := positionSignedWord(new(big.Int).Sub(signedBig(position.FundingBasis), fundingShare))
	if err != nil {
		return err
	}
	position.SizeBase, position.CostBasisUsd, position.FundingBasis = newSize, cost, fundingBasis
	return nil
}

// positionRemove closes a settled position in full: what its cost basis and
// funding basis keep beyond the fill's shares, the rounding of its last
// settlement, goes to residueUsd, and the position, its market index, its
// rank entry and its liquidation cooldown are deleted.
func (k *Keeper) positionRemove(ctx sdk.Context, account *types.Account, position types.Position, notional,
	fundingShare *big.Int, books *types.VenueBooks) error {
	leftover := new(big.Int).Sub(signedBig(position.FundingBasis), fundingShare)
	leftover.Sub(leftover, new(big.Int).Sub(position.CostBasisUsd.ToBig(), notional))
	if !position.IsLong {
		leftover.Neg(leftover)
	}
	if leftover.Sign() < 0 {
		return fmt.Errorf("%w: closing position %s leaves %s USD below its shares", types.ErrInvalidParams,
			position.ID.Dec(), leftover)
	}
	residue, err := positionWord(new(big.Int).Add(books.ResidueUsd.ToBig(), leftover))
	if err != nil {
		return err
	}
	if account.PositionCount == 0 {
		return fmt.Errorf("%w: position %s of %s is not counted", types.ErrInvalidParams, position.ID.Dec(),
			account.Address)
	}
	books.ResidueUsd = residue
	account.PositionCount--
	store := k.store(ctx)
	store.Delete(types.PositionKey(position.ID))
	store.Delete(types.AccountMarketPositionKey(position.Account, position.MarketID))
	store.Delete(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID))
	store.Delete(types.LiquidationCooldownKey(position.ID))
	return nil
}

// positionClosedEvent is PositionClosed for closed base at exit price, the
// realised PnL of lx_perps_pnl_compute against the displayed entry:
// closed x (exit - entry) x side / 1e18, a loss rounded up and a profit down.
func positionClosedEvent(position types.Position, closed *big.Int, exit uint256.Int, notional *big.Int,
	full bool) types.EventRecord {
	pnl := new(big.Int).Mul(closed, new(big.Int).Sub(exit.ToBig(), position.EntryPrice.ToBig()))
	if !position.IsLong {
		pnl.Neg(pnl)
	}
	pnl.Div(pnl, positionWad)
	return types.EventRecord{Name: types.EventPositionClosed, Args: []any{
		position.ID.ToBig(), position.Account, new(big.Int).SetUint64(position.MarketID), new(big.Int).Set(notional),
		exit.ToBig(), pnl, full,
	}}
}

// positionModifiedEvent is PositionModified for the position: its size at its
// reference price and its share of the account's equity, weighted by the
// notional of the account's positions at their reference prices, in USD and
// in USDL units rounded down; 0 for an account without positive equity.
func (k *Keeper) positionModifiedEvent(ctx sdk.Context, account common.Address, position types.Position,
	equity *big.Int) (types.EventRecord, error) {
	own := positionNotionalAt(position)
	total := new(big.Int)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), positionAccountPrefix(account))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		held, err := k.positionByID(ctx, iterator.Value())
		if err != nil {
			return types.EventRecord{}, err
		}
		total.Add(total, positionNotionalAt(held))
	}
	share := new(big.Int)
	if equity.Sign() > 0 && total.Sign() > 0 {
		share.Div(new(big.Int).Mul(equity, own), total)
	}
	return types.EventRecord{Name: types.EventPositionModified, Args: []any{
		position.ID.ToBig(), own, share, new(big.Int).Quo(share, positionUsdlUnit),
	}}, nil
}

// positionRank is the ADL rank score of the position at its reference price:
// (price / entry for a long, entry / price for a short) x notional / equity in
// 1e18 fixed point, one floor division, saturating at 2^256-1 and 0 for an
// account without positive equity.
func positionRank(position types.Position, equity *big.Int) uint256.Int {
	var rank uint256.Int
	if equity.Sign() <= 0 || position.EntryPrice.IsZero() || position.ReferencePrice.IsZero() {
		return rank
	}
	numerator := new(big.Int).Mul(positionNotionalAt(position), positionWad)
	denominator := new(big.Int).Set(equity)
	if position.IsLong {
		numerator.Mul(numerator, position.ReferencePrice.ToBig())
		denominator.Mul(denominator, position.EntryPrice.ToBig())
	} else {
		numerator.Mul(numerator, position.EntryPrice.ToBig())
		denominator.Mul(denominator, position.ReferencePrice.ToBig())
	}
	score := numerator.Quo(numerator, denominator)
	if score.Cmp(positionMaxWord) > 0 {
		score.Set(positionMaxWord)
	}
	rank.SetFromBig(score)
	return rank
}

// ClosePosition returns the reduce-only MARKET IOC order that closes the
// caller's position in full, sized at mark; the caller places it, so it
// fills at the default slippage bound against the book.
func (k *Keeper) ClosePosition(ctx sdk.Context, caller common.Address, positionID uint256.Int) (types.Order, error) {
	position, mark, err := k.positionToClose(ctx, caller, positionID)
	if err != nil {
		return types.Order{}, err
	}
	return positionCloseOrder(position, position.SizeBase, mark)
}

// PartialClose returns the reduce-only MARKET IOC order that closes
// closeSizeUsd of the caller's position at mark, rounded down to the lot. It
// refuses a zero size, a size above the position notional at mark with
// ErrCloseSizeExceedsPosition and a size below one lot with ErrLotSize.
func (k *Keeper) PartialClose(ctx sdk.Context, caller common.Address, positionID, closeSizeUsd uint256.Int) (types.Order, error) {
	if closeSizeUsd.IsZero() {
		return types.Order{}, fmt.Errorf("%w: zero close size", types.ErrZeroAmount)
	}
	position, mark, err := k.positionToClose(ctx, caller, positionID)
	if err != nil {
		return types.Order{}, err
	}
	notional := new(big.Int).Div(new(big.Int).Mul(position.SizeBase.ToBig(), mark.ToBig()), positionWad)
	if closeSizeUsd.ToBig().Cmp(notional) > 0 {
		return types.Order{}, fmt.Errorf("%w: %s USD of position %s worth %s USD at mark", types.ErrCloseSizeExceedsPosition,
			closeSizeUsd.Dec(), positionID.Dec(), notional)
	}
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return types.Order{}, err
	}
	lot := positionLot(market)
	base := new(big.Int).Div(new(big.Int).Mul(closeSizeUsd.ToBig(), positionWad), mark.ToBig())
	base.Sub(base, new(big.Int).Mod(base, lot))
	if base.Sign() == 0 {
		return types.Order{}, fmt.Errorf("%w: %s USD at mark %s is below one lot of %s", types.ErrLotSize,
			closeSizeUsd.Dec(), mark.Dec(), lot)
	}
	var size uint256.Int
	size.SetFromBig(base)
	return positionCloseOrder(position, size, mark)
}

func (k *Keeper) positionToClose(ctx sdk.Context, caller common.Address, positionID uint256.Int) (types.Position, uint256.Int, error) {
	idBytes := positionID.Bytes32()
	position, err := k.positionByID(ctx, idBytes[:])
	if err != nil {
		return types.Position{}, uint256.Int{}, err
	}
	if position.Account != caller {
		return types.Position{}, uint256.Int{}, fmt.Errorf("%w: position %s", types.ErrNotPositionHolder, positionID.Dec())
	}
	state, err := k.GetMarketState(ctx, position.MarketID)
	if err != nil {
		return types.Position{}, uint256.Int{}, err
	}
	if state.MarkPrice.IsZero() {
		return types.Position{}, uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale,
			position.MarketID)
	}
	return position, state.MarkPrice, nil
}

func positionCloseOrder(position types.Position, size, mark uint256.Int) (types.Order, error) {
	sizeUsd, err := positionWord(new(big.Int).Div(new(big.Int).Mul(size.ToBig(), mark.ToBig()), positionWad))
	if err != nil {
		return types.Order{}, err
	}
	return types.Order{
		Account:     position.Account,
		MarketID:    position.MarketID,
		IsLong:      !position.IsLong,
		Type:        types.OrderTypeMarket,
		TimeInForce: types.TimeInForceIOC,
		ReduceOnly:  true,
		SizeUsd:     sizeUsd,
		SizeBase:    size,
		Leverage:    position.Leverage,
	}, nil
}

// ReconcileReduceOnly runs after each fill of the account in the market: a
// resting reduce-only order is cancelled when the account holds no position
// there or one on the order's own side, and shrunk to the position size when
// its remainder is larger, its notional recomputed at its limit price rounded
// down. It returns one OrderCancelled record per cancelled order.
func (k *Keeper) ReconcileReduceOnly(ctx sdk.Context, account common.Address, marketID uint64) ([]types.EventRecord, error) {
	position, exists, err := k.positionOf(ctx, account, marketID)
	if err != nil {
		return nil, err
	}
	orders, err := k.positionReduceOnlyOrders(ctx, account, marketID)
	if err != nil {
		return nil, err
	}
	var events []types.EventRecord
	for _, order := range orders {
		if !exists || position.IsLong == order.IsLong {
			if err := k.positionCancelOrder(ctx, order); err != nil {
				return nil, err
			}
			events = append(events, types.EventRecord{Name: types.EventOrderCancelled, Args: []any{order.ID.ToBig(), order.Account}})
			continue
		}
		var remaining uint256.Int
		if _, underflow := remaining.SubOverflow(&order.SizeBase, &order.FilledBase); underflow {
			return nil, fmt.Errorf("%w: order %s filled above its size", types.ErrInvalidParams, order.ID.Dec())
		}
		if !remaining.Gt(&position.SizeBase) {
			continue
		}
		size, err := positionWord(new(big.Int).Add(order.FilledBase.ToBig(), position.SizeBase.ToBig()))
		if err != nil {
			return nil, err
		}
		sizeUsd, err := positionWord(new(big.Int).Div(new(big.Int).Mul(size.ToBig(), order.LimitPrice.ToBig()), positionWad))
		if err != nil {
			return nil, err
		}
		order.SizeBase, order.SizeUsd = size, sizeUsd
		k.store(ctx).Set(types.OrderKey(order.ID), order.Marshal())
	}
	return events, nil
}

// positionReduceOnlyOrders lists the account's resting reduce-only orders in
// the market in ascending order id.
func (k *Keeper) positionReduceOnlyOrders(ctx sdk.Context, account common.Address, marketID uint64) ([]types.Order, error) {
	store := k.store(ctx)
	prefix := types.AccountOrderPrefixFor(account)
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()
	var orders []types.Order
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+32 {
			return nil, fmt.Errorf("%w: account order key %x", types.ErrInvalidParams, key)
		}
		var id uint256.Int
		id.SetBytes32(key[len(prefix):])
		bz := store.Get(types.OrderKey(id))
		if bz == nil {
			return nil, fmt.Errorf("%w: indexed order %s missing", types.ErrInvalidParams, id.Dec())
		}
		var order types.Order
		if err := order.Unmarshal(bz); err != nil {
			return nil, fmt.Errorf("%w: corrupt order %s: %v", types.ErrInvalidParams, id.Dec(), err)
		}
		if order.ID != id || order.Account != account {
			return nil, fmt.Errorf("%w: order %s does not match its index", types.ErrInvalidParams, id.Dec())
		}
		if order.MarketID == marketID && order.ReduceOnly && order.Status == types.OrderStatusResting {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

// positionCancelOrder takes a resting order out of the book with status
// CANCELLED, frees its account order, cloid and expiry entries and counts it
// out of the account's open orders.
func (k *Keeper) positionCancelOrder(ctx sdk.Context, order types.Order) error {
	holder, err := k.positionAccount(ctx, order.Account)
	if err != nil {
		return err
	}
	if holder.OpenOrderCount == 0 {
		return fmt.Errorf("%w: order %s of %s is not counted open", types.ErrInvalidParams, order.ID.Dec(), order.Account)
	}
	order.Status = types.OrderStatusCancelled
	if err := k.BookRemove(ctx, order); err != nil {
		return err
	}
	holder.OpenOrderCount--
	store := k.store(ctx)
	store.Set(types.AccountKey(order.Account), holder.Marshal())
	store.Delete(types.AccountOrderKey(order.Account, order.ID))
	if order.Cloid != ([16]byte{}) {
		store.Delete(types.CloidKey(order.Account, order.MarketID, order.Cloid))
	}
	if order.Deadline != 0 {
		store.Delete(types.ExpiryKey(order.Deadline, order.ID))
	}
	return nil
}

// positionAccount returns the stored account, ErrNoAccount when none was
// created.
func (k *Keeper) positionAccount(ctx sdk.Context, address common.Address) (types.Account, error) {
	bz := k.store(ctx).Get(types.AccountKey(address))
	if bz == nil {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, address)
	}
	var account types.Account
	if err := account.Unmarshal(bz); err != nil {
		return types.Account{}, fmt.Errorf("%w: corrupt account %s: %v", types.ErrInvalidParams, address, err)
	}
	if !account.Created || account.Address != address {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, address)
	}
	return account, nil
}

// positionOf reads the account's position in the market through the
// AccountMarketPosition index.
func (k *Keeper) positionOf(ctx sdk.Context, account common.Address, marketID uint64) (types.Position, bool, error) {
	idBytes := k.store(ctx).Get(types.AccountMarketPositionKey(account, marketID))
	if idBytes == nil {
		return types.Position{}, false, nil
	}
	position, err := k.positionByID(ctx, idBytes)
	if err != nil {
		return types.Position{}, false, err
	}
	if position.Account != account || position.MarketID != marketID {
		return types.Position{}, false, fmt.Errorf("%w: position %s does not match its index", types.ErrInvalidParams,
			position.ID.Dec())
	}
	return position, true, nil
}

// positionByID reads the position whose 32-byte id is idBytes,
// ErrUnknownPosition when none is open.
func (k *Keeper) positionByID(ctx sdk.Context, idBytes []byte) (types.Position, error) {
	if len(idBytes) != 32 {
		return types.Position{}, fmt.Errorf("%w: position id of %d bytes", types.ErrInvalidParams, len(idBytes))
	}
	var id uint256.Int
	id.SetBytes32(idBytes)
	bz := k.store(ctx).Get(types.PositionKey(id))
	if bz == nil {
		return types.Position{}, fmt.Errorf("%w: %s", types.ErrUnknownPosition, id.Dec())
	}
	var position types.Position
	if err := position.Unmarshal(bz); err != nil {
		return types.Position{}, fmt.Errorf("%w: corrupt position %s: %v", types.ErrInvalidParams, id.Dec(), err)
	}
	if position.ID != id {
		return types.Position{}, fmt.Errorf("%w: position key %s holds position %s", types.ErrInvalidParams, id.Dec(),
			position.ID.Dec())
	}
	return position, nil
}

func positionAccountPrefix(account common.Address) []byte {
	return append(append([]byte(nil), types.AccountMarketPositionPrefix...), account.Bytes()...)
}

func (k *Keeper) positionVenueBooks(ctx sdk.Context) (types.VenueBooks, error) {
	var books types.VenueBooks
	bz := k.store(ctx).Get(types.VenueBooksKey)
	if bz == nil {
		return books, nil
	}
	if err := books.Unmarshal(bz); err != nil {
		return types.VenueBooks{}, fmt.Errorf("%w: corrupt venue books: %v", types.ErrInvalidParams, err)
	}
	return books, nil
}

// positionLot is the lot of the market, 10^(18 - szDecimals) base units.
func positionLot(market types.Market) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals)-int64(market.SzDecimals)), nil)
}

func positionCheckLot(market types.Market, size uint256.Int) error {
	lot := positionLot(market)
	if new(big.Int).Mod(size.ToBig(), lot).Sign() != 0 {
		return fmt.Errorf("%w: fill of %s base is not a multiple of %s", types.ErrLotSize, size.Dec(), lot)
	}
	return nil
}

// positionNotionalAt is size x reference price / 1e18, rounded down.
func positionNotionalAt(position types.Position) *big.Int {
	notional := new(big.Int).Mul(position.SizeBase.ToBig(), position.ReferencePrice.ToBig())
	return notional.Div(notional, positionWad)
}

// positionRound divides a 1e36-scaled product by 1e18, rounding up when up
// is set and down otherwise.
func positionRound(product *big.Int, up bool) *big.Int {
	if up {
		return new(big.Int).Neg(new(big.Int).Div(new(big.Int).Neg(product), positionWad))
	}
	return new(big.Int).Div(product, positionWad)
}

func positionCeilDiv(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

// positionWord writes a non-negative value below 2^256 as a word.
func positionWord(value *big.Int) (uint256.Int, error) {
	var word uint256.Int
	if value.Sign() < 0 || word.SetFromBig(value) {
		return uint256.Int{}, fmt.Errorf("%w: %s outside uint256", types.ErrOverflow, value)
	}
	return word, nil
}

// positionSignedWord writes value as a two's complement int256 word.
func positionSignedWord(value *big.Int) (uint256.Int, error) {
	if value.Cmp(two255) >= 0 || value.Cmp(new(big.Int).Neg(two255)) < 0 {
		return uint256.Int{}, fmt.Errorf("%w: %s outside int256", types.ErrOverflow, value)
	}
	unsigned := new(big.Int).Set(value)
	if unsigned.Sign() < 0 {
		unsigned.Add(unsigned, two256)
	}
	var word uint256.Int
	word.SetFromBig(unsigned)
	return word, nil
}
