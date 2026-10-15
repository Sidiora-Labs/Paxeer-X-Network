package keeper

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Insurance sources of InsuranceContribution, numbered as contracts/perps.md
// section 2.
const (
	InsuranceSourceBackstopMargin uint8 = 1
	InsuranceSourceBackstopProfit uint8 = 2
)

// insuranceUsdPerUsdl is 1e12, the 18-decimal USD value of one USDL unit.
var insuranceUsdPerUsdl = new(big.Int).Exp(big.NewInt(10), big.NewInt(types.USDDecimals-types.CollateralDecimals), nil)

// insuranceInt256Min and insuranceInt256Max bound a signed funding basis.
var (
	insuranceInt256Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
	insuranceInt256Min = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
)

// BackstopCloseResult is the outcome of one EndBlock pass over the insurance
// fund's backstop positions: the closing orders sent, the insurance events of
// those closes and the positions whose close failed and was rolled back, each
// error naming its position.
type BackstopCloseResult struct {
	Slices   uint64
	Events   []types.EventRecord
	Failures []error
}

// Backstop hands an account whose equity is below Params.BackstopRatioNumerator
// / Params.BackstopRatioDenominator (2/3) of its maintenance margin to the
// insurance fund: the account and the fund are settled at mark, the account's
// open orders are cancelled, perps_insurance takes over every position at
// mark, netting it against a position the fund already holds in the market,
// and receives the remaining collateral. The account is left with zero
// positions and zero collateral; a deficit it carries is covered at once by
// CoverDeficit. It returns BackstopLiquidation per position, the
// OpenInterestChanged of a netting, InsuranceContribution source
// BACKSTOP_MARGIN for the collateral and the events of the cover.
func (k *Keeper) Backstop(ctx sdk.Context, address common.Address) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	insuranceAddress := k.InsuranceAccountAddress()
	if address == insuranceAddress {
		return nil, fmt.Errorf("%w: the insurance fund is never backstopped", types.ErrNotLiquidatable)
	}
	positions, err := k.liquidationAccountPositions(ctx, address)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return nil, fmt.Errorf("%w: account %s holds no position", types.ErrNotLiquidatable, address)
	}
	for _, position := range positions {
		if err := k.RequireFresh(ctx, position.MarketID); err != nil {
			return nil, err
		}
	}
	events, err := k.Settle(ctx, address)
	if err != nil {
		return nil, err
	}
	below, err := k.belowBackstopLine(ctx, params, address)
	if err != nil {
		return nil, err
	}
	if !below {
		return nil, fmt.Errorf("%w: account %s is above the backstop line", types.ErrNotLiquidatable, address)
	}
	orderIDs, err := k.liquidationOpenOrders(ctx, address)
	if err != nil {
		return nil, err
	}
	for _, orderID := range orderIDs {
		cancelled, err := k.CancelOrder(ctx, address, orderID)
		if err != nil {
			return nil, err
		}
		events = append(events, cancelled)
	}
	if _, err := k.insuranceFundAccount(ctx); err != nil {
		return nil, err
	}
	fundSettled, err := k.Settle(ctx, insuranceAddress)
	if err != nil {
		return nil, err
	}
	events = append(events, fundSettled...)

	if positions, err = k.liquidationAccountPositions(ctx, address); err != nil {
		return nil, err
	}
	account, err := k.liquidationAccount(ctx, address)
	if err != nil {
		return nil, err
	}
	fund, err := k.insuranceFundAccount(ctx)
	if err != nil {
		return nil, err
	}
	now, err := liquidationNow(ctx)
	if err != nil {
		return nil, err
	}
	marks := make([]uint256.Int, len(positions))
	notionals := make([]*big.Int, len(positions))
	for i, position := range positions {
		if marks[i], err = k.liquidationMark(ctx, position.MarketID); err != nil {
			return nil, err
		}
		notionals[i] = liquidationNotional(position.SizeBase, marks[i])
	}
	takenUsd := new(big.Int).Mul(account.Collateral.ToBig(), insuranceUsdPerUsdl)
	shares := insuranceShares(takenUsd, notionals)

	for i, position := range positions {
		oiEvent, err := k.backstopTakeOver(ctx, &account, &fund, position, marks[i], now)
		if err != nil {
			return nil, err
		}
		if oiEvent != nil {
			events = append(events, *oiEvent)
		}
		events = append(events, types.EventRecord{Name: types.EventBackstopLiquidation, Args: []any{
			address, new(big.Int).SetUint64(position.MarketID), notionals[i], marks[i].ToBig(), shares[i],
		}})
	}
	taken := account.Collateral
	if _, overflow := fund.Collateral.AddOverflow(&fund.Collateral, &taken); overflow {
		return nil, fmt.Errorf("%w: insurance collateral", types.ErrOverflow)
	}
	account.Collateral = uint256.Int{}
	store := k.store(ctx)
	store.Set(types.AccountKey(address), account.Marshal())
	store.Set(types.AccountKey(insuranceAddress), fund.Marshal())
	if !taken.IsZero() {
		events = append(events, insuranceContribution(params, taken, InsuranceSourceBackstopMargin))
	}
	if _, err := k.liquidationRebucket(ctx, address); err != nil {
		return nil, err
	}
	if err := k.insuranceRerank(ctx); err != nil {
		return nil, err
	}
	if _, err := k.liquidationRebucket(ctx, insuranceAddress); err != nil {
		return nil, err
	}
	if !account.DeficitUsd.IsZero() {
		cover, err := k.CoverDeficit(ctx, address, positions[0].MarketID)
		if err != nil {
			return nil, err
		}
		events = append(events, cover...)
	}
	return events, nil
}

