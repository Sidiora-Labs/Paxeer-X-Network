package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// BookEntry is one entry of prefix 0x06; Price is the order's limit price,
// keyed by BookPriceKey.
type BookEntry struct {
	MarketID uint64      `json:"market_id"`
	Side     byte        `json:"side"`
	Price    uint256.Int `json:"price"`
	Sequence uint64      `json:"sequence"`
	OrderID  uint256.Int `json:"order_id"`
}

// CloidEntry is one entry of prefix 0x07.
type CloidEntry struct {
	Account  common.Address `json:"account"`
	MarketID uint64         `json:"market_id"`
	Cloid    [16]byte       `json:"cloid"`
	OrderID  uint256.Int    `json:"order_id"`
}

// AccountOrderEntry is one entry of prefix 0x08.
type AccountOrderEntry struct {
	Account common.Address `json:"account"`
	OrderID uint256.Int    `json:"order_id"`
}

// AccountMarketPositionEntry is one entry of prefix 0x0A.
type AccountMarketPositionEntry struct {
	Account    common.Address `json:"account"`
	MarketID   uint64         `json:"market_id"`
	PositionID uint256.Int    `json:"position_id"`
}

// TriggerEntry is one entry of prefix 0x0B; TriggerPrice is the order's
// trigger price, keyed by direction.
type TriggerEntry struct {
	MarketID     uint64      `json:"market_id"`
	Direction    byte        `json:"direction"`
	TriggerPrice uint256.Int `json:"trigger_price"`
	Sequence     uint64      `json:"sequence"`
	OrderID      uint256.Int `json:"order_id"`
}

// ExpiryEntry is one entry of prefix 0x0C.
type ExpiryEntry struct {
	Deadline uint64      `json:"deadline"`
	OrderID  uint256.Int `json:"order_id"`
}

// VolumeBucket is one entry of prefix 0x0D: an account's USD volume of the
// UTC day floor(block time / 86400).
type VolumeBucket struct {
	Account        common.Address `json:"account"`
	Day            uint64         `json:"day"`
	TakerVolumeUsd uint256.Int    `json:"taker_volume_usd"`
	MakerVolumeUsd uint256.Int    `json:"maker_volume_usd"`
}

// VenueVolumeBucket is one entry of prefix 0x0E.
type VenueVolumeBucket struct {
	Day       uint64      `json:"day"`
	VolumeUsd uint256.Int `json:"volume_usd"`
}

// LiquidationCooldown is one entry of prefix 0x0F.
type LiquidationCooldown struct {
	PositionID    uint256.Int `json:"position_id"`
	LastSliceTime uint64      `json:"last_slice_time"`
	SliceBasis    uint256.Int `json:"slice_basis"`
}

// AttachedTpSl is one entry of prefix 0x11; an order id of 0 means none.
type AttachedTpSl struct {
	PositionID        uint256.Int `json:"position_id"`
	TakeProfitOrderID uint256.Int `json:"take_profit_order_id"`
	StopLossOrderID   uint256.Int `json:"stop_loss_order_id"`
}

// MarginRatioEntry is one entry of prefix 0x12.
type MarginRatioEntry struct {
	Bucket  uint16         `json:"bucket"`
	Account common.Address `json:"account"`
}

// AdlRankEntry is one entry of prefix 0x13.
type AdlRankEntry struct {
	MarketID   uint64      `json:"market_id"`
	Side       byte        `json:"side"`
	RankScore  uint256.Int `json:"rank_score"`
	PositionID uint256.Int `json:"position_id"`
}

// Counters are the values of prefix 0x10: the next order id, the next position
// id and the next global placement sequence.
type Counters struct {
	NextOrderID    uint256.Int `json:"next_order_id"`
	NextPositionID uint256.Int `json:"next_position_id"`
	NextSequence   uint64      `json:"next_sequence"`
}

