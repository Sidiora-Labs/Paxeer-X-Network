package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

var (
	accountFixedOne   = big.NewInt(1_000_000_000_000_000_000)
	accountUsdPerUsdl = big.NewInt(1_000_000_000_000)
	// accountNotionalFloorDivisor makes the withdrawal floor 10 percent of
	// the position notional at mark.
	accountNotionalFloorDivisor = big.NewInt(10)
)

// GetAccount returns the stored account, ErrNoAccount when the address never
// created one.
func (k *Keeper) GetAccount(ctx sdk.Context, address common.Address) (types.Account, error) {
	account, found, err := k.accountLoad(ctx, address)
	if err != nil {
		return types.Account{}, err
	}
	if !found {
		return types.Account{}, fmt.Errorf("%w: %s", types.ErrNoAccount, address)
	}
	return account, nil
}

// EnsureAccount returns the account of the address, creating an empty cross
// account on the first createVault, deposit or order method; created reports
// whether this call created it.
func (k *Keeper) EnsureAccount(ctx sdk.Context, address common.Address) (account types.Account, created bool, err error) {
	if address == (common.Address{}) {
		return types.Account{}, false, fmt.Errorf("%w: account", types.ErrZeroAddress)
	}
	account, found, err := k.accountLoad(ctx, address)
	if err != nil || found {
		return account, false, err
	}
	account = types.Account{Address: address, Created: true}
	k.accountStore(ctx, account)
	return account, true, nil
}

// Deposit books amount USDL units on the account after the deposit leg
// transferFrom(account, perps module EVM address, amount) moved them, creating
// the account on its first deposit and settling its positions at mark first.
// It returns the settlement events followed by CollateralDeposited.
func (k *Keeper) Deposit(ctx sdk.Context, address common.Address, amount uint256.Int) ([]types.EventRecord, error) {
	if amount.IsZero() {
		return nil, fmt.Errorf("%w: deposit", types.ErrZeroAmount)
	}
	if _, _, err := k.EnsureAccount(ctx, address); err != nil {
		return nil, err
	}
	events, err := k.Settle(ctx, address)
	if err != nil {
		return nil, err
	}
	account, err := k.GetAccount(ctx, address)
	if err != nil {
		return nil, err
	}
	if _, overflow := account.Collateral.AddOverflow(&account.Collateral, &amount); overflow {
		return nil, fmt.Errorf("%w: collateral of %s plus %s", types.ErrOverflow, address, amount.Dec())
	}
	k.accountStore(ctx, account)
	if _, err := k.RefreshMarginRatio(ctx, address); err != nil {
		return nil, err
	}
	return append(events, collateralEvent(types.EventCollateralDeposited, address, amount, account.Collateral)), nil
}

// Withdraw books the withdrawal of amount USDL units that the withdrawal leg
// transfer(account, amount) pays out of the perps module EVM address. The
// positions are settled at mark first and amount is bounded by Withdrawable
// with ErrInsufficientMargin. It returns the settlement events followed by
// CollateralWithdrawn.
func (k *Keeper) Withdraw(ctx sdk.Context, address common.Address, amount uint256.Int) ([]types.EventRecord, error) {
	if address == (common.Address{}) {
		return nil, fmt.Errorf("%w: account", types.ErrZeroAddress)
	}
	if amount.IsZero() {
		return nil, fmt.Errorf("%w: withdrawal", types.ErrZeroAmount)
	}
	events, err := k.Settle(ctx, address)
	if err != nil {
		return nil, err
	}
	account, err := k.accountDebitWithdrawable(ctx, address, amount)
	if err != nil {
		return nil, err
	}
	k.accountStore(ctx, account)
	if _, err := k.RefreshMarginRatio(ctx, address); err != nil {
		return nil, err
	}
	return append(events, collateralEvent(types.EventCollateralWithdrawn, address, amount, account.Collateral)), nil
}