// CoverDeficit draws the recorded deficit of the account from the
// perps_insurance store balance, the role of lx_perps_insurance_cover: the
// USDL amount is the deficit rounded up, the rounding surplus going to
// residueUsd, and InsuranceDraw(marketID, amount, new balance) is emitted.
// What the fund cannot cover stays recorded in the deficit sum of VenueBooks,
// as lx_perps_deficit_record keeps it against the insurance account: the
// remainder moves to the deficit of perps_insurance, VaultDeficit is emitted
// with it in USDL units rounded up and ExecuteADL deleverages it.
func (k *Keeper) CoverDeficit(ctx sdk.Context, address common.Address, marketID uint64) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	account, err := k.liquidationAccount(ctx, address)
	if err != nil {
		return nil, err
	}
	if account.DeficitUsd.IsZero() {
		return nil, nil
	}
	insuranceAddress := k.InsuranceAccountAddress()
	fund := &account
	var insurance types.Account
	if address != insuranceAddress {
		if insurance, err = k.insuranceFundAccount(ctx); err != nil {
			return nil, err
		}
		fund = &insurance
	}
	books, err := k.insuranceVenueBooks(ctx)
	if err != nil {
		return nil, err
	}

	deficit := account.DeficitUsd.ToBig()
	need := insuranceCeilDiv(deficit, insuranceUsdPerUsdl)
	draw := new(big.Int).Set(need)
	if fund.Collateral.ToBig().Cmp(need) < 0 {
		draw = fund.Collateral.ToBig()
	}
	drawUsd := new(big.Int).Mul(draw, insuranceUsdPerUsdl)
	covered := new(big.Int).Set(drawUsd)
	if draw.Cmp(need) == 0 {
		covered.Set(deficit)
		residue := new(big.Int).Sub(drawUsd, deficit)
		if books.ResidueUsd, err = insuranceAdd(books.ResidueUsd, residue); err != nil {
			return nil, err
		}
	}
	if books.DeficitSumUsd.ToBig().Cmp(covered) < 0 {
		return nil, fmt.Errorf("%w: deficit sum %s below the deficit %s of account %s",
			types.ErrInvalidParams, books.DeficitSumUsd.Dec(), account.DeficitUsd.Dec(), address)
	}
	books.DeficitSumUsd.SetFromBig(new(big.Int).Sub(books.DeficitSumUsd.ToBig(), covered))
	fund.Collateral.SetFromBig(new(big.Int).Sub(fund.Collateral.ToBig(), draw))
	remainder := new(big.Int).Sub(deficit, covered)
	account.DeficitUsd.SetFromBig(remainder)

	var events []types.EventRecord
	if draw.Sign() > 0 {
		events = append(events, types.EventRecord{Name: types.EventInsuranceDraw, Args: []any{
			new(big.Int).SetUint64(marketID), draw, fund.Collateral.ToBig(),
		}})
	}
	if remainder.Sign() > 0 {
		events = append(events, types.EventRecord{Name: types.EventVaultDeficit, Args: []any{
			params.CollateralToken, insuranceCeilDiv(remainder, insuranceUsdPerUsdl),
		}})
		if address != insuranceAddress {
			if insurance.DeficitUsd, err = insuranceAdd(insurance.DeficitUsd, remainder); err != nil {
				return nil, err
			}
			account.DeficitUsd = uint256.Int{}
		}
	}
	store := k.store(ctx)
	store.Set(types.AccountKey(address), account.Marshal())
	if address != insuranceAddress {
		store.Set(types.AccountKey(insuranceAddress), insurance.Marshal())
	}
	store.Set(types.VenueBooksKey, books.Marshal())
	if _, err := k.liquidationRebucket(ctx, address); err != nil {
		return nil, err
	}
	if address != insuranceAddress {
		if _, err := k.liquidationRebucket(ctx, insuranceAddress); err != nil {
			return nil, err
		}
	}
	if remainder.Sign() > 0 {
		deficitIDs, marks, err := k.insuranceAdlInputs(ctx, insuranceAddress)
		if err != nil {
			return nil, err
		}
		adl, err := k.ExecuteADL(ctx, deficitIDs, marks)
		if err != nil {
			return nil, err
		}
		events = append(events, adl...)
	}
	return events, nil
}

