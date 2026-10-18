package keeper

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

const (
	SolvencyInvariantName     = "perps-solvency"
	OpenInterestInvariantName = "perps-open-interest"
	BookInvariantName         = "perps-book"
	OrdersInvariantName       = "perps-orders"
	PositionsInvariantName    = "perps-positions"
	FundingInvariantName      = "perps-funding"
	CountersInvariantName     = "perps-counters"
	DeterminismInvariantName  = "perps-determinism"
	IndexesInvariantName      = "perps-indexes"
)

var (
	usdPerUsdlUnit   = new(big.Int).Exp(big.NewInt(10), big.NewInt(types.USDDecimals-types.CollateralDecimals), nil)
	balanceOfMethod  = crypto.Keccak256([]byte("balanceOf(address)"))[:4]
	knownStorePrefix = [][]byte{
		types.ParamsKey, types.MarketPrefix, types.MarketStatePrefix, types.AccountPrefix, types.OrderPrefix,
		types.BookPrefix, types.CloidPrefix, types.AccountOrderPrefix, types.PositionPrefix,
		types.AccountMarketPositionPrefix, types.TriggerPrefix, types.ExpiryPrefix, types.VolumePrefix,
		types.VenueVolumePrefix, types.LiquidationCooldownPrefix, types.SequencePrefix, types.AttachedTpSlPrefix,
		types.MarginRatioIndexPrefix, types.AdlRankIndexPrefix, types.SweepCursorKey, types.VenueBooksKey,
	}
)

// RegisterInvariants registers the nine venue invariants of contracts/perps.md
// section 24.
func RegisterInvariants(registry sdk.InvariantRegistry, k *Keeper) {
	registry.RegisterRoute(types.ModuleName, SolvencyInvariantName, SolvencyInvariant(k))
	registry.RegisterRoute(types.ModuleName, OpenInterestInvariantName, OpenInterestInvariant(k))
	registry.RegisterRoute(types.ModuleName, BookInvariantName, BookInvariant(k))
	registry.RegisterRoute(types.ModuleName, OrdersInvariantName, OrdersInvariant(k))
	registry.RegisterRoute(types.ModuleName, PositionsInvariantName, PositionsInvariant(k))
	registry.RegisterRoute(types.ModuleName, FundingInvariantName, FundingInvariant(k))
	registry.RegisterRoute(types.ModuleName, CountersInvariantName, CountersInvariant(k))
	registry.RegisterRoute(types.ModuleName, DeterminismInvariantName, DeterminismInvariant(k))
	registry.RegisterRoute(types.ModuleName, IndexesInvariantName, IndexesInvariant(k))
}

// venueInvariant runs check over the decoded store and reports every line it
// writes; a store that does not decode breaks the invariant.
func venueInvariant(k *Keeper, name string, check func(sdk.Context, *venue) string) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		v, err := k.loadVenue(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, name, fmt.Sprintf("store unreadable: %v\n", err)), true
		}
		message := check(ctx, v)
		return sdk.FormatInvariant(types.ModuleName, name, message), message != ""
	}
}

