package coverage

import (
	"bytes"
	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Permission is a selector-scoped contract authorization observed at a pinned
// block. From is inclusive; To is exclusive (zero means open-ended). Caller identity must come from authenticated emitter-call evidence;
// Swap.sender is trader identity and cannot substitute for the caller.
type Permission struct {
	ChainID         uint64
	Emitter, Caller common.Address
	Selector        [4]byte
	From, To        uint64
}

func CallerAllowed(entries []Permission, chain uint64, emitter, caller common.Address, selector [4]byte, block uint64) bool {
	if chain != 125 || emitter == (common.Address{}) || caller == (common.Address{}) {
		return false
	}
	for _, p := range entries {
		if p.ChainID == chain && p.Emitter == emitter && p.Caller == caller && p.Selector == selector && block >= p.From && (p.To == 0 || block < p.To) {
			return true
		}
	}
	return false
}

type PermissionEpoch struct {
	ChainID         uint64
	Emitter, Caller common.Address
	Selector        [4]byte
	From            Position
	Until           *Position
}

func CallerAllowedAt(entries []PermissionEpoch, chain uint64, emitter, caller common.Address, selector [4]byte, at Position) bool {
	if chain != 125 || emitter == (common.Address{}) || caller == (common.Address{}) || selector == ([4]byte{}) {
		return false
	}
	matches := 0
	for _, p := range entries {
		if p.Until != nil && !p.From.Before(*p.Until) {
			return false
		}
		if p.ChainID == chain && p.Emitter == emitter && p.Caller == caller && p.Selector == selector && !at.Before(p.From) && (p.Until == nil || at.Before(*p.Until)) {
			matches++
		}
	}
	return matches == 1
}

func TypedSelector(event string) ([4]byte, bool) {
	method := "emit" + event
	switch event {
	case "ContractUpgraded":
		method = "emitUpgraded"
	case "CircuitBreakerTriggered":
		method = "emitCircuitBreaker"
	}
	bundles, err := catalog.Bundles()
	if err != nil {
		return [4]byte{}, false
	}
	for _, b := range bundles {
		if b.Name != "EventEmitter" {
			continue
		}
		a, err := abi.JSON(bytes.NewReader(b.ABI))
		if err != nil {
			return [4]byte{}, false
		}
		m, ok := a.Methods[method]
		if !ok || len(m.ID) != 4 {
			return [4]byte{}, false
		}
		var selector [4]byte
		copy(selector[:], m.ID)
		return selector, true
	}
	return [4]byte{}, false
}