// insuranceAdlInputs lists the positions of the account carrying the uncovered
// deficit in ascending id, the order ExecuteADL requires, with the stored mark
// of each of their markets, the mark of the last completed price update.
func (k *Keeper) insuranceAdlInputs(ctx sdk.Context, address common.Address) ([]uint256.Int, map[uint64]uint256.Int, error) {
	positions, err := k.liquidationAccountPositions(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]uint256.Int, 0, len(positions))
	marks := make(map[uint64]uint256.Int, len(positions))
	for _, position := range positions {
		state, err := k.GetMarketState(ctx, position.MarketID)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, position.ID)
		marks[position.MarketID] = state.MarkPrice
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Lt(&ids[j]) })
	return ids, marks, nil
}

// CloseBackstopPositions is EndBlock step 9: it closes the insurance fund's
// backstop positions against the book with reduce-only IOC market orders
// under the slice rule of liquidation (a position above
// Params.LiquidationSliceThresholdUsd closes Params.LiquidationSlicePpm of its
// size at most once per Params.LiquidationCooldownSeconds), at most
// Params.SlicesPerBlock orders. A position of a market without a fresh price
// or without fillable liquidity waits for a later block. Profit realised by a
// close is booked as InsuranceContribution source BACKSTOP_PROFIT, a loss as
// InsuranceDraw, and a loss beyond the fund goes to CoverDeficit.
func (k *Keeper) CloseBackstopPositions(ctx sdk.Context) (BackstopCloseResult, error) {
	var result BackstopCloseResult
	params, err := k.GetParams(ctx)
	if err != nil {
		return result, err
	}
	now, err := liquidationNow(ctx)
	if err != nil {
		return result, err
	}
	positions, err := k.liquidationAccountPositions(ctx, k.InsuranceAccountAddress())
	if err != nil {
		return result, err
	}
	for _, position := range positions {
		if result.Slices >= params.SlicesPerBlock {
			break
		}
		fresh, err := k.liquidationFresh(ctx, []types.Position{position})
		if err != nil {
			return result, err
		}
		if !fresh {
			continue
		}
		large, closeBase, err := k.liquidationCloseSize(ctx, params, position)
		if err != nil {
			return result, err
		}
		if large {
			cooling, err := k.liquidationCooling(ctx, params, position.ID, now)
			if err != nil {
				return result, err
			}
			if cooling {
				continue
			}
		}
		cacheCtx, write := ctx.CacheContext()
		events, err := k.backstopClose(cacheCtx, params, position, closeBase, large, now)
		if errors.Is(err, types.ErrNoLiquidity) || errors.Is(err, types.ErrSlippageExceeded) {
			continue
		}
		if err != nil {
			result.Failures = append(result.Failures, fmt.Errorf("position %s: %w", position.ID.Dec(), err))
			continue
		}
		write()
		result.Slices++
		result.Events = append(result.Events, events...)
	}
	return result, nil
}