// SolvencyInvariant checks invariant 1 in 18-decimal USD:
// balanceOf(perps module EVM address) of the USDL ERC20, read by
// StaticCallEVM, x 1e12 equals 1e12 x the sum of collateral of every account,
// perps_fees and perps_insurance included, minus the sum of account deficits,
// minus the sum over positions of side x cost basis, plus the sum over
// positions of side x funding basis, plus residueUsd.
func SolvencyInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, SolvencyInvariantName, func(ctx sdk.Context, v *venue) string {
		token, custody := v.params.CollateralToken, k.ModuleEVMAddress()
		balance, err := k.usdlBalance(ctx, token, custody)
		if err != nil {
			return fmt.Sprintf("USDL %s balance of %s unreadable: %v\n", token.Hex(), custody.Hex(), err)
		}
		liabilities := new(big.Int)
		for _, account := range v.accounts {
			liabilities.Add(liabilities, new(big.Int).Mul(account.Collateral.ToBig(), usdPerUsdlUnit))
			liabilities.Sub(liabilities, account.DeficitUsd.ToBig())
		}
		for _, position := range v.positions {
			term := new(big.Int).Sub(signedBig(position.FundingBasis), position.CostBasisUsd.ToBig())
			if !position.IsLong {
				term.Neg(term)
			}
			liabilities.Add(liabilities, term)
		}
		liabilities.Add(liabilities, v.books.ResidueUsd.ToBig())
		held := new(big.Int).Mul(balance, usdPerUsdlUnit)
		if held.Cmp(liabilities) == 0 {
			return ""
		}
		return fmt.Sprintf("USDL %s balance of %s is %s units, %s USD, against liabilities of %s USD: difference %s USD\n",
			token.Hex(), custody.Hex(), balance, held, liabilities, new(big.Int).Sub(liabilities, held))
	})
}

// usdlBalance reads token.balanceOf(holder) with StaticCallEVM from the perps
// module account.
func (k *Keeper) usdlBalance(ctx sdk.Context, token, holder common.Address) (*big.Int, error) {
	input := append(append([]byte(nil), balanceOfMethod...), common.LeftPadBytes(holder.Bytes(), 32)...)
	ret, err := k.evm.StaticCallEVM(ctx, k.ModuleAddress(), &token, input)
	if err != nil {
		return nil, err
	}
	if len(ret) != 32 {
		return nil, fmt.Errorf("balanceOf returned %d bytes", len(ret))
	}
	return new(big.Int).SetBytes(ret), nil
}

// OpenInterestInvariant checks invariant 2: for each market the stored long
// and short open interest equal the sums of long and short position sizes, in
// base units, and long equals short.
func OpenInterestInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, OpenInterestInvariantName, func(_ sdk.Context, v *venue) string {
		longs, shorts := map[uint64]*big.Int{}, map[uint64]*big.Int{}
		ids := map[uint64]bool{}
		for id := range v.markets {
			ids[id] = true
		}
		for id := range v.states {
			ids[id] = true
		}
		for _, position := range v.positions {
			ids[position.MarketID] = true
			sums := shorts
			if position.IsLong {
				sums = longs
			}
			if sums[position.MarketID] == nil {
				sums[position.MarketID] = new(big.Int)
			}
			sums[position.MarketID].Add(sums[position.MarketID], position.SizeBase.ToBig())
		}
		message := ""
		for _, id := range sortedKeys(ids) {
			state := v.states[id]
			long, short := state.LongOpenInterest.ToBig(), state.ShortOpenInterest.ToBig()
			if sum := orZero(longs[id]); long.Cmp(sum) != 0 {
				message += fmt.Sprintf("market %d: long open interest %s, long position sizes %s\n", id, long, sum)
			}
			if sum := orZero(shorts[id]); short.Cmp(sum) != 0 {
				message += fmt.Sprintf("market %d: short open interest %s, short position sizes %s\n", id, short, sum)
			}
			if long.Cmp(short) != 0 {
				message += fmt.Sprintf("market %d: long open interest %s differs from short open interest %s\n", id, long, short)
			}
		}
		return message
	})
}

