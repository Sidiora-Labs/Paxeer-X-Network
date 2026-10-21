package decode

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Seed is a perps.projection_seeds row: the state of an order or position
// that predates the indexed blocks, read through the perps getters at Height
// (the block before Block, the block of the event that needed it, or the
// latest height when Path is latest). The store builds the missing
// projection row from it (contracts/data-services.md section 2).
type Seed struct {
	Kind     string
	ID       string
	Block    uint64
	Height   uint64
	Path     string
	Account  common.Address
	MarketID string
	IsLong   bool
	SizeUSD  string
	Order    *OrderSeed
	Position *PositionSeed
}

// OrderSeed is the getOrderDetails record of an order, with the remaining
// size and projection status derived from it.
type OrderSeed struct {
	OrderType        uint8
	TimeInForce      uint8
	OrderStatus      uint8
	ReduceOnly       bool
	LimitPrice       string
	TriggerPrice     string
	AcceptablePrice  string
	SizeBase         string
	FilledBase       string
	RemainingSizeUSD string
	Status           string
	Deadline         uint64
	Cloid            [16]byte
}

// PositionSeed is the getPosition and getPositionDetails record of a
// position; a zero take-profit or stop-loss price is unset.
type PositionSeed struct {
	CollateralUSD    string
	CollateralAmount string
	CollateralToken  common.Address
	EntryPrice       string
	Leverage         string
	TakeProfitPrice  *string
	StopLossPrice    *string
}

// Order statuses of contracts/perps.md section 2 and order types that rest
// only once triggered.
const (
	orderUntriggered uint8 = 0
	orderResting     uint8 = 1
	orderFilled      uint8 = 2
	orderCancelled   uint8 = 3
	orderExpired     uint8 = 4
	orderTypeLimit   uint8 = 0
)

type orderDetails struct {
	OrderId         *big.Int
	User            common.Address
	MarketId        *big.Int
	IsLong          bool
	OrderType       uint8
	TimeInForce     uint8
	Status          uint8
	ReduceOnly      bool
	LimitPrice      *big.Int
	TriggerPrice    *big.Int
	AcceptablePrice *big.Int
	SizeUsd         *big.Int
	SizeBase        *big.Int
	FilledBase      *big.Int
	Leverage        *big.Int
	Deadline        uint64
	Cloid           [16]byte
	Sequence        uint64
}

type positionDetails struct {
	PositionId       *big.Int
	User             common.Address
	MarketId         *big.Int
	IsLong           bool
	SizeBase         *big.Int
	SizeUsdAtMark    *big.Int
	EntryPrice       *big.Int
	Leverage         *big.Int
	UnrealizedPnlUsd *big.Int
	FundingOwedUsd   *big.Int
	LiquidationPrice *big.Int
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	OpenedAt         *big.Int
}

// newSeed builds the seed of g from the unpacked outputs of its methods:
// getOrderDetails for an order, getPosition then getPositionDetails for a
// position.
func newSeed(g stateGetter, outputs [][]any) (Seed, error) {
	switch g.what {
	case "order":
		var out struct{ Details orderDetails }
		if err := g.methods[0].Outputs.Copy(&out, outputs[0]); err != nil {
			return Seed{}, err
		}
		return orderSeed(out.Details)
	case "position":
		var head struct {
			User             common.Address
			MarketId         *big.Int
			IsLong           bool
			SizeUsd          *big.Int
			CollateralUsd    *big.Int
			CollateralToken  common.Address
			CollateralAmount *big.Int
			EntryPrice       *big.Int
			Timestamp        *big.Int
			Active           bool
		}
		if err := g.methods[0].Outputs.Copy(&head, outputs[0]); err != nil {
			return Seed{}, err
		}
		var out struct{ Details positionDetails }
		if err := g.methods[1].Outputs.Copy(&out, outputs[1]); err != nil {
			return Seed{}, err
		}
		d := out.Details
		switch {
		case head.User == (common.Address{}) || !head.Active:
			return Seed{}, errors.New("getPosition returned no open position")
		case d.User != head.User || d.MarketId.Cmp(head.MarketId) != 0 || d.IsLong != head.IsLong || d.EntryPrice.Cmp(head.EntryPrice) != 0:
			return Seed{}, errors.New("getPosition and getPositionDetails disagree")
		}
		return Seed{Account: head.User, MarketID: head.MarketId.String(), IsLong: head.IsLong, SizeUSD: head.SizeUsd.String(),
			Position: &PositionSeed{
				CollateralUSD: head.CollateralUsd.String(), CollateralAmount: head.CollateralAmount.String(),
				CollateralToken: head.CollateralToken, EntryPrice: head.EntryPrice.String(), Leverage: d.Leverage.String(),
				TakeProfitPrice: nonZero(d.TakeProfitPrice), StopLossPrice: nonZero(d.StopLossPrice),
			}}, nil
	}
	return Seed{}, fmt.Errorf("no seed of %s", g.what)
}

// orderSeed derives the remaining size and the projection status of d. The
// remaining size is size_usd x (size_base - filled_base) / size_base rounded
// down, size_usd for an unfilled order and 0 for a filled one; an order
// resting after its trigger is triggered.
func orderSeed(d orderDetails) (Seed, error) {
	if d.User == (common.Address{}) {
		return Seed{}, errors.New("getOrderDetails returned no account")
	}
	if d.SizeBase.Sign() <= 0 || d.FilledBase.Cmp(d.SizeBase) > 0 {
		return Seed{}, fmt.Errorf("getOrderDetails filled %s of size %s", d.FilledBase, d.SizeBase)
	}
	left := new(big.Int).Sub(d.SizeBase, d.FilledBase)
	remaining := left.Mul(left, d.SizeUsd).Quo(left, d.SizeBase)
	var status string
	switch d.Status {
	case orderUntriggered:
		status = "active"
	case orderResting:
		status = "active"
		if d.OrderType != orderTypeLimit {
			status = "triggered"
		}
	case orderFilled:
		status = "filled"
	case orderCancelled:
		status = "cancelled"
	case orderExpired:
		status = "expired"
	default:
		return Seed{}, fmt.Errorf("getOrderDetails status %d", d.Status)
	}
	return Seed{Account: d.User, MarketID: d.MarketId.String(), IsLong: d.IsLong, SizeUSD: d.SizeUsd.String(),
		Order: &OrderSeed{
			OrderType: d.OrderType, TimeInForce: d.TimeInForce, OrderStatus: d.Status, ReduceOnly: d.ReduceOnly,
			LimitPrice: d.LimitPrice.String(), TriggerPrice: d.TriggerPrice.String(), AcceptablePrice: d.AcceptablePrice.String(),
			SizeBase: d.SizeBase.String(), FilledBase: d.FilledBase.String(), RemainingSizeUSD: remaining.String(), Status: status,
			Deadline: d.Deadline, Cloid: d.Cloid,
		}}, nil
}

func nonZero(v *big.Int) *string {
	if v.Sign() == 0 {
		return nil
	}
	s := v.String()
	return &s
}