// backstopClose matches one reduce-only IOC market order of closeBase of the
// fund against the book, keeps the slice time of a large position that stays
// open, and books the change of the fund's collateral and deficit.
func (k *Keeper) backstopClose(ctx sdk.Context, params types.Params, position types.Position, closeBase uint256.Int,
	large bool, now uint64) ([]types.EventRecord, error) {
	market, err := k.GetMarket(ctx, position.MarketID)
	if err != nil {
		return nil, err
	}
	before, err := k.insuranceFundAccount(ctx)
	if err != nil {
		return nil, err
	}
	order := types.Order{
		Account:     position.Account,
		MarketID:    position.MarketID,
		IsLong:      !position.IsLong,
		Type:        types.OrderTypeMarket,
		TimeInForce: types.TimeInForceIOC,
		Status:      types.OrderStatusResting,
		ReduceOnly:  true,
		SizeBase:    closeBase,
		Leverage:    position.Leverage,
	}
	if err := k.admitOrder(ctx, &order); err != nil {
		return nil, err
	}
	k.setOrder(ctx, order)
	matched, err := k.Match(ctx, market, order)
	if err != nil {
		return nil, err
	}
	store := k.store(ctx)
	cooldownKey := types.LiquidationCooldownKey(position.ID)
	if store.Has(types.PositionKey(position.ID)) && large {
		store.Set(cooldownKey, binary.BigEndian.AppendUint64(nil, now))
	} else {
		store.Delete(cooldownKey)
	}
	after, err := k.insuranceFundAccount(ctx)
	if err != nil {
		return nil, err
	}
	events := matched.Events
	switch after.Collateral.Cmp(&before.Collateral) {
	case 1:
		var profit uint256.Int
		profit.Sub(&after.Collateral, &before.Collateral)
		events = append(events, insuranceContribution(params, profit, InsuranceSourceBackstopProfit))
	case -1:
		var loss uint256.Int
		loss.Sub(&before.Collateral, &after.Collateral)
		events = append(events, types.EventRecord{Name: types.EventInsuranceDraw, Args: []any{
			new(big.Int).SetUint64(position.MarketID), loss.ToBig(), after.Collateral.ToBig(),
		}})
	}
	if after.DeficitUsd.Gt(&before.DeficitUsd) {
		cover, err := k.CoverDeficit(ctx, after.Address, position.MarketID)
		if err != nil {
			return nil, err
		}
		events = append(events, cover...)
	}
	return events, nil
}