// GenesisState carries every prefix of contracts/perps.md section 20. Its
// 256-bit fields marshal to JSON only through a pointer, so export and import
// a *GenesisState.
type GenesisState struct {
	Params                 Params                       `json:"params"`
	Markets                []Market                     `json:"markets"`
	MarketStates           []MarketState                `json:"market_states"`
	Accounts               []Account                    `json:"accounts"`
	Orders                 []Order                      `json:"orders"`
	BookEntries            []BookEntry                  `json:"book_entries"`
	CloidEntries           []CloidEntry                 `json:"cloid_entries"`
	AccountOrders          []AccountOrderEntry          `json:"account_orders"`
	Positions              []Position                   `json:"positions"`
	AccountMarketPositions []AccountMarketPositionEntry `json:"account_market_positions"`
	TriggerEntries         []TriggerEntry               `json:"trigger_entries"`
	ExpiryEntries          []ExpiryEntry                `json:"expiry_entries"`
	VolumeBuckets          []VolumeBucket               `json:"volume_buckets"`
	VenueVolumeBuckets     []VenueVolumeBucket          `json:"venue_volume_buckets"`
	LiquidationCooldowns   []LiquidationCooldown        `json:"liquidation_cooldowns"`
	Counters               Counters                     `json:"counters"`
	AttachedTpSls          []AttachedTpSl               `json:"attached_tp_sls"`
	MarginRatioEntries     []MarginRatioEntry           `json:"margin_ratio_entries"`
	AdlRankEntries         []AdlRankEntry               `json:"adl_rank_entries"`
	SweepCursor            uint256.Int                  `json:"sweep_cursor"`
	VenueBooks             VenueBooks                   `json:"venue_books"`
}

// DefaultGenesis is an empty venue under DefaultParams whose counters start at
// 1, as 0 means none.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Counters: Counters{
			NextOrderID:    *uint256.NewInt(1),
			NextPositionID: *uint256.NewInt(1),
			NextSequence:   1,
		},
	}
}

func invalidGenesis(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidGenesis}, args...)...)
}

type genesisIndex struct {
	markets   map[uint64]bool
	accounts  map[common.Address]Account
	orders    map[uint256.Int]Order
	positions map[uint256.Int]Position
}

// ValidateGenesis refuses with ErrInvalidGenesis invalid params or markets,
// duplicate ids and index keys, resting orders without their book entry and
// untriggered orders without their trigger entry, index entries without their
// market, account, order or position or that disagree with it, attached TP/SL
// legs that are not untriggered reduce-only TAKE_PROFIT and STOP_LOSS orders
// closing their position, a deficit sum
// other than the sum of account deficits, and counters not above every stored
// id and sequence.
func ValidateGenesis(gs GenesisState) error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidGenesis, err)
	}
	idx := genesisIndex{
		markets:   map[uint64]bool{},
		accounts:  map[common.Address]Account{},
		orders:    map[uint256.Int]Order{},
		positions: map[uint256.Int]Position{},
	}
	for _, check := range []func(GenesisState) error{
		idx.validateMarkets, idx.validateAccounts, idx.validateOrders, idx.validateOrderIndexes,
		idx.validatePositions, idx.validatePositionIndexes, idx.validateVolumes, validateCounters,
	} {
		if err := check(gs); err != nil {
			return err
		}
	}
	return nil
}

func (idx genesisIndex) validateMarkets(gs GenesisState) error {
	for _, market := range gs.Markets {
		if err := market.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidGenesis, err)
		}
		if idx.markets[market.ID] {
			return invalidGenesis("duplicate market %d", market.ID)
		}
		idx.markets[market.ID] = true
	}
	states := map[uint64]bool{}
	for _, state := range gs.MarketStates {
		if !idx.markets[state.MarketID] {
			return invalidGenesis("market state of unknown market %d", state.MarketID)
		}
		if states[state.MarketID] {
			return invalidGenesis("duplicate market state %d", state.MarketID)
		}
		states[state.MarketID] = true
	}
	return nil
}

func (idx genesisIndex) validateAccounts(gs GenesisState) error {
	var deficits uint256.Int
	for _, account := range gs.Accounts {
		if account.Address == (common.Address{}) {
			return invalidGenesis("account with the zero address")
		}
		if _, dup := idx.accounts[account.Address]; dup {
			return invalidGenesis("duplicate account %s", account.Address)
		}
		idx.accounts[account.Address] = account
		if _, overflow := deficits.AddOverflow(&deficits, &account.DeficitUsd); overflow {
			return invalidGenesis("account deficits overflow 256 bits")
		}
	}
	if !deficits.Eq(&gs.VenueBooks.DeficitSumUsd) {
		return invalidGenesis("venue deficit sum %s differs from the account deficits %s",
			gs.VenueBooks.DeficitSumUsd.Dec(), deficits.Dec())
	}
	return nil
}

