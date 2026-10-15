package types

import (
	"fmt"
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// TwapHistoryDepth is the number of slots of the TWAP sample ring of a token.
const TwapHistoryDepth = 1000

// TokenBook is the book of one token's pool (prefix 0x02).
type TokenBook struct {
	Token               common.Address `json:"token"`
	Reserves            sdk.Int        `json:"reserves"`
	TotalDeposited      sdk.Int        `json:"total_deposited"`
	TotalWithdrawn      sdk.Int        `json:"total_withdrawn"`
	Decimals            uint8          `json:"decimals"`
	ShareDenom          string         `json:"share_denom"`
	MinimumSharesLocked bool           `json:"minimum_shares_locked"`
}

// TokenEscrow is the amount of Token reserved by active orders, disjoint
// from the book (prefix 0x03).
type TokenEscrow struct {
	Token  common.Address `json:"token"`
	Amount sdk.Int        `json:"amount"`
}

// FeeRail tells the swap-rail accrual from the order-rail accrual.
type FeeRail uint8

const (
	FeeRailSwap FeeRail = iota
	FeeRailOrder
)

// FeeAccrual is the fee and impact retained on one rail in Token (prefix 0x04).
type FeeAccrual struct {
	Rail   FeeRail        `json:"rail"`
	Token  common.Address `json:"token"`
	Amount sdk.Int        `json:"amount"`
}

// UserOrder is one entry of the per-user order index, with the kind of the
// order (prefix 0x08).
type UserOrder struct {
	User      common.Address `json:"user"`
	OrderID   uint64         `json:"order_id"`
	OrderType OrderType      `json:"order_type"`
}

// TwapSample is the index price of Token at Time in ring Slot (prefix 0x0C).
type TwapSample struct {
	Token common.Address `json:"token"`
	Slot  uint32         `json:"slot"`
	Time  uint64         `json:"time"`
	Index sdk.Int        `json:"index"`
}

// TwapRing is the head and the filled count of Token's sample ring (prefix 0x0D).
type TwapRing struct {
	Token common.Address `json:"token"`
	Head  uint32         `json:"head"`
	Count uint32         `json:"count"`
}

// TokenVolume is the traded USD volume, 18 decimals, and trade count of Token
// (prefix 0x0E).
type TokenVolume struct {
	Token      common.Address `json:"token"`
	VolumeUSD  sdk.Int        `json:"volume_usd"`
	TradeCount uint64         `json:"trade_count"`
}

// OrderExpiry is one entry of the expiry index, ordered by (ExpiresAt,
// OrderID), of an active order with a non-zero expiresAt (prefix 0x11).
type OrderExpiry struct {
	ExpiresAt uint64 `json:"expires_at"`
	OrderID   uint64 `json:"order_id"`
}

// FillIndex is the active index the fill scan is walking, in scan order.
type FillIndex uint8

const (
	FillIndexStopLimit FillIndex = iota
	FillIndexLimit
	FillIndexStopLoss
)

// EndBlockCursors resume the bounded EndBlock scans (prefix 0x12): each
// scan restarts after the order id it holds, 0 meaning the start of its
// index; the fill scan also records which index it is in.
type EndBlockCursors struct {
	Activation uint64    `json:"activation"`
	FillIndex  FillIndex `json:"fill_index"`
	Fill       uint64    `json:"fill"`
}

// FillBackoff holds back an order whose EndBlock fill failed: scans skip it
// until NextRetryHeight (prefix 0x13).
type FillBackoff struct {
	OrderID         uint64 `json:"order_id"`
	Failures        uint32 `json:"failures"`
	NextRetryHeight int64  `json:"next_retry_height"`
}

// ActiveOrderCount is the number of active orders of User across the three
// kinds (prefix 0x14).
type ActiveOrderCount struct {
	User  common.Address `json:"user"`
	Count uint64         `json:"count"`
}

// GenesisState carries every prefix of the spot store.
type GenesisState struct {
	Params                Params             `json:"params"`
	Books                 []TokenBook        `json:"books"`
	Escrows               []TokenEscrow      `json:"escrows"`
	FeeAccruals           []FeeAccrual       `json:"fee_accruals"`
	NextOrderID           uint64             `json:"next_order_id"`
	LimitOrders           []LimitOrder       `json:"limit_orders"`
	StopLimitOrders       []StopLimitOrder   `json:"stop_limit_orders"`
	UserOrders            []UserOrder        `json:"user_orders"`
	ActiveLimitOrders     []uint64           `json:"active_limit_orders"`
	ActiveStopLimitOrders []uint64           `json:"active_stop_limit_orders"`
	ActiveStopLossOrders  []uint64           `json:"active_stop_loss_orders"`
	TwapSamples           []TwapSample       `json:"twap_samples"`
	TwapRings             []TwapRing         `json:"twap_rings"`
	Volumes               []TokenVolume      `json:"volumes"`
	StopLossOrders        []StopLossOrder    `json:"stop_loss_orders"`
	OrderMinOutputs       []OrderMinOutput   `json:"order_min_outputs"`
	Expiries              []OrderExpiry      `json:"expiries"`
	Cursors               EndBlockCursors    `json:"cursors"`
	FillBackoffs          []FillBackoff      `json:"fill_backoffs"`
	ActiveCounts          []ActiveOrderCount `json:"active_counts"`
}

func DefaultGenesis() *GenesisState {
	return &GenesisState{Params: DefaultParams(), Books: []TokenBook{}, Escrows: []TokenEscrow{},
		FeeAccruals: []FeeAccrual{}, NextOrderID: 1, LimitOrders: []LimitOrder{}, StopLimitOrders: []StopLimitOrder{},
		UserOrders: []UserOrder{}, ActiveLimitOrders: []uint64{}, ActiveStopLimitOrders: []uint64{},
		ActiveStopLossOrders: []uint64{}, TwapSamples: []TwapSample{}, TwapRings: []TwapRing{}, Volumes: []TokenVolume{},
		StopLossOrders: []StopLossOrder{}, OrderMinOutputs: []OrderMinOutput{}, Expiries: []OrderExpiry{},
		FillBackoffs: []FillBackoff{}, ActiveCounts: []ActiveOrderCount{}}
}

// genesisOrder is the part of a record of any kind that the indexes check.
type genesisOrder struct {
	kind      OrderType
	user      common.Address
	escrow    common.Address
	amount    sdk.Int
	active    bool
	expiresAt uint64
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	orders, err := gs.validateOrders()
	if err != nil {
		return err
	}
	if err := gs.validateOrderIndexes(orders); err != nil {
		return err
	}
	if err := gs.validateEscrowAndCounts(orders); err != nil {
		return err
	}
	return gs.validatePools()
}

func (gs GenesisState) validateOrders() (map[uint64]genesisOrder, error) {
	if gs.NextOrderID == 0 {
		return nil, fmt.Errorf("%w: next order id must start at 1", ErrInvalidGenesis)
	}
	orders := map[uint64]genesisOrder{}
	add := func(id uint64, order genesisOrder, err error) error {
		if err != nil {
			return err
		}
		if _, dup := orders[id]; dup || id >= gs.NextOrderID {
			return fmt.Errorf("%w: order id %d is duplicate or not below the next id %d", ErrInvalidGenesis, id,
				gs.NextOrderID)
		}
		orders[id] = order
		return nil
	}
	for _, o := range gs.LimitOrders {
		if err := add(o.ID, genesisOrder{o.OrderType, o.User, o.EscrowToken(), o.Amount, o.IsActive(), o.ExpiresAt},
			o.Validate()); err != nil {
			return nil, err
		}
	}
	for _, o := range gs.StopLimitOrders {
		if err := add(o.ID, genesisOrder{o.OrderType, o.User, o.EscrowToken(), o.Amount, o.IsActive(), o.ExpiresAt},
			o.Validate()); err != nil {
			return nil, err
		}
	}
	for _, o := range gs.StopLossOrders {
		if err := add(o.ID, genesisOrder{OrderTypeStopLoss, o.User, o.EscrowToken(), o.Amount, o.IsActive(),
			o.ExpiresAt}, o.Validate()); err != nil {
			return nil, err
		}
	}
	return orders, nil
}

func isKind(kind OrderType, kinds ...OrderType) bool {
	for _, k := range kinds {
		if kind == k {
			return true
		}
	}
	return false
}

func (gs GenesisState) validateOrderIndexes(orders map[uint64]genesisOrder) error {
	indexed := map[uint64]bool{}
	for _, entry := range gs.UserOrders {
		order, ok := orders[entry.OrderID]
		if !ok || indexed[entry.OrderID] || order.user != entry.User || order.kind != entry.OrderType {
			return fmt.Errorf("%w: user order entry %s/%d does not match a record", ErrInvalidGenesis,
				entry.User.Hex(), entry.OrderID)
		}
		indexed[entry.OrderID] = true
	}
	if len(indexed) != len(orders) {
		return fmt.Errorf("%w: every order needs one user order entry", ErrInvalidGenesis)
	}

	active := map[uint64]bool{}
	for _, index := range []struct {
		name  string
		ids   []uint64
		kinds []OrderType
	}{
		{"limit", gs.ActiveLimitOrders, []OrderType{OrderTypeLimitBuy, OrderTypeLimitSell}},
		{"stop-limit", gs.ActiveStopLimitOrders, []OrderType{OrderTypeStopLimitBuy, OrderTypeStopLimitSell}},
		{"stop-loss", gs.ActiveStopLossOrders, []OrderType{OrderTypeStopLoss}},
	} {
		for _, id := range index.ids {
			order, ok := orders[id]
			if !ok || !isKind(order.kind, index.kinds...) {
				return fmt.Errorf("%w: active %s index entry %d has no record", ErrInvalidGenesis, index.name, id)
			}
			if active[id] || !order.active {
				return fmt.Errorf("%w: active %s index entry %d is duplicate or not active", ErrInvalidGenesis,
					index.name, id)
			}
			active[id] = true
		}
	}
	activeExpiring := 0
	for id, order := range orders {
		if order.active != active[id] {
			return fmt.Errorf("%w: active order %d is missing from its active index", ErrInvalidGenesis, id)
		}
		if order.active && order.expiresAt != 0 {
			activeExpiring++
		}
	}

	expiries := map[uint64]bool{}
	for _, entry := range gs.Expiries {
		order, ok := orders[entry.OrderID]
		if !ok || expiries[entry.OrderID] || !order.active || order.expiresAt == 0 ||
			order.expiresAt != entry.ExpiresAt {
			return fmt.Errorf("%w: expiry entry %d/%d does not match an active order", ErrInvalidGenesis,
				entry.ExpiresAt, entry.OrderID)
		}
		expiries[entry.OrderID] = true
	}
	if len(expiries) != activeExpiring {
		return fmt.Errorf("%w: every active order with an expiry needs one expiry entry", ErrInvalidGenesis)
	}

	minimums := map[uint64]bool{}
	for _, entry := range gs.OrderMinOutputs {
		if _, ok := orders[entry.OrderID]; !ok || minimums[entry.OrderID] || !validUint256(entry.MinOutput, false) {
			return fmt.Errorf("%w: order min output %d", ErrInvalidGenesis, entry.OrderID)
		}
		minimums[entry.OrderID] = true
	}

	backoffs := map[uint64]bool{}
	for _, entry := range gs.FillBackoffs {
		if !active[entry.OrderID] || backoffs[entry.OrderID] || entry.Failures == 0 || entry.NextRetryHeight <= 0 {
			return fmt.Errorf("%w: fill back-off %d", ErrInvalidGenesis, entry.OrderID)
		}
		backoffs[entry.OrderID] = true
	}
	if gs.Cursors.FillIndex > FillIndexStopLoss {
		return fmt.Errorf("%w: fill cursor index %d", ErrInvalidGenesis, gs.Cursors.FillIndex)
	}
	return nil
}

func (gs GenesisState) validateEscrowAndCounts(orders map[uint64]genesisOrder) error {
	sums := map[common.Address]*big.Int{}
	counts := map[common.Address]uint64{}
	for _, order := range orders {
		if !order.active {
			continue
		}
		if sums[order.escrow] == nil {
			sums[order.escrow] = new(big.Int)
		}
		sums[order.escrow].Add(sums[order.escrow], order.amount.BigInt())
		counts[order.user]++
	}

	escrowed := map[common.Address]bool{}
	for _, entry := range gs.Escrows {
		sum := sums[entry.Token]
		if sum == nil {
			sum = new(big.Int)
		}
		if escrowed[entry.Token] || entry.Amount.IsNil() || entry.Amount.BigInt().Cmp(sum) != 0 {
			return fmt.Errorf("%w: escrow of %s differs from the sum %s of its active orders", ErrInvalidGenesis,
				entry.Token.Hex(), sum)
		}
		escrowed[entry.Token] = true
	}
	for token, sum := range sums {
		if !escrowed[token] {
			return fmt.Errorf("%w: escrow of %s differs from the sum %s of its active orders", ErrInvalidGenesis,
				token.Hex(), sum)
		}
	}

	counted := map[common.Address]bool{}
	for _, entry := range gs.ActiveCounts {
		if counted[entry.User] || entry.Count != counts[entry.User] {
			return fmt.Errorf("%w: active count of %s differs from its %d active orders", ErrInvalidGenesis,
				entry.User.Hex(), counts[entry.User])
		}
		counted[entry.User] = true
	}
	for user, count := range counts {
		if !counted[user] {
			return fmt.Errorf("%w: active count of %s differs from its %d active orders", ErrInvalidGenesis,
				user.Hex(), count)
		}
	}
	return nil
}

func (gs GenesisState) validatePools() error {
	zero := common.Address{}
	books := map[common.Address]bool{}
	for _, book := range gs.Books {
		if book.Token == zero || books[book.Token] || !validUint256(book.Reserves, false) ||
			!validUint256(book.TotalDeposited, false) || !validUint256(book.TotalWithdrawn, false) ||
			book.Decimals > MaxTokenDecimals || sdk.ValidateDenom(book.ShareDenom) != nil {
			return fmt.Errorf("%w: book %s", ErrInvalidGenesis, book.Token.Hex())
		}
		books[book.Token] = true
	}
	type railToken struct {
		rail  FeeRail
		token common.Address
	}
	accruals := map[railToken]bool{}
	for _, accrual := range gs.FeeAccruals {
		key := railToken{accrual.Rail, accrual.Token}
		if accrual.Rail > FeeRailOrder || accrual.Token == zero || accruals[key] || !validUint256(accrual.Amount, false) {
			return fmt.Errorf("%w: fee accrual %d/%s", ErrInvalidGenesis, accrual.Rail, accrual.Token.Hex())
		}
		accruals[key] = true
	}
	rings := map[common.Address]TwapRing{}
	for _, ring := range gs.TwapRings {
		if _, dup := rings[ring.Token]; ring.Token == zero || dup || ring.Head >= TwapHistoryDepth ||
			ring.Count > TwapHistoryDepth {
			return fmt.Errorf("%w: twap ring %s", ErrInvalidGenesis, ring.Token.Hex())
		}
		rings[ring.Token] = ring
	}
	type tokenSlot struct {
		token common.Address
		slot  uint32
	}
	slots := map[tokenSlot]bool{}
	samples := map[common.Address]uint32{}
	for _, sample := range gs.TwapSamples {
		key := tokenSlot{sample.Token, sample.Slot}
		_, ok := rings[sample.Token]
		if !ok || slots[key] || sample.Slot >= TwapHistoryDepth || !validUint256(sample.Index, false) {
			return fmt.Errorf("%w: twap sample %s/%d", ErrInvalidGenesis, sample.Token.Hex(), sample.Slot)
		}
		slots[key] = true
		samples[sample.Token]++
	}
	for token, ring := range rings {
		if samples[token] != ring.Count {
			return fmt.Errorf("%w: twap ring %s counts %d samples, genesis has %d", ErrInvalidGenesis, token.Hex(),
				ring.Count, samples[token])
		}
	}
	volumes := map[common.Address]bool{}
	for _, volume := range gs.Volumes {
		if volume.Token == zero || volumes[volume.Token] || !validUint256(volume.VolumeUSD, false) {
			return fmt.Errorf("%w: volume %s", ErrInvalidGenesis, volume.Token.Hex())
		}
		volumes[volume.Token] = true
	}
	return nil
}
