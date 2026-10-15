package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Role ids of the fee-right collection (Launch-x SidioraNFT MINTER_ROLE and
// STRATEGY_SETTER_ROLE).
var (
	FeeRightMinterRole         = crypto.Keccak256Hash([]byte("MINTER_ROLE"))
	FeeRightStrategySetterRole = crypto.Keccak256Hash([]byte("STRATEGY_SETTER_ROLE"))
)

// Fee-right store prefixes. Every key below a prefix starts with the 20-byte
// collection address, so each collection is its own ERC721Enumerable.
var (
	feeRightPrefix           = []byte{0x40} // collection, id -> FeeRight
	feeRightBalancePrefix    = []byte{0x41} // collection, owner -> _balances
	feeRightOperatorPrefix   = []byte{0x42} // collection, owner, operator -> _operatorApprovals
	feeRightAllTokensPrefix  = []byte{0x43} // collection, index -> _allTokens
	feeRightAllIndexPrefix   = []byte{0x44} // collection, id -> _allTokensIndex
	feeRightOwnedPrefix      = []byte{0x45} // collection, owner, index -> _ownedTokens
	feeRightOwnedIndexPrefix = []byte{0x46} // collection, id -> _ownedTokensIndex
	feeRightPoolPrefix       = []byte{0x47} // collection, pool -> poolTokenId
	feeRightCollectionPrefix = []byte{0x48} // collection -> FeeRightCollectionState
)

func feeRightKey(prefix []byte, collection common.Address, parts ...[]byte) []byte {
	out := append(append([]byte(nil), prefix...), collection.Bytes()...)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func idBytes(id uint64) []byte { return binary.BigEndian.AppendUint64(nil, id) }

// FeeRightCollectionState holds the collection counters: nextTokenId,
// poolIndexMigrationCursor, poolIndexReady and the length of _allTokens.
type FeeRightCollectionState struct {
	NextTokenID              uint64 `json:"next_token_id"`
	PoolIndexMigrationCursor uint64 `json:"pool_index_migration_cursor"`
	PoolIndexReady           bool   `json:"pool_index_ready"`
	TotalSupply              uint64 `json:"total_supply"`
}

// GetFeeRightCollection returns collection's counters; a collection without a
// record is in the state SidioraNFT.initialize leaves.
func (k *Keeper) GetFeeRightCollection(ctx sdk.Context, collection common.Address) (FeeRightCollectionState, error) {
	bz := k.store(ctx).Get(feeRightKey(feeRightCollectionPrefix, collection))
	if bz == nil {
		return FeeRightCollectionState{NextTokenID: 1, PoolIndexReady: true}, nil
	}
	var state FeeRightCollectionState
	if err := json.Unmarshal(bz, &state); err != nil || state.NextTokenID == 0 ||
		state.TotalSupply >= state.NextTokenID {
		return FeeRightCollectionState{}, fmt.Errorf("%w: corrupt fee-right collection %s", types.ErrOverflow, collection)
	}
	return state, nil
}

func (k *Keeper) setFeeRightCollection(ctx sdk.Context, collection common.Address, state FeeRightCollectionState) error {
	bz, err := json.Marshal(state)
	if err != nil {
		return err
	}
	k.store(ctx).Set(feeRightKey(feeRightCollectionPrefix, collection), bz)
	return nil
}

// GetFeeRight returns token id of collection.
func (k *Keeper) GetFeeRight(ctx sdk.Context, collection common.Address, id uint64) (types.FeeRight, bool, error) {
	bz := k.store(ctx).Get(feeRightKey(feeRightPrefix, collection, idBytes(id)))
	if bz == nil {
		return types.FeeRight{}, false, nil
	}
	var right types.FeeRight
	if err := json.Unmarshal(bz, &right); err != nil || right.ID != id || !common.IsHexAddress(right.Owner) ||
		(right.Approved != "" && !common.IsHexAddress(right.Approved)) || !common.IsHexAddress(right.Pool) ||
		right.Strategy.Validate() != nil {
		return types.FeeRight{}, false, fmt.Errorf("%w: corrupt fee right %s/%d", types.ErrOverflow, collection, id)
	}
	return right, true, nil
}

func (k *Keeper) setFeeRight(ctx sdk.Context, collection common.Address, right types.FeeRight) error {
	bz, err := json.Marshal(right)
	if err != nil {
		return err
	}
	k.store(ctx).Set(feeRightKey(feeRightPrefix, collection, idBytes(right.ID)), bz)
	return nil
}

func (k *Keeper) mustFeeRight(ctx sdk.Context, collection common.Address, id uint64) (types.FeeRight, error) {
	right, found, err := k.GetFeeRight(ctx, collection, id)
	if err != nil {
		return types.FeeRight{}, err
	}
	if !found {
		return types.FeeRight{}, types.ErrTokenNotFound
	}
	return right, nil
}

// FeeRightOwnerOf is ownerOf(uint256).
func (k *Keeper) FeeRightOwnerOf(ctx sdk.Context, collection common.Address, id uint64) (common.Address, error) {
	right, err := k.mustFeeRight(ctx, collection, id)
	if err != nil {
		return common.Address{}, err
	}
	return common.HexToAddress(right.Owner), nil
}

// FeeRightBalanceOf is balanceOf(address).
func (k *Keeper) FeeRightBalanceOf(ctx sdk.Context, collection, owner common.Address) (uint64, error) {
	if owner == (common.Address{}) {
		return 0, types.ErrZeroAddress
	}
	return k.getUint64(ctx, feeRightKey(feeRightBalancePrefix, collection, owner.Bytes())), nil
}

// FeeRightGetApproved is getApproved(uint256).
func (k *Keeper) FeeRightGetApproved(ctx sdk.Context, collection common.Address, id uint64) (common.Address, error) {
	right, err := k.mustFeeRight(ctx, collection, id)
	if err != nil {
		return common.Address{}, err
	}
	if right.Approved == "" {
		return common.Address{}, nil
	}
	return common.HexToAddress(right.Approved), nil
}

// FeeRightIsApprovedForAll is isApprovedForAll(address,address).
func (k *Keeper) FeeRightIsApprovedForAll(ctx sdk.Context, collection, owner, operator common.Address) bool {
	return k.store(ctx).Has(feeRightKey(feeRightOperatorPrefix, collection, owner.Bytes(), operator.Bytes()))
}

// FeeRightPoolTokenID is poolTokenId(address): 0 for a pool without a right.
func (k *Keeper) FeeRightPoolTokenID(ctx sdk.Context, collection, pool common.Address) uint64 {
	return k.getUint64(ctx, feeRightKey(feeRightPoolPrefix, collection, pool.Bytes()))
}

// FeeRightByIndex is tokenByIndex(uint256).
func (k *Keeper) FeeRightByIndex(ctx sdk.Context, collection common.Address, index uint64) (uint64, error) {
	state, err := k.GetFeeRightCollection(ctx, collection)
	if err != nil {
		return 0, err
	}
	if index >= state.TotalSupply {
		return 0, types.ErrIndexOutOfBounds
	}
	return k.getUint64(ctx, feeRightKey(feeRightAllTokensPrefix, collection, idBytes(index))), nil
}

// FeeRightOfOwnerByIndex is tokenOfOwnerByIndex(address,uint256).
func (k *Keeper) FeeRightOfOwnerByIndex(ctx sdk.Context, collection, owner common.Address, index uint64) (uint64, error) {
	if index >= k.getUint64(ctx, feeRightKey(feeRightBalancePrefix, collection, owner.Bytes())) {
		return 0, types.ErrIndexOutOfBounds
	}
	return k.getUint64(ctx, feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(index))), nil
}

func (k *Keeper) isApprovedOrOwner(ctx sdk.Context, collection, spender common.Address, right types.FeeRight) bool {
	owner := common.HexToAddress(right.Owner)
	return spender == owner || (right.Approved != "" && common.HexToAddress(right.Approved) == spender) ||
		k.FeeRightIsApprovedForAll(ctx, collection, owner, spender)
}

func (k *Keeper) addOwnedFeeRight(ctx sdk.Context, collection, owner common.Address, id uint64) {
	balanceKey := feeRightKey(feeRightBalancePrefix, collection, owner.Bytes())
	index := k.getUint64(ctx, balanceKey)
	k.setUint64(ctx, feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(index)), id)
	k.setUint64(ctx, feeRightKey(feeRightOwnedIndexPrefix, collection, idBytes(id)), index)
	k.setUint64(ctx, balanceKey, index+1)
}