// backstopTakeOver moves one settled position of the account to the fund at
// mark. Without a fund position in the market the position changes holder
// and its displayed entry becomes the mark; a fund position on the same side
// grows by it with the size-weighted entry; a fund position on the other side
// nets against it, the netted size leaving the open interest of both sides.
// Cost basis and funding basis carry over as signed sums, so the solvency
// identity is unchanged; a netting that closes both positions books what
// their bases leave to residueUsd. The account records are updated in memory
// and stored by the caller.
func (k *Keeper) backstopTakeOver(ctx sdk.Context, account, fund *types.Account, position types.Position,
	mark uint256.Int, now uint64) (*types.EventRecord, error) {
	store := k.store(ctx)
	k.insuranceDropPosition(ctx, position, account.Address)
	if account.PositionCount == 0 {
		return nil, fmt.Errorf("%w: position count of account %s", types.ErrInvalidParams, account.Address)
	}
	account.PositionCount--

	marketKey := types.AccountMarketPositionKey(fund.Address, position.MarketID)
	bz := store.Get(marketKey)
	if bz == nil {
		position.Account = fund.Address
		position.EntryPrice = mark
		position.OpenedAt = now
		store.Set(types.PositionKey(position.ID), position.Marshal())
		word := position.ID.Bytes32()
		store.Set(marketKey, word[:])
		fund.PositionCount++
		return nil, nil
	}
	if len(bz) != 32 {
		return nil, fmt.Errorf("%w: account market position entry %x", types.ErrInvalidParams, marketKey)
	}
	var heldID uint256.Int
	heldID.SetBytes32(bz)
	held, err := k.liquidationPosition(ctx, heldID)
	if err != nil {
		return nil, err
	}
	store.Delete(types.AdlRankIndexKey(held.MarketID, types.SideOf(held.IsLong), held.RankScore, held.ID))

	heldCost, takenCost := held.CostBasisUsd.ToBig(), position.CostBasisUsd.ToBig()
	heldFunding, takenFunding := insuranceSigned(held.FundingBasis), insuranceSigned(position.FundingBasis)
	if held.IsLong == position.IsLong {
		var size uint256.Int
		if _, overflow := size.AddOverflow(&held.SizeBase, &position.SizeBase); overflow {
			return nil, fmt.Errorf("%w: size of position %s", types.ErrOverflow, held.ID.Dec())
		}
		weighted := new(big.Int).Mul(held.SizeBase.ToBig(), held.EntryPrice.ToBig())
		weighted.Add(weighted, new(big.Int).Mul(position.SizeBase.ToBig(), mark.ToBig()))
		weighted.Add(weighted, new(big.Int).Rsh(size.ToBig(), 1))
		weighted.Quo(weighted, size.ToBig())
		if err := insuranceSetWords(&held, size, weighted, new(big.Int).Add(heldCost, takenCost),
			new(big.Int).Add(heldFunding, takenFunding)); err != nil {
			return nil, err
		}
		store.Set(types.PositionKey(held.ID), held.Marshal())
		return nil, nil
	}

	netted := held.SizeBase
	if position.SizeBase.Lt(&netted) {
		netted = position.SizeBase
	}
	oiEvent, err := k.CheckOpenInterest(ctx, OpenInterestFill{MarketID: position.MarketID, SizeBase: netted, PriceUsd: mark})
	if err != nil {
		return nil, err
	}
	switch held.SizeBase.Cmp(&position.SizeBase) {
	case 1:
		var size uint256.Int
		size.Sub(&held.SizeBase, &position.SizeBase)
		if err := insuranceSetWords(&held, size, held.EntryPrice.ToBig(), new(big.Int).Sub(heldCost, takenCost),
			new(big.Int).Sub(heldFunding, takenFunding)); err != nil {
			return nil, err
		}
		store.Set(types.PositionKey(held.ID), held.Marshal())
	case -1:
		var size uint256.Int
		size.Sub(&position.SizeBase, &held.SizeBase)
		held.IsLong = position.IsLong
		held.OpenedAt = now
		if err := insuranceSetWords(&held, size, mark.ToBig(), new(big.Int).Sub(takenCost, heldCost),
			new(big.Int).Sub(takenFunding, heldFunding)); err != nil {
			return nil, err
		}
		store.Set(types.PositionKey(held.ID), held.Marshal())
	default:
		// The pair adds side x (funding basis - cost basis) of the held side to
		// the solvency identity; with both gone residueUsd carries it.
		left := new(big.Int).Sub(new(big.Int).Sub(heldFunding, takenFunding), new(big.Int).Sub(heldCost, takenCost))
		if !held.IsLong {
			left.Neg(left)
		}
		if err := k.insuranceBookResidue(ctx, left); err != nil {
			return nil, err
		}
		k.insuranceDropPosition(ctx, held, fund.Address)
		if fund.PositionCount == 0 {
			return nil, fmt.Errorf("%w: position count of the insurance fund", types.ErrInvalidParams)
		}
		fund.PositionCount--
	}
	return oiEvent, nil
}