// BookInvariant checks invariant 3: every RESTING order has exactly one book
// entry and every book entry points to a RESTING order of the same market,
// side, price and placement sequence.
func BookInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, BookInvariantName, func(_ sdk.Context, v *venue) string {
		orders := make(map[uint256.Int]types.Order, len(v.orders))
		for _, order := range v.orders {
			orders[order.ID] = order
		}
		entries := map[uint256.Int]int{}
		message := ""
		for _, entry := range v.book {
			order, ok := orders[entry.OrderID]
			switch {
			case !ok:
				message += fmt.Sprintf("book entry market %d side %d price %s sequence %d: order %s does not exist\n",
					entry.MarketID, entry.Side, entry.Price.Dec(), entry.Sequence, entry.OrderID.Dec())
			case order.Status != types.OrderStatusResting || order.MarketID != entry.MarketID ||
				types.SideOf(order.IsLong) != entry.Side || !order.LimitPrice.Eq(&entry.Price) || order.Sequence != entry.Sequence:
				message += fmt.Sprintf("book entry market %d side %d price %s sequence %d disagrees with order %s "+
					"(status %d, market %d, side %d, price %s, sequence %d)\n",
					entry.MarketID, entry.Side, entry.Price.Dec(), entry.Sequence, entry.OrderID.Dec(),
					order.Status, order.MarketID, types.SideOf(order.IsLong), order.LimitPrice.Dec(), order.Sequence)
			}
			entries[entry.OrderID]++
		}
		for _, order := range v.orders {
			if order.Status == types.OrderStatusResting && entries[order.ID] != 1 {
				message += fmt.Sprintf("resting order %s has %d book entries\n", order.ID.Dec(), entries[order.ID])
			}
		}
		return message
	})
}

// OrdersInvariant checks invariant 4: an account's open order count equals
// the number of its UNTRIGGERED and RESTING orders and never exceeds the
// open-order limit.
func OrdersInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, OrdersInvariantName, func(_ sdk.Context, v *venue) string {
		open := map[common.Address]uint64{}
		message := ""
		for _, order := range v.orders {
			if _, ok := v.accountIndex[order.Account]; !ok {
				message += fmt.Sprintf("order %s of unknown account %s\n", order.ID.Dec(), order.Account.Hex())
			}
			if order.Status == types.OrderStatusUntriggered || order.Status == types.OrderStatusResting {
				open[order.Account]++
			}
		}
		for _, account := range v.accounts {
			count := open[account.Address]
			if count != account.OpenOrderCount {
				message += fmt.Sprintf("account %s: open order count %d, open orders %d\n",
					account.Address.Hex(), account.OpenOrderCount, count)
			}
			if count > v.params.MaxOpenOrdersPerAccount {
				message += fmt.Sprintf("account %s: %d open orders above the limit %d\n",
					account.Address.Hex(), count, v.params.MaxOpenOrdersPerAccount)
			}
		}
		return message
	})
}

// PositionsInvariant checks invariant 5: one position per account and
// market, held in the AccountMarketPosition index, of a listed market and a
// known account, whose size is a non-zero multiple of the market lot.
func PositionsInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, PositionsInvariantName, func(_ sdk.Context, v *venue) string {
		type holding struct {
			account common.Address
			market  uint64
		}
		held := map[holding]uint256.Int{}
		message := ""
		for _, position := range v.positions {
			id := position.ID.Dec()
			if _, ok := v.accountIndex[position.Account]; !ok {
				message += fmt.Sprintf("position %s of unknown account %s\n", id, position.Account.Hex())
			}
			market, ok := v.markets[position.MarketID]
			switch {
			case !ok:
				message += fmt.Sprintf("position %s of unknown market %d\n", id, position.MarketID)
			case position.SizeBase.IsZero():
				message += fmt.Sprintf("position %s has zero size\n", id)
			default:
				lot := lotSize(market)
				var remainder uint256.Int
				if remainder.Mod(&position.SizeBase, &lot); !remainder.IsZero() {
					message += fmt.Sprintf("position %s size %s is not a multiple of the lot %s\n", id, position.SizeBase.Dec(), lot.Dec())
				}
			}
			key := holding{position.Account, position.MarketID}
			if first, dup := held[key]; dup {
				message += fmt.Sprintf("account %s holds positions %s and %s in market %d\n",
					position.Account.Hex(), first.Dec(), id, position.MarketID)
				continue
			}
			held[key] = position.ID
		}
		indexed := map[uint256.Int]bool{}
		for _, entry := range v.accountPositions {
			key := holding{entry.Account, entry.MarketID}
			indexed[entry.PositionID] = true
			if id, ok := held[key]; !ok || !id.Eq(&entry.PositionID) {
				message += fmt.Sprintf("account market entry %s market %d points to position %s, not held there\n",
					entry.Account.Hex(), entry.MarketID, entry.PositionID.Dec())
			}
		}
		for _, position := range v.positions {
			if !indexed[position.ID] {
				message += fmt.Sprintf("position %s has no account market entry\n", position.ID.Dec())
			}
		}
		return message
	})
}