// TransferCollateral moves amount USDL units from one account to another as
// store entries; no token moves. Both accounts are settled at mark first and
// amount is bounded by the withdrawable amount of from. It returns the
// settlement events followed by CollateralTransferred.
func (k *Keeper) TransferCollateral(ctx sdk.Context, from, to common.Address, amount uint256.Int) ([]types.EventRecord, error) {
	if amount.IsZero() {
		return nil, fmt.Errorf("%w: transfer", types.ErrZeroAmount)
	}
	if from == (common.Address{}) || to == (common.Address{}) {
		return nil, fmt.Errorf("%w: transfer from %s to %s", types.ErrZeroAddress, from, to)
	}
	if from == to {
		return nil, fmt.Errorf("%w: %s", types.ErrSelfTransfer, from)
	}
	if _, err := k.GetAccount(ctx, to); err != nil {
		return nil, err
	}
	events, err := k.Settle(ctx, from)
	if err != nil {
		return nil, err
	}
	recipientEvents, err := k.Settle(ctx, to)
	if err != nil {
		return nil, err
	}
	events = append(events, recipientEvents...)
	sender, err := k.accountDebitWithdrawable(ctx, from, amount)
	if err != nil {
		return nil, err
	}
	recipient, err := k.GetAccount(ctx, to)
	if err != nil {
		return nil, err
	}
	if _, overflow := recipient.Collateral.AddOverflow(&recipient.Collateral, &amount); overflow {
		return nil, fmt.Errorf("%w: collateral of %s plus %s", types.ErrOverflow, to, amount.Dec())
	}
	k.accountStore(ctx, sender)
	k.accountStore(ctx, recipient)
	if _, err := k.RefreshMarginRatio(ctx, from); err != nil {
		return nil, err
	}
	if _, err := k.RefreshMarginRatio(ctx, to); err != nil {
		return nil, err
	}
	return append(events, types.EventRecord{
		Name: types.EventCollateralTransferred,
		Args: []any{from, to, amount.ToBig()},
	}), nil
}

// EmergencyWithdraw drains the withdrawable collateral of an account without
// positions: it cancels every open order of the account, then books the
// withdrawal of everything left withdrawable. It refuses an account with an
// open position with ErrInsufficientMargin and an account with nothing to
// withdraw with ErrZeroAmount. It returns the amount the withdrawal leg pays
// and the OrderCancelled events followed by CollateralWithdrawn.
func (k *Keeper) EmergencyWithdraw(ctx sdk.Context, address common.Address) (uint256.Int, []types.EventRecord, error) {
	if address == (common.Address{}) {
		return uint256.Int{}, nil, fmt.Errorf("%w: account", types.ErrZeroAddress)
	}
	if _, err := k.GetAccount(ctx, address); err != nil {
		return uint256.Int{}, nil, err
	}
	positions, err := k.accountPositions(ctx, address)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if len(positions) > 0 {
		return uint256.Int{}, nil, fmt.Errorf("%w: %s holds %d positions", types.ErrInsufficientMargin, address, len(positions))
	}
	orderIDs, err := k.accountOpenOrders(ctx, address)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	var events []types.EventRecord
	for _, orderID := range orderIDs {
		cancelled, err := k.CancelOrder(ctx, address, orderID)
		if err != nil {
			return uint256.Int{}, nil, err
		}
		events = append(events, cancelled)
	}
	amount, err := k.Withdrawable(ctx, address)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	if amount.IsZero() {
		return uint256.Int{}, nil, fmt.Errorf("%w: %s has nothing withdrawable", types.ErrZeroAmount, address)
	}
	account, err := k.accountDebitWithdrawable(ctx, address, amount)
	if err != nil {
		return uint256.Int{}, nil, err
	}
	k.accountStore(ctx, account)
	return amount, append(events, collateralEvent(types.EventCollateralWithdrawn, address, amount, account.Collateral)), nil
}