func (idx genesisIndex) validateOrders(gs GenesisState) error {
	sequences := map[uint64]bool{}
	for _, order := range gs.Orders {
		id := order.ID.Dec()
		if order.ID.IsZero() {
			return invalidGenesis("order with id 0")
		}
		if _, dup := idx.orders[order.ID]; dup {
			return invalidGenesis("duplicate order %s", id)
		}
		if sequences[order.Sequence] {
			return invalidGenesis("duplicate placement sequence %d", order.Sequence)
		}
		if !idx.markets[order.MarketID] {
			return invalidGenesis("order %s of unknown market %d", id, order.MarketID)
		}
		if _, ok := idx.accounts[order.Account]; !ok {
			return invalidGenesis("order %s without its account %s", id, order.Account)
		}
		if order.Type > OrderTypeStopMarket || order.TimeInForce > TimeInForcePostOnly || order.Status > OrderStatusExpired {
			return invalidGenesis("order %s has an unknown type, time in force or status", id)
		}
		if order.FilledBase.Gt(&order.SizeBase) {
			return invalidGenesis("order %s filled above its size", id)
		}
		idx.orders[order.ID] = order
		sequences[order.Sequence] = true
	}
	return nil
}

func (idx genesisIndex) order(id uint256.Int, entry string) (Order, error) {
	order, ok := idx.orders[id]
	if !ok {
		return Order{}, invalidGenesis("%s entry without its order %s", entry, id.Dec())
	}
	return order, nil
}

func (idx genesisIndex) validateOrderIndexes(gs GenesisState) error {
	booked := map[uint256.Int]bool{}
	for _, entry := range gs.BookEntries {
		order, err := idx.order(entry.OrderID, "book")
		if err != nil {
			return err
		}
		if booked[entry.OrderID] {
			return invalidGenesis("duplicate book entry of order %s", entry.OrderID.Dec())
		}
		if order.Status != OrderStatusResting || order.MarketID != entry.MarketID || SideOf(order.IsLong) != entry.Side ||
			!order.LimitPrice.Eq(&entry.Price) || order.Sequence != entry.Sequence {
			return invalidGenesis("book entry disagrees with order %s", entry.OrderID.Dec())
		}
		booked[entry.OrderID] = true
	}
	triggered := map[uint256.Int]bool{}
	for _, entry := range gs.TriggerEntries {
		order, err := idx.order(entry.OrderID, "trigger")
		if err != nil {
			return err
		}
		if triggered[entry.OrderID] {
			return invalidGenesis("duplicate trigger entry of order %s", entry.OrderID.Dec())
		}
		if (entry.Direction != TriggerRising && entry.Direction != TriggerFalling) || order.Status != OrderStatusUntriggered ||
			order.MarketID != entry.MarketID || !order.TriggerPrice.Eq(&entry.TriggerPrice) || order.Sequence != entry.Sequence {
			return invalidGenesis("trigger entry disagrees with order %s", entry.OrderID.Dec())
		}
		triggered[entry.OrderID] = true
	}
	for _, order := range gs.Orders {
		if order.Status == OrderStatusResting && !booked[order.ID] {
			return invalidGenesis("resting order %s without a book entry", order.ID.Dec())
		}
		if order.Status == OrderStatusUntriggered && !triggered[order.ID] {
			return invalidGenesis("untriggered order %s without a trigger entry", order.ID.Dec())
		}
	}
	cloids := map[string]bool{}
	for _, entry := range gs.CloidEntries {
		order, err := idx.order(entry.OrderID, "cloid")
		if err != nil {
			return err
		}
		key := string(CloidKey(entry.Account, entry.MarketID, entry.Cloid))
		if cloids[key] {
			return invalidGenesis("duplicate cloid entry of order %s", entry.OrderID.Dec())
		}
		if entry.Cloid == ([16]byte{}) || order.Account != entry.Account || order.MarketID != entry.MarketID || order.Cloid != entry.Cloid {
			return invalidGenesis("cloid entry disagrees with order %s", entry.OrderID.Dec())
		}
		cloids[key] = true
	}
	accountOrders := map[string]bool{}
	for _, entry := range gs.AccountOrders {
		if _, ok := idx.accounts[entry.Account]; !ok {
			return invalidGenesis("account order entry without its account %s", entry.Account)
		}
		order, err := idx.order(entry.OrderID, "account order")
		if err != nil {
			return err
		}
		key := string(AccountOrderKey(entry.Account, entry.OrderID))
		if accountOrders[key] {
			return invalidGenesis("duplicate account order entry of order %s", entry.OrderID.Dec())
		}
		if order.Account != entry.Account {
			return invalidGenesis("account order entry disagrees with order %s", entry.OrderID.Dec())
		}
		accountOrders[key] = true
	}
	expiries := map[uint256.Int]bool{}
	for _, entry := range gs.ExpiryEntries {
		order, err := idx.order(entry.OrderID, "expiry")
		if err != nil {
			return err
		}
		if expiries[entry.OrderID] {
			return invalidGenesis("duplicate expiry entry of order %s", entry.OrderID.Dec())
		}
		if entry.Deadline == 0 || order.Deadline != entry.Deadline {
			return invalidGenesis("expiry entry disagrees with order %s", entry.OrderID.Dec())
		}
		expiries[entry.OrderID] = true
	}
	return nil
}