// insuranceDropPosition deletes the position record and every entry keyed by
// it: the holder's market index, its ADL rank entry, its liquidation cooldown
// and its attached TP/SL pair.
func (k *Keeper) insuranceDropPosition(ctx sdk.Context, position types.Position, holder common.Address) {
	store := k.store(ctx)
	store.Delete(types.PositionKey(position.ID))
	store.Delete(types.AccountMarketPositionKey(holder, position.MarketID))
	store.Delete(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID))
	store.Delete(types.LiquidationCooldownKey(position.ID))
	store.Delete(types.AttachedTpSlKey(position.ID))
}

// insuranceRerank rewrites the rank score and AdlRankIndex entry of every
// fund position at mark against the fund's account value: (mark / entry for a
// long, entry / mark for a short) x notional / account value, 0 when the
// account value is not positive.
func (k *Keeper) insuranceRerank(ctx sdk.Context) error {
	insuranceAddress := k.InsuranceAccountAddress()
	positions, err := k.liquidationAccountPositions(ctx, insuranceAddress)
	if err != nil {
		return err
	}
	if len(positions) == 0 {
		return nil
	}
	equity, err := k.Equity(ctx, insuranceAddress)
	if err != nil {
		return err
	}
	store := k.store(ctx)
	for _, position := range positions {
		mark, err := k.liquidationMark(ctx, position.MarketID)
		if err != nil {
			return err
		}
		score, err := insuranceRankScore(position, mark, equity.BigInt())
		if err != nil {
			return err
		}
		side := types.SideOf(position.IsLong)
		store.Delete(types.AdlRankIndexKey(position.MarketID, side, position.RankScore, position.ID))
		position.RankScore = score
		store.Set(types.PositionKey(position.ID), position.Marshal())
		store.Set(types.AdlRankIndexKey(position.MarketID, side, position.RankScore, position.ID), []byte{})
	}
	return nil
}

func insuranceRankScore(position types.Position, mark uint256.Int, accountValue *big.Int) (uint256.Int, error) {
	if accountValue.Sign() <= 0 || position.EntryPrice.IsZero() || mark.IsZero() {
		return uint256.Int{}, nil
	}
	numerator, denominator := mark.ToBig(), position.EntryPrice.ToBig()
	if !position.IsLong {
		numerator, denominator = denominator, numerator
	}
	score := new(big.Int).Mul(numerator, liquidationNotional(position.SizeBase, mark))
	score.Mul(score, usdUnit)
	score.Quo(score, new(big.Int).Mul(denominator, accountValue))
	var out uint256.Int
	if out.SetFromBig(score) {
		return uint256.Int{}, fmt.Errorf("%w: rank score of position %s", types.ErrOverflow, position.ID.Dec())
	}
	return out, nil
}

// belowBackstopLine reports whether equity x denominator < numerator x
// maintenance margin at mark.
func (k *Keeper) belowBackstopLine(ctx sdk.Context, params types.Params, address common.Address) (bool, error) {
	summary, err := k.AccountMargin(ctx, address)
	if err != nil {
		return false, err
	}
	equity := summary.Equity
	maintenance := summary.MaintenanceMargin
	left := new(big.Int).Mul(equity.BigInt(), new(big.Int).SetUint64(params.BackstopRatioDenominator))
	right := new(big.Int).Mul(maintenance.BigInt(), new(big.Int).SetUint64(params.BackstopRatioNumerator))
	return left.Cmp(right) < 0, nil
}

// insuranceFundAccount reads the perps_insurance store account, storing an
// empty created record when the fund has none yet.
func (k *Keeper) insuranceFundAccount(ctx sdk.Context) (types.Account, error) {
	address := k.InsuranceAccountAddress()
	if !k.store(ctx).Has(types.AccountKey(address)) {
		fund := types.Account{Address: address, Created: true}
		k.store(ctx).Set(types.AccountKey(address), fund.Marshal())
		return fund, nil
	}
	return k.liquidationAccount(ctx, address)
}

func (k *Keeper) insuranceVenueBooks(ctx sdk.Context) (types.VenueBooks, error) {
	var books types.VenueBooks
	bz := k.store(ctx).Get(types.VenueBooksKey)
	if bz == nil {
		return books, nil
	}
	if err := books.Unmarshal(bz); err != nil {
		return types.VenueBooks{}, fmt.Errorf("%w: venue books: %v", types.ErrInvalidParams, err)
	}
	return books, nil
}

