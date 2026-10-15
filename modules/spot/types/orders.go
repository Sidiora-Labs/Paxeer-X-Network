package types

import (
	"fmt"
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// OrderType is the order kind, with the values of the application enum.
type OrderType uint8

const (
	OrderTypeLimitBuy OrderType = iota
	OrderTypeLimitSell
	OrderTypeStopLoss
	OrderTypeStopLimitBuy
	OrderTypeStopLimitSell
)

// OrderStatus is the order state, with the values of the application enum.
type OrderStatus uint8

const (
	OrderStatusPending OrderStatus = iota
	OrderStatusActivated
	OrderStatusExecuted
	OrderStatusCancelled
	OrderStatusExpired
)

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// LimitOrder is a OrderTypeLimitBuy or OrderTypeLimitSell resting on 0x1022, in the field
// order of the application tuple. Prices are USD per whole token, 18
// decimals; times are block seconds, ExpiresAt 0 meaning no expiry.
type LimitOrder struct {
	ID          uint64         `json:"id"`
	User        common.Address `json:"user"`
	Stablecoin  common.Address `json:"stablecoin"`
	Token       common.Address `json:"token"`
	Amount      sdk.Int        `json:"amount"`
	TargetPrice sdk.Int        `json:"target_price"`
	OrderType   OrderType      `json:"order_type"`
	Status      OrderStatus    `json:"status"`
	CreatedAt   uint64         `json:"created_at"`
	ExpiresAt   uint64         `json:"expires_at"`
}

// StopLimitOrder is a OrderTypeStopLimitBuy or OrderTypeStopLimitSell resting on 0x1023,
// in the field order of the application tuple.
type StopLimitOrder struct {
	ID         uint64         `json:"id"`
	User       common.Address `json:"user"`
	Stablecoin common.Address `json:"stablecoin"`
	Token      common.Address `json:"token"`
	Amount     sdk.Int        `json:"amount"`
	StopPrice  sdk.Int        `json:"stop_price"`
	LimitPrice sdk.Int        `json:"limit_price"`
	OrderType  OrderType      `json:"order_type"`
	Status     OrderStatus    `json:"status"`
	CreatedAt  uint64         `json:"created_at"`
	ExpiresAt  uint64         `json:"expires_at"`
}

// StopLossOrder is a OrderTypeStopLoss resting on 0x1023 in its own record, in the
// field order of the application tuple, which has no order type.
type StopLossOrder struct {
	ID           uint64         `json:"id"`
	User         common.Address `json:"user"`
	Stablecoin   common.Address `json:"stablecoin"`
	Token        common.Address `json:"token"`
	Amount       sdk.Int        `json:"amount"`
	TriggerPrice sdk.Int        `json:"trigger_price"`
	Status       OrderStatus    `json:"status"`
	CreatedAt    uint64         `json:"created_at"`
	ExpiresAt    uint64         `json:"expires_at"`
}

// OrderMinOutput is the minimum net output of a fill of OrderID, in base
// units of the output token; an order without an entry has minimum 0.
type OrderMinOutput struct {
	OrderID   uint64  `json:"order_id"`
	MinOutput sdk.Int `json:"min_output"`
}

func (t OrderType) IsBuy() bool { return t == OrderTypeLimitBuy || t == OrderTypeStopLimitBuy }

// escrowToken is the input a buy escrows (the stablecoin) or a sell escrows
// (the token).
func escrowToken(orderType OrderType, stablecoin, token common.Address) common.Address {
	if orderType.IsBuy() {
		return stablecoin
	}
	return token
}

func (o LimitOrder) EscrowToken() common.Address {
	return escrowToken(o.OrderType, o.Stablecoin, o.Token)
}

func (o StopLimitOrder) EscrowToken() common.Address {
	return escrowToken(o.OrderType, o.Stablecoin, o.Token)
}

func (o StopLossOrder) EscrowToken() common.Address { return o.Token }

// IsActive reports whether the order still holds escrow.
func (o LimitOrder) IsActive() bool { return o.Status == OrderStatusPending }

func (o StopLimitOrder) IsActive() bool {
	return o.Status == OrderStatusPending || o.Status == OrderStatusActivated
}

func (o StopLossOrder) IsActive() bool { return o.Status == OrderStatusPending }

func validUint256(value sdk.Int, positive bool) bool {
	if value.IsNil() || value.IsNegative() || value.BigInt().Cmp(maxUint256) > 0 {
		return false
	}
	return !positive || value.IsPositive()
}

func validateOrderCommon(id uint64, user, stablecoin, token common.Address, amount sdk.Int, status OrderStatus,
	createdAt, expiresAt uint64) error {
	zero := common.Address{}
	switch {
	case id == 0:
		return fmt.Errorf("%w: order id 0", ErrInvalidGenesis)
	case user == zero || stablecoin == zero || token == zero:
		return fmt.Errorf("%w: order %d has a zero address", ErrInvalidGenesis, id)
	case stablecoin == token:
		return fmt.Errorf("%w: order %d trades a token against itself", ErrInvalidGenesis, id)
	case !validUint256(amount, true):
		return fmt.Errorf("%w: order %d amount", ErrInvalidGenesis, id)
	case status > OrderStatusExpired:
		return fmt.Errorf("%w: order %d status %d", ErrInvalidGenesis, id, status)
	case expiresAt != 0 && expiresAt <= createdAt:
		return fmt.Errorf("%w: order %d expires at or before its creation", ErrInvalidGenesis, id)
	}
	return nil
}

func (o LimitOrder) Validate() error {
	if err := validateOrderCommon(o.ID, o.User, o.Stablecoin, o.Token, o.Amount, o.Status, o.CreatedAt,
		o.ExpiresAt); err != nil {
		return err
	}
	if o.OrderType != OrderTypeLimitBuy && o.OrderType != OrderTypeLimitSell {
		return fmt.Errorf("%w: limit order %d type %d", ErrInvalidGenesis, o.ID, o.OrderType)
	}
	if o.Status == OrderStatusActivated {
		return fmt.Errorf("%w: limit order %d is ACTIVATED", ErrInvalidGenesis, o.ID)
	}
	if !validUint256(o.TargetPrice, true) {
		return fmt.Errorf("%w: limit order %d target price", ErrInvalidGenesis, o.ID)
	}
	return nil
}

func (o StopLimitOrder) Validate() error {
	if err := validateOrderCommon(o.ID, o.User, o.Stablecoin, o.Token, o.Amount, o.Status, o.CreatedAt,
		o.ExpiresAt); err != nil {
		return err
	}
	if o.OrderType != OrderTypeStopLimitBuy && o.OrderType != OrderTypeStopLimitSell {
		return fmt.Errorf("%w: stop-limit order %d type %d", ErrInvalidGenesis, o.ID, o.OrderType)
	}
	if !validUint256(o.StopPrice, true) || !validUint256(o.LimitPrice, true) {
		return fmt.Errorf("%w: stop-limit order %d prices", ErrInvalidGenesis, o.ID)
	}
	if (o.OrderType == OrderTypeStopLimitBuy && o.LimitPrice.LT(o.StopPrice)) ||
		(o.OrderType == OrderTypeStopLimitSell && o.LimitPrice.GT(o.StopPrice)) {
		return fmt.Errorf("%w: stop-limit order %d limit on the wrong side of the stop", ErrInvalidGenesis, o.ID)
	}
	return nil
}

func (o StopLossOrder) Validate() error {
	if err := validateOrderCommon(o.ID, o.User, o.Stablecoin, o.Token, o.Amount, o.Status, o.CreatedAt,
		o.ExpiresAt); err != nil {
		return err
	}
	if o.Status == OrderStatusActivated {
		return fmt.Errorf("%w: stop-loss order %d is ACTIVATED", ErrInvalidGenesis, o.ID)
	}
	if !validUint256(o.TriggerPrice, true) {
		return fmt.Errorf("%w: stop-loss order %d trigger price", ErrInvalidGenesis, o.ID)
	}
	return nil
}