// Withdrawable is equity - max(initial margin of positions and orders, 10
// percent of the position notional at mark), floored at 0, in USDL units
// rounded down (contracts/perps-parameters.md section 2).
func (k *Keeper) Withdrawable(ctx sdk.Context, address common.Address) (uint256.Int, error) {
	if _, err := k.GetAccount(ctx, address); err != nil {
		return uint256.Int{}, err
	}
	margin, err := k.AccountMargin(ctx, address)
	if err != nil {
		return uint256.Int{}, err
	}
	positions, err := k.accountPositions(ctx, address)
	if err != nil {
		return uint256.Int{}, err
	}
	notional := new(big.Int)
	for _, position := range positions {
		mark, err := k.accountMark(ctx, position.MarketID)
		if err != nil {
			return uint256.Int{}, err
		}
		value, err := accountNotional(position.SizeBase, mark, true)
		if err != nil {
			return uint256.Int{}, err
		}
		notional.Add(notional, value.ToBig())
	}
	required := accountCeilDiv(notional, accountNotionalFloorDivisor)
	if initial := margin.InitialMargin.BigInt(); initial.Cmp(required) > 0 {
		required = initial
	}
	free := new(big.Int).Sub(margin.Equity.BigInt(), required)
	if free.Sign() <= 0 {
		return uint256.Int{}, nil
	}
	usdl, _, err := types.UsdToUsdlDown(sdk.NewIntFromBigInt(free))
	if err != nil {
		return uint256.Int{}, err
	}
	var out uint256.Int
	out.SetFromBig(usdl.BigInt())
	return out, nil
}

// Settle realises the PnL and the funding of every position of the account at
// the mark of its market before a collateral change: side x (size x mark /
// 1e18 - cost basis) is booked into the collateral, credits rounded down and
// debits rounded up to the USDL unit with the remainder in residueUsd, a loss
// above the collateral leaving it at 0 and the rest as the account's deficit;
// the reference price becomes the mark and the cost basis size x mark / 1e18,
// rounded down for a long and up for a short. Funding settles through
// SettlePositionFunding at the market's cumulative index and the account is
// re-bucketed in the MarginRatioIndex. It returns the PositionFundingApplied
// events.
func (k *Keeper) Settle(ctx sdk.Context, address common.Address) ([]types.EventRecord, error) {
	account, err := k.GetAccount(ctx, address)
	if err != nil {
		return nil, err
	}
	positions, err := k.accountPositions(ctx, address)
	if err != nil {
		return nil, err
	}
	var events []types.EventRecord
	for _, position := range positions {
		state, err := k.GetMarketState(ctx, position.MarketID)
		if err != nil {
			return nil, err
		}
		if state.MarkPrice.IsZero() {
			return nil, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, position.MarketID)
		}
		costBasis, err := accountNotional(position.SizeBase, state.MarkPrice, !position.IsLong)
		if err != nil {
			return nil, err
		}
		pnl := new(big.Int).Sub(costBasis.ToBig(), position.CostBasisUsd.ToBig())
		if !position.IsLong {
			pnl.Neg(pnl)
		}
		if err := k.accountBookPnl(ctx, &account, pnl); err != nil {
			return nil, err
		}
		position.ReferencePrice = state.MarkPrice
		position.CostBasisUsd = costBasis
		funding, err := k.SettlePositionFunding(ctx, &account, &position, state.FundingIndex)
		if err != nil {
			return nil, err
		}
		if funding != nil {
			events = append(events, *funding)
		}
		k.store(ctx).Set(types.PositionKey(position.ID), position.Marshal())
	}
	k.accountStore(ctx, account)
	if _, err := k.RefreshMarginRatio(ctx, address); err != nil {
		return nil, err
	}
	return events, nil
}