// removeOwnedFeeRight is _removeTokenFromOwnerEnumeration with the balance
// decrement of _transfer: the last owned id fills the removed slot.
func (k *Keeper) removeOwnedFeeRight(ctx sdk.Context, collection, owner common.Address, id uint64) error {
	store := k.store(ctx)
	balanceKey := feeRightKey(feeRightBalancePrefix, collection, owner.Bytes())
	balance := k.getUint64(ctx, balanceKey)
	indexKey := feeRightKey(feeRightOwnedIndexPrefix, collection, idBytes(id))
	index := k.getUint64(ctx, indexKey)
	if balance == 0 || index >= balance {
		return fmt.Errorf("%w: fee-right enumeration of %s", types.ErrOverflow, owner)
	}
	last := balance - 1
	if index != last {
		lastID := k.getUint64(ctx, feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(last)))
		k.setUint64(ctx, feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(index)), lastID)
		k.setUint64(ctx, feeRightKey(feeRightOwnedIndexPrefix, collection, idBytes(lastID)), index)
	}
	store.Delete(feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(last)))
	store.Delete(indexKey)
	k.setUint64(ctx, balanceKey, last)
	return nil
}

// MintFeeRight is the storage work of SidioraNFT.mint: it takes nextTokenId
// for pool and writes the token with its strategy, pool, balance and
// enumeration entries. The MINTER_ROLE check, the airdrop-support check of
// strategy 2 and the receiver hook belong to the calling surface.
func (k *Keeper) MintFeeRight(ctx sdk.Context, collection, to, pool common.Address, strategy types.FeeStrategy) (uint64, error) {
	if to == (common.Address{}) || pool == (common.Address{}) {
		return 0, types.ErrZeroAddress
	}
	if strategy.Validate() != nil {
		return 0, types.ErrInvalidStrategy
	}
	state, err := k.GetFeeRightCollection(ctx, collection)
	if err != nil {
		return 0, err
	}
	if !state.PoolIndexReady {
		return 0, types.ErrPoolIndexMigrationRequired
	}
	if k.FeeRightPoolTokenID(ctx, collection, pool) != 0 {
		return 0, types.ErrDuplicatePool
	}
	id := state.NextTokenID
	if id == ^uint64(0) {
		return 0, types.ErrOverflow
	}
	if _, found, err := k.GetFeeRight(ctx, collection, id); err != nil {
		return 0, err
	} else if found {
		return 0, types.ErrAlreadyMinted
	}
	if err := k.setFeeRight(ctx, collection, types.FeeRight{ID: id, Owner: to.Hex(), Strategy: strategy,
		Pool: pool.Hex()}); err != nil {
		return 0, err
	}
	k.setUint64(ctx, feeRightKey(feeRightPoolPrefix, collection, pool.Bytes()), id)
	k.setUint64(ctx, feeRightKey(feeRightAllTokensPrefix, collection, idBytes(state.TotalSupply)), id)
	k.setUint64(ctx, feeRightKey(feeRightAllIndexPrefix, collection, idBytes(id)), state.TotalSupply)
	k.addOwnedFeeRight(ctx, collection, to, id)
	state.NextTokenID = id + 1
	state.TotalSupply++
	if err := k.setFeeRightCollection(ctx, collection, state); err != nil {
		return 0, err
	}
	return id, nil
}