// lotSize is the market lot in 18-decimal base units, 10^(18 - szDecimals);
// Market.Validate bounds szDecimals by 18.
func lotSize(market types.Market) uint256.Int {
	var lot uint256.Int
	lot.Exp(uint256.NewInt(10), uint256.NewInt(uint64(types.USDDecimals)-uint64(market.SzDecimals)))
	return lot
}

// FundingInvariant checks the store side of invariant 6: every market's
// cumulative index was last advanced at or before the block hour, and every
// position carrying a funding basis has the market state whose index its
// settlement reads, so the lazy settlement never measures funding from a
// zero index. The rounding of each settlement toward the venue keeps its
// residue in residueUsd, which SolvencyInvariant binds to the USDL balance.
func FundingInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, FundingInvariantName, func(ctx sdk.Context, v *venue) string {
		seconds, err := blockTimeSeconds(ctx)
		if err != nil {
			return fmt.Sprintf("block time unreadable: %v\n", err)
		}
		hour := seconds / 3600
		message := ""
		for _, id := range sortedKeys(v.states) {
			if last := v.states[id].LastIndexHour; last > hour {
				message += fmt.Sprintf("market %d: funding index advanced at hour %d after the block hour %d\n", id, last, hour)
			}
		}
		for _, position := range v.positions {
			if _, ok := v.states[position.MarketID]; !ok && !position.FundingBasis.IsZero() {
				message += fmt.Sprintf("position %s carries funding basis %s in market %d without market state\n",
					position.ID.Dec(), signedBig(position.FundingBasis), position.MarketID)
			}
		}
		return message
	})
}

// CountersInvariant checks invariant 7: the next order id and the next
// position id are greater than every stored id.
func CountersInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, CountersInvariantName, func(_ sdk.Context, v *venue) string {
		message := ""
		for _, order := range v.orders {
			if !v.nextOrderID.Gt(&order.ID) {
				message += fmt.Sprintf("next order id %s not above order %s\n", v.nextOrderID.Dec(), order.ID.Dec())
			}
		}
		for _, position := range v.positions {
			if !v.nextPositionID.Gt(&position.ID) {
				message += fmt.Sprintf("next position id %s not above position %s\n", v.nextPositionID.Dec(), position.ID.Dec())
			}
		}
		return message
	})
}

// DeterminismInvariant checks the store side of invariant 8: every key lies
// under a prefix of contracts/perps.md section 20 with that prefix's layout,
// every record decodes to the record its key names, and the JSON of params
// and markets is the canonical encoding the keeper writes, so two nodes that
// apply the same blocks hold byte-identical stores. The replay of fixed
// inputs is the golden tests' part.
func DeterminismInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, DeterminismInvariantName, func(ctx sdk.Context, _ *venue) string {
		message := ""
		iterator := k.store(ctx).Iterator(nil, nil)
		defer iterator.Close()
		for ; iterator.Valid(); iterator.Next() {
			key, value := iterator.Key(), iterator.Value()
			if !slices.ContainsFunc(knownStorePrefix, func(prefix []byte) bool { return key[0] == prefix[0] }) {
				message += fmt.Sprintf("key %x under no store prefix\n", key)
				continue
			}
			var canonical []byte
			var err error
			switch key[0] {
			case types.ParamsKey[0]:
				canonical, err = canonicalJSON[types.Params](value)
			case types.MarketPrefix[0]:
				canonical, err = canonicalJSON[types.Market](value)
			default:
				continue
			}
			if err != nil || !bytes.Equal(canonical, value) {
				message += fmt.Sprintf("key %x holds non-canonical JSON\n", key)
			}
		}
		return message
	})
}