// insuranceBookResidue adds delta, which may be negative, to residueUsd.
func (k *Keeper) insuranceBookResidue(ctx sdk.Context, delta *big.Int) error {
	if delta.Sign() == 0 {
		return nil
	}
	books, err := k.insuranceVenueBooks(ctx)
	if err != nil {
		return err
	}
	residue := new(big.Int).Add(books.ResidueUsd.ToBig(), delta)
	if residue.Sign() < 0 {
		return fmt.Errorf("%w: residue %s below the netting remainder %s",
			types.ErrInvalidParams, books.ResidueUsd.Dec(), new(big.Int).Neg(delta))
	}
	if books.ResidueUsd.SetFromBig(residue) {
		return fmt.Errorf("%w: residue", types.ErrOverflow)
	}
	k.store(ctx).Set(types.VenueBooksKey, books.Marshal())
	return nil
}

// insuranceSetWords stores size, entry, cost basis and the signed funding
// basis of a netted fund position, refusing a negative cost basis or a value
// outside its word.
func insuranceSetWords(position *types.Position, size uint256.Int, entry, cost, funding *big.Int) error {
	if cost.Sign() < 0 {
		return fmt.Errorf("%w: cost basis %s of position %s", types.ErrInvalidParams, cost, position.ID.Dec())
	}
	var entryWord, costWord uint256.Int
	if entryWord.SetFromBig(entry) || costWord.SetFromBig(cost) {
		return fmt.Errorf("%w: position %s", types.ErrOverflow, position.ID.Dec())
	}
	fundingWord, err := insuranceSignedWord(funding)
	if err != nil {
		return err
	}
	position.SizeBase = size
	position.EntryPrice = entryWord
	position.CostBasisUsd = costWord
	position.FundingBasis = fundingWord
	return nil
}

// insuranceShares splits total over the notionals pro rata, rounding down,
// with the last share taking the rest so the shares sum to total.
func insuranceShares(total *big.Int, notionals []*big.Int) []*big.Int {
	shares := make([]*big.Int, len(notionals))
	sum := new(big.Int)
	for _, notional := range notionals {
		sum.Add(sum, notional)
	}
	given := new(big.Int)
	for i, notional := range notionals {
		if i == len(notionals)-1 || sum.Sign() == 0 {
			shares[i] = new(big.Int).Sub(total, given)
			given.Set(total)
			continue
		}
		shares[i] = new(big.Int).Quo(new(big.Int).Mul(total, notional), sum)
		given.Add(given, shares[i])
	}
	return shares
}

func insuranceContribution(params types.Params, amount uint256.Int, source uint8) types.EventRecord {
	return types.EventRecord{Name: types.EventInsuranceContribution, Args: []any{
		params.CollateralToken, amount.ToBig(), source,
	}}
}

func insuranceCeilDiv(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func insuranceAdd(word uint256.Int, amount *big.Int) (uint256.Int, error) {
	sum := new(big.Int).Add(word.ToBig(), amount)
	var out uint256.Int
	if sum.Sign() < 0 || out.SetFromBig(sum) {
		return uint256.Int{}, fmt.Errorf("%w: %s plus %s", types.ErrOverflow, word.Dec(), amount)
	}
	return out, nil
}

// insuranceSigned reads a word as an int256 in two's complement.
func insuranceSigned(word uint256.Int) *big.Int {
	if word.Sign() >= 0 {
		return word.ToBig()
	}
	var magnitude uint256.Int
	magnitude.Neg(&word)
	return new(big.Int).Neg(magnitude.ToBig())
}

// insuranceSignedWord writes an int256 as a word in two's complement.
func insuranceSignedWord(value *big.Int) (uint256.Int, error) {
	if value.Cmp(insuranceInt256Max) > 0 || value.Cmp(insuranceInt256Min) < 0 {
		return uint256.Int{}, fmt.Errorf("%w: %s outside int256", types.ErrOverflow, value)
	}
	var word uint256.Int
	word.SetFromBig(new(big.Int).Abs(value))
	if value.Sign() < 0 {
		word.Neg(&word)
	}
	return word, nil
}
