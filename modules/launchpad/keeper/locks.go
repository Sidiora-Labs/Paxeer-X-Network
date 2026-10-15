package keeper

import (
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TransientStore is the EIP-1153 transient storage of the EVM state the
// precompile run executes on. It lives for one transaction and reverts with
// the call frame that wrote it, so a lock taken by a run that reverts is gone
// with the revert.
type TransientStore interface {
	GetTransientState(addr common.Address, key common.Hash) common.Hash
	SetTransientState(addr common.Address, key, value common.Hash)
}

type lockRole uint8

const (
	lockPool lockRole = iota + 1
	lockFeeAccumulator
	lockTreasury
	lockRouter
	lockFeesRouter
)

// Lock is one nonReentrant guard of the contract surfaces: a pool's guard over
// swap, swapFor and syncReserves, or the single guard of the FeeAccumulator,
// Treasury, Router or FeesRouter role.
type Lock struct {
	role lockRole
	pool common.Address
}

// PoolLock is the guard of one pool's swap, swapFor and syncReserves.
func PoolLock(pool common.Address) Lock { return Lock{role: lockPool, pool: pool} }

var (
	// FeeAccumulatorLock spans recordFee, claim, burn, triggerAirdrop,
	// claimAirdrop, claimAirdropEpoch, sendLpRewards, claimOpticalSurplus,
	// claimLegacyAirdrop and allocateLegacyAirdrop.
	FeeAccumulatorLock = Lock{role: lockFeeAccumulator}
	// TreasuryLock is the Treasury guard, which reverts ReentrantCall().
	TreasuryLock = Lock{role: lockTreasury}
	// RouterLock spans the Router nonReentrant entry points.
	RouterLock = Lock{role: lockRouter}
	// FeesRouterLock spans the FeesRouter nonReentrant entry points.
	FeesRouterLock = Lock{role: lockFeesRouter}
)

var (
	launchpadAddress = common.HexToAddress(types.LaunchpadAddress)
	lockEntered      = common.BigToHash(common.Big1)
)

func (l Lock) slot() common.Hash { return crypto.Keccak256Hash([]byte{byte(l.role)}, l.pool.Bytes()) }

func (l Lock) refusal() error {
	if l.role == lockTreasury {
		return types.ErrReentrantCall
	}
	return types.ErrReentrancyGuardReentrantCall
}

// EnterLock takes lock in the transient store of the precompile run, refusing
// a second entry while it is held with ReentrancyGuardReentrantCall(), or
// ReentrantCall() for the Treasury. The caller defers the returned release so
// the lock is dropped on return; a reverted run drops it with the revert.
func (k *Keeper) EnterLock(store TransientStore, lock Lock) (func(), error) {
	if lock.role < lockPool || lock.role > lockFeesRouter {
		return nil, fmt.Errorf("%w: lock role %d", types.ErrInvalidConfiguration, lock.role)
	}
	slot := lock.slot()
	if store.GetTransientState(launchpadAddress, slot) == lockEntered {
		return nil, lock.refusal()
	}
	store.SetTransientState(launchpadAddress, slot, lockEntered)
	return func() { store.SetTransientState(launchpadAddress, slot, common.Hash{}) }, nil
}

// LockHeld reports whether lock is held in this run. Views never take a lock,
// so they answer while one is held.
func (k *Keeper) LockHeld(store TransientStore, lock Lock) bool {
	return store.GetTransientState(launchpadAddress, lock.slot()) == lockEntered
}