func (idx genesisIndex) validatePositions(gs GenesisState) error {
	held := map[string]bool{}
	for _, position := range gs.Positions {
		id := position.ID.Dec()
		if position.ID.IsZero() {
			return invalidGenesis("position with id 0")
		}
		if _, dup := idx.positions[position.ID]; dup {
			return invalidGenesis("duplicate position %s", id)
		}
		if !idx.markets[position.MarketID] {
			return invalidGenesis("position %s of unknown market %d", id, position.MarketID)
		}
		if _, ok := idx.accounts[position.Account]; !ok {
			return invalidGenesis("position %s without its account %s", id, position.Account)
		}
		if position.SizeBase.IsZero() {
			return invalidGenesis("position %s has zero size", id)
		}
		key := string(AccountMarketPositionKey(position.Account, position.MarketID))
		if held[key] {
			return invalidGenesis("second position of account %s in market %d", position.Account, position.MarketID)
		}
		held[key] = true
		idx.positions[position.ID] = position
	}
	return nil
}

func (idx genesisIndex) position(id uint256.Int, entry string) (Position, error) {
	position, ok := idx.positions[id]
	if !ok {
		return Position{}, invalidGenesis("%s entry without its position %s", entry, id.Dec())
	}
	return position, nil
}

func (idx genesisIndex) validatePositionIndexes(gs GenesisState) error {
	accountMarkets := map[string]bool{}
	for _, entry := range gs.AccountMarketPositions {
		if _, ok := idx.accounts[entry.Account]; !ok {
			return invalidGenesis("account market entry without its account %s", entry.Account)
		}
		position, err := idx.position(entry.PositionID, "account market")
		if err != nil {
			return err
		}
		key := string(AccountMarketPositionKey(entry.Account, entry.MarketID))
		if accountMarkets[key] {
			return invalidGenesis("duplicate account market entry of position %s", entry.PositionID.Dec())
		}
		if position.Account != entry.Account || position.MarketID != entry.MarketID {
			return invalidGenesis("account market entry disagrees with position %s", entry.PositionID.Dec())
		}
		accountMarkets[key] = true
	}
	cooldowns := map[uint256.Int]bool{}
	for _, entry := range gs.LiquidationCooldowns {
		if _, err := idx.position(entry.PositionID, "liquidation cooldown"); err != nil {
			return err
		}
		if cooldowns[entry.PositionID] {
			return invalidGenesis("duplicate liquidation cooldown of position %s", entry.PositionID.Dec())
		}
		if entry.SliceBasis.IsZero() {
			return invalidGenesis("liquidation cooldown of position %s without a slice basis", entry.PositionID.Dec())
		}
		cooldowns[entry.PositionID] = true
	}
	attached := map[uint256.Int]bool{}
	for _, entry := range gs.AttachedTpSls {
		position, err := idx.position(entry.PositionID, "attached TP/SL")
		if err != nil {
			return err
		}
		if attached[entry.PositionID] {
			return invalidGenesis("duplicate attached TP/SL of position %s", entry.PositionID.Dec())
		}
		legs := []struct {
			id   uint256.Int
			typ  OrderType
			role string
		}{{entry.TakeProfitOrderID, OrderTypeTakeProfit, "take-profit"}, {entry.StopLossOrderID, OrderTypeStopLoss, "stop-loss"}}
		for _, leg := range legs {
			if leg.id.IsZero() {
				continue
			}
			order, err := idx.order(leg.id, "attached TP/SL")
			if err != nil {
				return err
			}
			if order.Type != leg.typ || !order.ReduceOnly || order.Status != OrderStatusUntriggered ||
				order.Account != position.Account || order.MarketID != position.MarketID || order.IsLong == position.IsLong {
				return invalidGenesis("attached TP/SL order %s is not a reduce-only %s trigger of position %s",
					leg.id.Dec(), leg.role, entry.PositionID.Dec())
			}
		}
		attached[entry.PositionID] = true
	}
	ratios := map[common.Address]bool{}
	for _, entry := range gs.MarginRatioEntries {
		account, ok := idx.accounts[entry.Account]
		if !ok {
			return invalidGenesis("margin ratio entry without its account %s", entry.Account)
		}
		if ratios[entry.Account] {
			return invalidGenesis("duplicate margin ratio entry of account %s", entry.Account)
		}
		if account.MarginRatioBucket != entry.Bucket {
			return invalidGenesis("margin ratio entry disagrees with account %s", entry.Account)
		}
		ratios[entry.Account] = true
	}
	ranks := map[uint256.Int]bool{}
	for _, entry := range gs.AdlRankEntries {
		position, err := idx.position(entry.PositionID, "ADL rank")
		if err != nil {
			return err
		}
		if ranks[entry.PositionID] {
			return invalidGenesis("duplicate ADL rank entry of position %s", entry.PositionID.Dec())
		}
		if position.MarketID != entry.MarketID || SideOf(position.IsLong) != entry.Side || !position.RankScore.Eq(&entry.RankScore) {
			return invalidGenesis("ADL rank entry disagrees with position %s", entry.PositionID.Dec())
		}
		ranks[entry.PositionID] = true
	}
	return nil
}