func canonicalJSON[T any](value []byte) ([]byte, error) {
	var record T
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, err
	}
	return json.Marshal(record)
}

// IndexesInvariant checks invariant 9: the MarginRatioIndex has exactly one
// entry per account with a position, at the account's bucket, and the
// AdlRankIndex exactly one entry per position, at its market, side and rank
// score.
func IndexesInvariant(k *Keeper) sdk.Invariant {
	return venueInvariant(k, IndexesInvariantName, func(_ sdk.Context, v *venue) string {
		withPosition := map[common.Address]bool{}
		positions := make(map[uint256.Int]types.Position, len(v.positions))
		for _, position := range v.positions {
			withPosition[position.Account] = true
			positions[position.ID] = position
		}
		message := ""
		ratios := map[common.Address]int{}
		for _, entry := range v.marginRatios {
			ratios[entry.Account]++
			index, ok := v.accountIndex[entry.Account]
			switch {
			case !withPosition[entry.Account]:
				message += fmt.Sprintf("margin ratio entry of account %s without a position\n", entry.Account.Hex())
			case !ok || v.accounts[index].MarginRatioBucket != entry.Bucket:
				message += fmt.Sprintf("margin ratio entry of account %s at bucket %d disagrees with the account\n",
					entry.Account.Hex(), entry.Bucket)
			}
		}
		for _, account := range v.accounts {
			if withPosition[account.Address] && ratios[account.Address] != 1 {
				message += fmt.Sprintf("account %s has %d margin ratio entries\n", account.Address.Hex(), ratios[account.Address])
			}
		}
		ranks := map[uint256.Int]int{}
		for _, entry := range v.adlRanks {
			ranks[entry.PositionID]++
			position, ok := positions[entry.PositionID]
			switch {
			case !ok:
				message += fmt.Sprintf("ADL rank entry market %d side %d of missing position %s\n",
					entry.MarketID, entry.Side, entry.PositionID.Dec())
			case position.MarketID != entry.MarketID || types.SideOf(position.IsLong) != entry.Side ||
				!position.RankScore.Eq(&entry.RankScore):
				message += fmt.Sprintf("ADL rank entry market %d side %d score %s disagrees with position %s\n",
					entry.MarketID, entry.Side, entry.RankScore.Dec(), entry.PositionID.Dec())
			}
		}
		for _, position := range v.positions {
			if ranks[position.ID] != 1 {
				message += fmt.Sprintf("position %s has %d ADL rank entries\n", position.ID.Dec(), ranks[position.ID])
			}
		}
		return message
	})
}

// venue is the decoded store the invariants read, records in store order.
type venue struct {
	params           types.Params
	markets          map[uint64]types.Market
	states           map[uint64]types.MarketState
	accounts         []types.Account
	accountIndex     map[common.Address]int
	orders           []types.Order
	book             []types.BookEntry
	positions        []types.Position
	accountPositions []types.AccountMarketPositionEntry
	marginRatios     []types.MarginRatioEntry
	adlRanks         []types.AdlRankEntry
	books            types.VenueBooks
	nextOrderID      uint256.Int
	nextPositionID   uint256.Int
}

func corruptStore(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{types.ErrInvalidParams}, args...)...)
}

// scanPrefix visits every entry under prefix with the key bytes after the
// prefix, refusing a key of another length than keyLen.
func (k *Keeper) scanPrefix(ctx sdk.Context, prefix []byte, keyLen int, visit func(key, value []byte) error) error {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()[len(prefix):]
		if len(key) != keyLen {
			return corruptStore("key %x has the wrong length for prefix %x", iterator.Key(), prefix)
		}
		if err := visit(key, iterator.Value()); err != nil {
			return err
		}
	}
	return nil
}