// accountBookPnl books realised PnL in USD into the account's collateral and
// the venue books.
func (k *Keeper) accountBookPnl(ctx sdk.Context, account *types.Account, pnl *big.Int) error {
	if pnl.Sign() == 0 {
		return nil
	}
	books, err := k.accountVenueBooks(ctx)
	if err != nil {
		return err
	}
	var residue *big.Int
	if pnl.Sign() > 0 {
		credit, remainder, err := types.UsdToUsdlDown(sdk.NewIntFromBigInt(pnl))
		if err != nil {
			return err
		}
		if account.Collateral, err = accountAdd(account.Collateral, credit.BigInt()); err != nil {
			return err
		}
		residue = remainder.BigInt()
	} else {
		loss := new(big.Int).Neg(pnl)
		collateralUsd := new(big.Int).Mul(account.Collateral.ToBig(), accountUsdPerUsdl)
		if loss.Cmp(collateralUsd) > 0 {
			deficit := loss.Sub(loss, collateralUsd)
			account.Collateral = uint256.Int{}
			if account.DeficitUsd, err = accountAdd(account.DeficitUsd, deficit); err != nil {
				return err
			}
			if books.DeficitSumUsd, err = accountAdd(books.DeficitSumUsd, deficit); err != nil {
				return err
			}
			k.store(ctx).Set(types.VenueBooksKey, books.Marshal())
			return nil
		}
		debit, remainder, err := types.UsdToUsdlUp(sdk.NewIntFromBigInt(loss))
		if err != nil {
			return err
		}
		var amount uint256.Int
		amount.SetFromBig(debit.BigInt())
		account.Collateral.Sub(&account.Collateral, &amount)
		residue = remainder.BigInt()
	}
	if books.ResidueUsd, err = accountAdd(books.ResidueUsd, residue); err != nil {
		return err
	}
	k.store(ctx).Set(types.VenueBooksKey, books.Marshal())
	return nil
}

// accountDebitWithdrawable returns the account with amount debited, refusing
// an amount above Withdrawable or the collateral with ErrInsufficientMargin.
func (k *Keeper) accountDebitWithdrawable(ctx sdk.Context, address common.Address, amount uint256.Int) (types.Account, error) {
	withdrawable, err := k.Withdrawable(ctx, address)
	if err != nil {
		return types.Account{}, err
	}
	account, err := k.GetAccount(ctx, address)
	if err != nil {
		return types.Account{}, err
	}
	if amount.Gt(&withdrawable) || amount.Gt(&account.Collateral) {
		return types.Account{}, fmt.Errorf("%w: %s requests %s of %s withdrawable", types.ErrInsufficientMargin,
			address, amount.Dec(), withdrawable.Dec())
	}
	account.Collateral.Sub(&account.Collateral, &amount)
	return account, nil
}

func (k *Keeper) accountLoad(ctx sdk.Context, address common.Address) (types.Account, bool, error) {
	bz := k.store(ctx).Get(types.AccountKey(address))
	if bz == nil {
		return types.Account{}, false, nil
	}
	var account types.Account
	if err := account.Unmarshal(bz); err != nil {
		return types.Account{}, false, fmt.Errorf("%w: account %s: %v", types.ErrInvalidParams, address, err)
	}
	if account.Address != address {
		return types.Account{}, false, fmt.Errorf("%w: account %s holds %s", types.ErrInvalidParams, address, account.Address)
	}
	return account, true, nil
}

func (k *Keeper) accountStore(ctx sdk.Context, account types.Account) {
	k.store(ctx).Set(types.AccountKey(account.Address), account.Marshal())
}

// accountPositions lists the account's positions in ascending market id from
// its AccountMarketPosition index.
func (k *Keeper) accountPositions(ctx sdk.Context, address common.Address) ([]types.Position, error) {
	prefix := types.AccountMarketPositionKey(address, 0)[:len(types.AccountMarketPositionPrefix)+common.AddressLength]
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	var ids []uint256.Int
	for ; iterator.Valid(); iterator.Next() {
		if len(iterator.Key()) != len(prefix)+8 || len(iterator.Value()) != 32 {
			return nil, fmt.Errorf("%w: account market position entry %x", types.ErrInvalidParams, iterator.Key())
		}
		var id uint256.Int
		id.SetBytes32(iterator.Value())
		ids = append(ids, id)
	}
	positions := make([]types.Position, 0, len(ids))
	for _, id := range ids {
		bz := k.store(ctx).Get(types.PositionKey(id))
		if bz == nil {
			return nil, fmt.Errorf("%w: %s indexed for %s", types.ErrUnknownPosition, id.Dec(), address)
		}
		var position types.Position
		if err := position.Unmarshal(bz); err != nil {
			return nil, fmt.Errorf("%w: position %s: %v", types.ErrInvalidParams, id.Dec(), err)
		}
		if position.Account != address {
			return nil, fmt.Errorf("%w: position %s indexed for %s", types.ErrNotPositionHolder, id.Dec(), address)
		}
		positions = append(positions, position)
	}
	return positions, nil
}