// ApproveFeeRight is approve(address,uint256) called by caller.
func (k *Keeper) ApproveFeeRight(ctx sdk.Context, collection, caller, to common.Address, id uint64) error {
	right, err := k.mustFeeRight(ctx, collection, id)
	if err != nil {
		return err
	}
	owner := common.HexToAddress(right.Owner)
	if caller != owner && !k.FeeRightIsApprovedForAll(ctx, collection, owner, caller) {
		return types.ErrNotApproved
	}
	right.Approved = ""
	if to != (common.Address{}) {
		right.Approved = to.Hex()
	}
	return k.setFeeRight(ctx, collection, right)
}

// SetFeeRightApprovalForAll is setApprovalForAll(address,bool) called by
// caller.
func (k *Keeper) SetFeeRightApprovalForAll(ctx sdk.Context, collection, caller, operator common.Address, approved bool) {
	key := feeRightKey(feeRightOperatorPrefix, collection, caller.Bytes(), operator.Bytes())
	if approved {
		k.store(ctx).Set(key, []byte{1})
		return
	}
	k.store(ctx).Delete(key)
}

// TransferFeeRight is transferFrom(address,address,uint256) called by caller;
// the receiver hook of safeTransferFrom belongs to the calling surface.
func (k *Keeper) TransferFeeRight(ctx sdk.Context, collection, caller, from, to common.Address, id uint64) error {
	right, err := k.mustFeeRight(ctx, collection, id)
	if err != nil {
		return err
	}
	if !k.isApprovedOrOwner(ctx, collection, caller, right) {
		return types.ErrNotApproved
	}
	if common.HexToAddress(right.Owner) != from {
		return types.ErrNotTokenOwner
	}
	if to == (common.Address{}) {
		return types.ErrZeroAddress
	}
	if from != to {
		if err := k.removeOwnedFeeRight(ctx, collection, from, id); err != nil {
			return err
		}
		k.addOwnedFeeRight(ctx, collection, to, id)
	}
	right.Owner = to.Hex()
	right.Approved = ""
	return k.setFeeRight(ctx, collection, right)
}