func storeWord(bz []byte) uint256.Int {
	var value uint256.Int
	value.SetBytes32(bz)
	return value
}

func requireEmpty(key, value []byte, prefix []byte) error {
	if len(value) != 0 {
		return corruptStore("index entry %x%x holds %d bytes", prefix, key, len(value))
	}
	return nil
}

func requireSide(side byte, prefix, key []byte) error {
	if side != types.SideBuy && side != types.SideSell {
		return corruptStore("entry %x%x has side %d", prefix, key, side)
	}
	return nil
}

func (k *Keeper) loadVenue(ctx sdk.Context) (*venue, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	v := &venue{
		params:       params,
		markets:      map[uint64]types.Market{},
		states:       map[uint64]types.MarketState{},
		accountIndex: map[common.Address]int{},
	}
	for _, load := range []func(sdk.Context, *venue) error{
		k.loadMarkets, k.loadAccounts, k.loadOrders, k.loadPositions, k.loadIndexes, k.loadBooks,
	} {
		if err := load(ctx, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func (k *Keeper) loadMarkets(ctx sdk.Context, v *venue) error {
	if err := k.scanPrefix(ctx, types.MarketPrefix, 8, func(key, value []byte) error {
		id := sdk.BigEndianToUint64(key)
		market, err := decodeMarket(id, value)
		if err != nil {
			return err
		}
		if market.ID != id {
			return corruptStore("market key %d holds market %d", id, market.ID)
		}
		v.markets[id] = market
		return nil
	}); err != nil {
		return err
	}
	return k.scanPrefix(ctx, types.MarketStatePrefix, 8, func(key, value []byte) error {
		id := sdk.BigEndianToUint64(key)
		var state types.MarketState
		if err := state.Unmarshal(value); err != nil {
			return corruptStore("market state %d: %v", id, err)
		}
		if state.MarketID != id {
			return corruptStore("market state key %d holds market %d", id, state.MarketID)
		}
		v.states[id] = state
		return nil
	})
}

func (k *Keeper) loadAccounts(ctx sdk.Context, v *venue) error {
	return k.scanPrefix(ctx, types.AccountPrefix, common.AddressLength, func(key, value []byte) error {
		var account types.Account
		if err := account.Unmarshal(value); err != nil {
			return corruptStore("account %x: %v", key, err)
		}
		if account.Address != common.BytesToAddress(key) {
			return corruptStore("account key %x holds account %s", key, account.Address.Hex())
		}
		v.accountIndex[account.Address] = len(v.accounts)
		v.accounts = append(v.accounts, account)
		return nil
	})
}

func (k *Keeper) loadOrders(ctx sdk.Context, v *venue) error {
	if err := k.scanPrefix(ctx, types.OrderPrefix, 32, func(key, value []byte) error {
		var order types.Order
		if err := order.Unmarshal(value); err != nil {
			return corruptStore("order %x: %v", key, err)
		}
		if id := storeWord(key); !order.ID.Eq(&id) {
			return corruptStore("order key %s holds order %s", id.Dec(), order.ID.Dec())
		}
		v.orders = append(v.orders, order)
		return nil
	}); err != nil {
		return err
	}
	// marketId (8), side (1), price key (32), sequence (8).
	return k.scanPrefix(ctx, types.BookPrefix, 49, func(key, value []byte) error {
		if len(value) != 32 {
			return corruptStore("book entry %x holds %d bytes", key, len(value))
		}
		entry := types.BookEntry{
			MarketID: sdk.BigEndianToUint64(key[:8]),
			Side:     key[8],
			Price:    storeWord(key[9:41]),
			Sequence: sdk.BigEndianToUint64(key[41:]),
			OrderID:  storeWord(value),
		}
		if err := requireSide(entry.Side, types.BookPrefix, key); err != nil {
			return err
		}
		if entry.Side == types.SideBuy {
			entry.Price.Not(&entry.Price)
		}
		v.book = append(v.book, entry)
		return nil
	})
}

func (k *Keeper) loadPositions(ctx sdk.Context, v *venue) error {
	if err := k.scanPrefix(ctx, types.PositionPrefix, 32, func(key, value []byte) error {
		var position types.Position
		if err := position.Unmarshal(value); err != nil {
			return corruptStore("position %x: %v", key, err)
		}
		if id := storeWord(key); !position.ID.Eq(&id) {
			return corruptStore("position key %s holds position %s", id.Dec(), position.ID.Dec())
		}
		v.positions = append(v.positions, position)
		return nil
	}); err != nil {
		return err
	}
	return k.scanPrefix(ctx, types.AccountMarketPositionPrefix, common.AddressLength+8, func(key, value []byte) error {
		if len(value) != 32 {
			return corruptStore("account market entry %x holds %d bytes", key, len(value))
		}
		v.accountPositions = append(v.accountPositions, types.AccountMarketPositionEntry{
			Account:    common.BytesToAddress(key[:common.AddressLength]),
			MarketID:   sdk.BigEndianToUint64(key[common.AddressLength:]),
			PositionID: storeWord(value),
		})
		return nil
	})
}

func (k *Keeper) loadIndexes(ctx sdk.Context, v *venue) error {
	if err := k.scanPrefix(ctx, types.MarginRatioIndexPrefix, 2+common.AddressLength, func(key, value []byte) error {
		if err := requireEmpty(key, value, types.MarginRatioIndexPrefix); err != nil {
			return err
		}
		v.marginRatios = append(v.marginRatios, types.MarginRatioEntry{
			Bucket:  binary.BigEndian.Uint16(key[:2]),
			Account: common.BytesToAddress(key[2:]),
		})
		return nil
	}); err != nil {
		return err
	}
	// marketId (8), side (1), inverted rank score (32), positionId (32).
	return k.scanPrefix(ctx, types.AdlRankIndexPrefix, 73, func(key, value []byte) error {
		if err := requireEmpty(key, value, types.AdlRankIndexPrefix); err != nil {
			return err
		}
		if err := requireSide(key[8], types.AdlRankIndexPrefix, key); err != nil {
			return err
		}
		entry := types.AdlRankEntry{
			MarketID:   sdk.BigEndianToUint64(key[:8]),
			Side:       key[8],
			RankScore:  storeWord(key[9:41]),
			PositionID: storeWord(key[41:]),
		}
		entry.RankScore.Not(&entry.RankScore)
		v.adlRanks = append(v.adlRanks, entry)
		return nil
	})
}

func (k *Keeper) loadBooks(ctx sdk.Context, v *venue) error {
	if bz := k.store(ctx).Get(types.VenueBooksKey); bz != nil {
		if err := v.books.Unmarshal(bz); err != nil {
			return corruptStore("venue books: %v", err)
		}
	}
	var err error
	if v.nextOrderID, err = k.storedSequence(ctx, types.SequenceOrderID); err != nil {
		return err
	}
	v.nextPositionID, err = k.storedSequence(ctx, types.SequencePositionID)
	return err
}

// storedSequence is the next id of a sequence, 1 before the first allocation
// as nextID reads it.
func (k *Keeper) storedSequence(ctx sdk.Context, name byte) (uint256.Int, error) {
	bz := k.store(ctx).Get(types.SequenceKey(name))
	if bz == nil {
		return *uint256.NewInt(1), nil
	}
	if len(bz) != 32 {
		return uint256.Int{}, corruptStore("sequence %d holds %d bytes", name, len(bz))
	}
	return storeWord(bz), nil
}

func orZero(value *big.Int) *big.Int {
	if value == nil {
		return new(big.Int)
	}
	return value
}

func sortedKeys[V any](m map[uint64]V) []uint64 {
	keys := make([]uint64, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