func (idx genesisIndex) validateVolumes(gs GenesisState) error {
	buckets := map[string]bool{}
	for _, bucket := range gs.VolumeBuckets {
		if _, ok := idx.accounts[bucket.Account]; !ok {
			return invalidGenesis("volume bucket without its account %s", bucket.Account)
		}
		key := string(VolumeKey(bucket.Account, bucket.Day))
		if buckets[key] {
			return invalidGenesis("duplicate volume bucket of account %s day %d", bucket.Account, bucket.Day)
		}
		buckets[key] = true
	}
	days := map[uint64]bool{}
	for _, bucket := range gs.VenueVolumeBuckets {
		if days[bucket.Day] {
			return invalidGenesis("duplicate venue volume bucket of day %d", bucket.Day)
		}
		days[bucket.Day] = true
	}
	return nil
}

func validateCounters(gs GenesisState) error {
	var maxOrder, maxPosition uint256.Int
	var maxSequence uint64
	for _, order := range gs.Orders {
		if order.ID.Gt(&maxOrder) {
			maxOrder = order.ID
		}
		maxSequence = max(maxSequence, order.Sequence)
	}
	for _, position := range gs.Positions {
		if position.ID.Gt(&maxPosition) {
			maxPosition = position.ID
		}
	}
	if !gs.Counters.NextOrderID.Gt(&maxOrder) {
		return invalidGenesis("next order id %s not above stored order id %s", gs.Counters.NextOrderID.Dec(), maxOrder.Dec())
	}
	if !gs.Counters.NextPositionID.Gt(&maxPosition) {
		return invalidGenesis("next position id %s not above stored position id %s",
			gs.Counters.NextPositionID.Dec(), maxPosition.Dec())
	}
	if gs.Counters.NextSequence <= maxSequence {
		return invalidGenesis("next placement sequence %d not above stored sequence %d", gs.Counters.NextSequence, maxSequence)
	}
	return nil
}