// SetFeeRightStrategy is the storage work of SidioraNFT.setFeeStrategy called
// by caller: the owner, the approved account, an operator or a
// STRATEGY_SETTER_ROLE member of the collection. It returns the old strategy;
// the airdrop-support check of strategy 2 belongs to the calling surface.
func (k *Keeper) SetFeeRightStrategy(ctx sdk.Context, collection, caller common.Address, id uint64, strategy types.FeeStrategy) (types.FeeStrategy, error) {
	right, err := k.mustFeeRight(ctx, collection, id)
	if err != nil {
		return 0, err
	}
	if strategy.Validate() != nil {
		return 0, types.ErrInvalidStrategy
	}
	if !k.isApprovedOrOwner(ctx, collection, caller, right) &&
		!k.store(ctx).Has(types.RoleKey(collection, FeeRightStrategySetterRole, caller)) {
		return 0, types.ErrNotApproved
	}
	old := right.Strategy
	right.Strategy = strategy
	return old, k.setFeeRight(ctx, collection, right)
}

// RequireFeeRightHolder is FeesRouter._requireNftOwner: caller must own
// token id of collection; a missing token refuses the same way.
func (k *Keeper) RequireFeeRightHolder(ctx sdk.Context, collection, caller common.Address, id uint64) (types.FeeRight, error) {
	right, found, err := k.GetFeeRight(ctx, collection, id)
	if err != nil {
		return types.FeeRight{}, err
	}
	if !found || common.HexToAddress(right.Owner) != caller {
		return types.FeeRight{}, types.ErrNotFeeRightsHolder
	}
	return right, nil
}

// RequireMarketFeeRight gates a fee-strategy execution on denom's market as
// FeesRouter does: caller holds the market's fee right, keyed by the market's
// collection and id, and that fee right carries strategy.
func (k *Keeper) RequireMarketFeeRight(ctx sdk.Context, caller common.Address, denom string, strategy types.FeeStrategy) (types.Market, types.FeeRight, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return types.Market{}, types.FeeRight{}, err
	}
	if !common.IsHexAddress(market.FeeRightCollection) || market.FeeRightID == 0 {
		return types.Market{}, types.FeeRight{}, types.ErrNotFeeRightsHolder
	}
	right, err := k.RequireFeeRightHolder(ctx, common.HexToAddress(market.FeeRightCollection), caller, market.FeeRightID)
	if err != nil {
		return types.Market{}, types.FeeRight{}, err
	}
	if right.Strategy != strategy {
		return types.Market{}, types.FeeRight{}, types.ErrWrongStrategy
	}
	return market, right, nil
}