// GetPosition reads position id, ErrUnknownPosition when it names no open
// position.
func (k *Keeper) GetPosition(ctx sdk.Context, id uint256.Int) (types.Position, error) {
	return k.liquidationPosition(ctx, id)
}

// AccountPositions lists the account's open positions in ascending market id,
// empty for an account with none.
func (k *Keeper) AccountPositions(ctx sdk.Context, address common.Address) ([]types.Position, error) {
	return k.accountPositions(ctx, address)
}

// AccountOpenOrders lists the ids of the account's open orders, empty for an
// account with none.
func (k *Keeper) AccountOpenOrders(ctx sdk.Context, address common.Address) ([]uint256.Int, error) {
	return k.accountOpenOrders(ctx, address)
}

// accountOpenOrders lists the ids of the account's open orders from its
// AccountOrder index.
func (k *Keeper) accountOpenOrders(ctx sdk.Context, address common.Address) ([]uint256.Int, error) {
	prefix := types.AccountOrderPrefixFor(address)
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	var ids []uint256.Int
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+32 {
			return nil, fmt.Errorf("%w: account order key %x", types.ErrInvalidParams, key)
		}
		var id uint256.Int
		id.SetBytes32(key[len(prefix):])
		ids = append(ids, id)
	}
	return ids, nil
}

// accountMark is the stored mark of the market, ErrPriceStale before any.
func (k *Keeper) accountMark(ctx sdk.Context, marketID uint64) (uint256.Int, error) {
	state, err := k.GetMarketState(ctx, marketID)
	if err != nil {
		return uint256.Int{}, err
	}
	if state.MarkPrice.IsZero() {
		return uint256.Int{}, fmt.Errorf("%w: market %d has no mark", types.ErrPriceStale, marketID)
	}
	return state.MarkPrice, nil
}

func (k *Keeper) accountVenueBooks(ctx sdk.Context) (types.VenueBooks, error) {
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

// accountNotional is size x price / 1e18 in USD, rounded up when roundUp.
func accountNotional(size, price uint256.Int, roundUp bool) (uint256.Int, error) {
	product := new(big.Int).Mul(size.ToBig(), price.ToBig())
	var notional *big.Int
	if roundUp {
		notional = accountCeilDiv(product, accountFixedOne)
	} else {
		notional = product.Quo(product, accountFixedOne)
	}
	var out uint256.Int
	if out.SetFromBig(notional) {
		return uint256.Int{}, fmt.Errorf("%w: notional of size %s at %s", types.ErrOverflow, size.Dec(), price.Dec())
	}
	return out, nil
}

func accountCeilDiv(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

// accountAdd adds a non-negative amount to a word, refusing a sum above
// 2^256-1 with ErrOverflow.
func accountAdd(word uint256.Int, amount *big.Int) (uint256.Int, error) {
	var addend, sum uint256.Int
	if amount.Sign() < 0 || addend.SetFromBig(amount) {
		return uint256.Int{}, fmt.Errorf("%w: %s", types.ErrOverflow, amount)
	}
	if _, overflow := sum.AddOverflow(&word, &addend); overflow {
		return uint256.Int{}, fmt.Errorf("%w: %s plus %s", types.ErrOverflow, word.Dec(), amount)
	}
	return sum, nil
}

func collateralEvent(name string, address common.Address, amount, balance uint256.Int) types.EventRecord {
	return types.EventRecord{Name: name, Args: []any{address, new(big.Int).Set(amount.ToBig()), new(big.Int).Set(balance.ToBig())}}
}
