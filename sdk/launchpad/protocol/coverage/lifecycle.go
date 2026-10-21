package coverage

import (
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type OrderState struct {
	Created, Active, Triggered, Filled bool
	Terminal                           string
	MinimumOutput, RefundAmount        *big.Int
	ExecutionHash                      common.Hash
	Last                               Position
}
type ControlState struct {
	Paused         bool
	Reattesting    bool
	DisabledFrom   uint64
	Implementation common.Address
	Last           Position
}
type ImplementationReview struct {
	Contract, Implementation common.Address
	Family, Version          string
	CodeHash                 common.Hash
}

type Lifecycle struct {
	Orders              map[string]OrderState
	Controls            map[common.Address]ControlState
	Registry            *catalog.Registry
	Reviewed            []ImplementationReview
	InvalidatedOpticals map[common.Address]Position
}

func NewLifecycle(registry *catalog.Registry, reviews []ImplementationReview) (*Lifecycle, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry required")
	}
	for i, r := range reviews {
		if r.Contract == (common.Address{}) || r.Implementation == (common.Address{}) || r.CodeHash == (common.Hash{}) || !registry.RevisionKnown(r.Family, r.Version) {
			return nil, fmt.Errorf("unreviewed implementation")
		}
		for _, old := range reviews[:i] {
			if old.Contract == r.Contract && old.Implementation == r.Implementation {
				return nil, fmt.Errorf("duplicate implementation review")
			}
		}
	}
	return &Lifecycle{Orders: map[string]OrderState{}, Controls: map[common.Address]ControlState{}, Registry: registry, Reviewed: append([]ImplementationReview(nil), reviews...), InvalidatedOpticals: map[common.Address]Position{}}, nil
}
func orderKey(e Effect) (string, error) {
	id, ok := e.Fields["orderId"].(*big.Int)
	if !ok || id == nil || id.Sign() < 0 {
		return "", fmt.Errorf("native order identity required")
	}
	return fmt.Sprintf("%d/%s/%s", e.Identity.ChainID, e.Identity.Emitter.Hex(), id.String()), nil
}
func (s *Lifecycle) apply(e Effect) error {
	if e.Role == "OpticalRegistry" && e.Kind == "optical_configuration" {
		optical, ok := e.Fields["optical"].(common.Address)
		if !ok || optical == (common.Address{}) {
			return fmt.Errorf("optical identity missing")
		}
		old, exists := s.InvalidatedOpticals[optical]
		if exists && !old.Before(e.Position) {
			return fmt.Errorf("out of order optical invalidation")
		}
		s.InvalidatedOpticals[optical] = e.Position
	}
	if e.Role == "PECOROrders" && (e.Kind == "order_created" || e.Kind == "order_triggered" || e.Kind == "order_filled" || e.Kind == "order_unchanged" || e.Kind == "expired_refunded" || e.Kind == "order_cancelled" || e.Kind == "minimum_output") {
		key, err := orderKey(e)
		if err != nil {
			return err
		}
		old := s.Orders[key]
		if old.Created && !old.Last.Before(e.Position) {
			return fmt.Errorf("out of order native order transition")
		}
		if e.Kind == "order_created" {
			if old.Created {
				return fmt.Errorf("duplicate order creation")
			}
			old.Created = true
			old.Active = true
		} else {
			if !old.Created || !old.Active || old.Filled || old.Terminal != "" {
				return fmt.Errorf("order transition requires active native order")
			}
			switch e.Kind {
			case "order_triggered":
				old.Triggered = true
			case "order_filled":
				old.Active = false
				old.Filled = true
				old.ExecutionHash = e.Identity.TransactionHash
			case "expired_refunded":
				amount, err := unsigned(e.Fields["amount"])
				if err != nil {
					return err
				}
				old.Active = false
				old.Terminal = "expired_refunded"
				old.RefundAmount = amount
			case "order_cancelled":
				old.Active = false
				old.Terminal = "cancelled"
			case "minimum_output":
				amount, err := unsigned(e.Fields["minimum"])
				if err != nil {
					return err
				}
				old.MinimumOutput = amount
			}
		}
		old.Last = e.Position
		s.Orders[key] = old
	}
	if e.Kind == "control_transition" {
		old := s.Controls[e.Identity.Emitter]
		if old.Last != (Position{}) && !old.Last.Before(e.Position) {
			return fmt.Errorf("out of order control transition")
		}
		switch e.Event {
		case "PauseToggled":
			paused, ok := e.Fields["paused"].(bool)
			if !ok {
				return fmt.Errorf("pause state missing")
			}
			old.Paused = paused
		case "Upgraded":
			implementation, ok := e.Fields["implementation"].(common.Address)
			if !ok || implementation == (common.Address{}) {
				return fmt.Errorf("implementation missing")
			}
			known := false
			for _, r := range s.Reviewed {
				dep, exists := s.Registry.DeploymentAt(e.Identity.ChainID, e.Identity.Emitter, e.Position.Block)
				if exists && r.Contract == e.Identity.Emitter && r.Implementation == implementation && r.Family == dep.Family {
					known = true
				}
			}
			old.Implementation = implementation
			if !known {
				old.Reattesting = true
				if old.DisabledFrom == 0 || e.Position.Block < old.DisabledFrom {
					old.DisabledFrom = e.Position.Block
				}
			}
		}
		old.Last = e.Position
		s.Controls[e.Identity.Emitter] = old
	}
	return nil
}
func (s *Lifecycle) WriteAllowed(contract common.Address, block uint64) bool {
	if s == nil || s.Registry == nil {
		return false
	}
	if _, registered := s.Registry.DeploymentAt(125, contract, block); !registered {
		return false
	}
	state := s.Controls[contract]
	return !state.Paused && (state.DisabledFrom == 0 || block < state.DisabledFrom)
}
