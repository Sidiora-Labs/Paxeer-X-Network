package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// FeesRouterRole is FeeAccumulator.FEES_ROUTER_ROLE, the role triggerAirdrop
// requires of its caller.
var FeesRouterRole = [32]byte(crypto.Keccak256Hash([]byte("FEES_ROUTER_ROLE")))

var (
	// poolAirdropEpochPrefix keys the FeeAccumulator epoch fields of one pool
	// and epoch: _airdropEpochAmount and slots 15 to 18, 23 and 24.
	poolAirdropEpochPrefix = []byte{0x26}
	// poolAirdropClaimPrefix keys _airdropClaimed of one pool, holder and
	// epoch.
	poolAirdropClaimPrefix = []byte{0x27}

	checkpointPastBalance     = crypto.Keccak256([]byte("getPastBalance(address,uint256)"))[:4]
	checkpointPastTotalSupply = crypto.Keccak256([]byte("getPastTotalSupply(uint256)"))[:4]
)

func poolAirdropEpochKey(pool common.Address, epoch uint64) []byte {
	key := append(append([]byte(nil), poolAirdropEpochPrefix...), pool.Bytes()...)
	return binary.BigEndian.AppendUint64(key, epoch)
}

func poolAirdropClaimKey(pool, holder common.Address, epoch uint64) []byte {
	key := append(append(append([]byte(nil), poolAirdropClaimPrefix...), pool.Bytes()...), holder.Bytes()...)
	return binary.BigEndian.AppendUint64(key, epoch)
}

// poolMarket is the market a pool surface serves; false when pool is not a
// registered pool surface.
func (k *Keeper) poolMarket(ctx sdk.Context, pool common.Address) (types.Market, bool, error) {
	surface, found, err := k.GetSurface(ctx, pool)
	if err != nil || !found || surface.Role != types.RolePool {
		return types.Market{}, false, err
	}
	market, found := k.GetMarket(ctx, surface.Market)
	if !found || market.Generation == types.GenerationNative || common.HexToAddress(market.PoolAddress) != pool {
		return types.Market{}, false, fmt.Errorf("%w: pool surface %s names market %q", types.ErrInvalidConfiguration,
			pool.Hex(), surface.Market)
	}
	return market, true, nil
}

func validPoolAirdropEpoch(record types.PoolAirdropEpoch) error {
	invalid := fmt.Errorf("%w: %s/%d", types.ErrAirdropBasis, record.Pool, record.Epoch)
	if !common.IsHexAddress(record.Pool) || common.HexToAddress(record.Pool).Hex() != record.Pool ||
		common.HexToAddress(record.Pool) == (common.Address{}) || record.Epoch == 0 {
		return invalid
	}
	for _, value := range []sdk.Int{record.Amount, record.EligibleSupply, record.Remaining, record.ClaimedSupply,
		record.LegacyRemaining} {
		if !uint256Amount(value) {
			return invalid
		}
	}
	if record.Remaining.GT(record.Amount) || record.ClaimedSupply.GT(record.EligibleSupply) {
		return invalid
	}
	if record.LegacyRoot == "" {
		if !record.LegacyRemaining.IsZero() {
			return invalid
		}
		return nil
	}
	root := common.HexToHash(record.LegacyRoot)
	if len(record.LegacyRoot) != 2+2*common.HashLength || root.Hex() != record.LegacyRoot || root == (common.Hash{}) ||
		!record.EligibleSupply.IsZero() {
		return invalid
	}
	return nil
}

// GetPoolAirdropEpoch is the epoch fields of pool's airdrop epoch: the amount,
// airdropSnapshotBlock, airdropEligibleSupply, airdropEpochRemaining,
// airdropClaimedSupply, legacyAirdropRoot and legacyAirdropRemaining.
func (k *Keeper) GetPoolAirdropEpoch(ctx sdk.Context, pool common.Address, epoch uint64) (types.PoolAirdropEpoch,
	bool, error) {
	bz := k.store(ctx).Get(poolAirdropEpochKey(pool, epoch))
	if bz == nil {
		return types.PoolAirdropEpoch{}, false, nil
	}
	var record types.PoolAirdropEpoch
	if err := json.Unmarshal(bz, &record); err != nil {
		return types.PoolAirdropEpoch{}, false, fmt.Errorf("%w: %s/%d: %v", types.ErrAirdropBasis, pool.Hex(), epoch, err)
	}
	if err := validPoolAirdropEpoch(record); err != nil {
		return types.PoolAirdropEpoch{}, false, err
	}
	if record.Pool != pool.Hex() || record.Epoch != epoch {
		return types.PoolAirdropEpoch{}, false, fmt.Errorf("%w: %s/%d", types.ErrAirdropBasis, pool.Hex(), epoch)
	}
	return record, true, nil
}

func (k *Keeper) writePoolAirdropEpoch(ctx sdk.Context, record types.PoolAirdropEpoch) error {
	if err := validPoolAirdropEpoch(record); err != nil {
		return err
	}
	bz, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: %s/%d: %v", types.ErrAirdropBasis, record.Pool, record.Epoch, err)
	}
	k.store(ctx).Set(poolAirdropEpochKey(common.HexToAddress(record.Pool), record.Epoch), bz)
	return nil
}

// SetPoolAirdropEpoch writes the epoch fields of an epoch the pool's market
// has opened, as the migration carries them from the FeeAccumulator.
func (k *Keeper) SetPoolAirdropEpoch(ctx sdk.Context, record types.PoolAirdropEpoch) error {
	if err := validPoolAirdropEpoch(record); err != nil {
		return err
	}
	market, found, err := k.poolMarket(ctx, common.HexToAddress(record.Pool))
	if err != nil {
		return err
	}
	if !found || record.Epoch > market.AirdropEpoch {
		return fmt.Errorf("%w: %s/%d is not an opened epoch", types.ErrAirdropBasis, record.Pool, record.Epoch)
	}
	return k.writePoolAirdropEpoch(ctx, record)
}

// HasClaimedPoolAirdrop is _airdropClaimed[pool][holder][epoch].
func (k *Keeper) HasClaimedPoolAirdrop(ctx sdk.Context, pool, holder common.Address, epoch uint64) bool {
	return k.store(ctx).Has(poolAirdropClaimKey(pool, holder, epoch))
}

// readCheckpoint runs one checkpoint view of the pool-token contract by nested
// staticcall from the FeeAccumulator.
func readCheckpoint(ctx sdk.Context, caller NestedCaller, accumulator, token common.Address, input []byte) (*big.Int,
	error) {
	ret, err := caller.StaticCall(ctx, accumulator, token, input)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", types.ErrUnsupportedSnapshotToken, token.Hex(), err)
	}
	if len(ret) != 32 {
		return nil, fmt.Errorf("%w: %s returned %d bytes", types.ErrUnsupportedSnapshotToken, token.Hex(), len(ret))
	}
	return new(big.Int).SetBytes(ret), nil
}

func pastBalance(ctx sdk.Context, caller NestedCaller, accumulator, token, account common.Address,
	block uint64) (*big.Int, error) {
	return readCheckpoint(ctx, caller, accumulator, token,
		erc20Call(checkpointPastBalance, addressWord(account), amountWord(new(big.Int).SetUint64(block))))
}

// TriggerAirdrop is FeeAccumulator.triggerAirdrop sent by sender: the pool's
// accumulated fees open epoch e+1 with snapshot block block.number-1 and the
// eligible supply getPastTotalSupply less the pool and dead-address balances
// at that block, read from the pool-token contract. The USDL stays in
// FeeHeld of the pool's FeeAccumulator.
func (k *Keeper) TriggerAirdrop(ctx sdk.Context, caller NestedCaller, sender, pool common.Address) (sdk.Int, error) {
	if err := k.CheckRole(ctx, k.generation2.FeeAccumulator, FeesRouterRole, sender); err != nil {
		return sdk.Int{}, err
	}
	market, found, err := k.poolMarket(ctx, pool)
	if err != nil {
		return sdk.Int{}, err
	}
	if !found || !market.AccumulatedFees.IsPositive() {
		return sdk.Int{}, types.ErrNoFeesAccumulated
	}
	amount := market.AccumulatedFees
	token := common.HexToAddress(market.TokenAddress)
	if ctx.BlockHeight() <= 0 || len(k.evmKeeper.GetCode(ctx, token)) == 0 {
		return sdk.Int{}, fmt.Errorf("%w: %s", types.ErrUnsupportedSnapshotToken, token.Hex())
	}
	snapshot := uint64(ctx.BlockHeight() - 1)
	accumulator := common.HexToAddress(market.FeeAccumulator)
	eligible, err := readCheckpoint(ctx, caller, accumulator, token,
		erc20Call(checkpointPastTotalSupply, amountWord(new(big.Int).SetUint64(snapshot))))
	if err != nil {
		return sdk.Int{}, err
	}
	for _, excluded := range []common.Address{pool, common.HexToAddress(types.DeadAddress)} {
		balance, err := pastBalance(ctx, caller, accumulator, token, excluded, snapshot)
		if err != nil {
			return sdk.Int{}, err
		}
		if eligible.Cmp(balance) < 0 {
			return sdk.Int{}, fmt.Errorf("%w: eligible supply below the balance of %s", types.ErrOverflow, excluded.Hex())
		}
		eligible.Sub(eligible, balance)
	}
	if eligible.Sign() == 0 {
		return sdk.Int{}, types.ErrZeroAmount
	}
	if market.AirdropEpoch == math.MaxUint64 {
		return sdk.Int{}, types.ErrOverflow
	}
	balance := market.AirdropBalance.Add(amount)
	if !uint256Amount(balance) {
		return sdk.Int{}, types.ErrOverflow
	}
	market.AccumulatedFees = sdk.ZeroInt()
	market.AirdropEpoch++
	market.AirdropBalance = balance
	record := types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: market.AirdropEpoch, Amount: amount, SnapshotBlock: snapshot,
		EligibleSupply: sdk.NewIntFromBigInt(eligible), Remaining: amount, ClaimedSupply: sdk.ZeroInt(),
		LegacyRemaining: sdk.ZeroInt()}
	if err := k.writePoolAirdropEpoch(ctx, record); err != nil {
		return sdk.Int{}, err
	}
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAirdropTriggered,
		sdk.NewAttribute(types.AttributeKeyDenom, market.Denom),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		sdk.NewAttribute(types.AttributeKeyEpoch, strconv.FormatUint(market.AirdropEpoch, 10))))
	return amount, nil
}

// ClaimAirdropEpoch is FeeAccumulator.claimAirdropEpoch: holder takes
// epochAmount*getPastBalance(holder, snapshot)/eligibleSupply of pool's epoch
// once, the final eligible claimant takes the rounding residue, and the USDL
// is paid from FeeHeld by USDL.transfer from the launchpad module EVM
// address.
func (k *Keeper) ClaimAirdropEpoch(ctx sdk.Context, caller NestedCaller, pool, holder common.Address,
	epoch uint64) (sdk.Context, sdk.Int, error) {
	market, found, err := k.poolMarket(ctx, pool)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if !found || epoch == 0 || epoch > market.AirdropEpoch {
		return ctx, sdk.Int{}, types.ErrAirdropNotTriggered
	}
	if holder == pool || holder == common.HexToAddress(types.DeadAddress) || holder == (common.Address{}) {
		return ctx, sdk.Int{}, types.ErrUnauthorized
	}
	if k.HasClaimedPoolAirdrop(ctx, pool, holder, epoch) {
		return ctx, sdk.Int{}, types.ErrAlreadyClaimed
	}
	record, found, err := k.GetPoolAirdropEpoch(ctx, pool, epoch)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if !found || record.EligibleSupply.IsZero() {
		return ctx, sdk.Int{}, types.ErrAirdropNotTriggered
	}
	accumulator := common.HexToAddress(market.FeeAccumulator)
	balance, err := pastBalance(ctx, caller, accumulator, common.HexToAddress(market.TokenAddress), holder,
		record.SnapshotBlock)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if balance.Sign() == 0 {
		return ctx, sdk.Int{}, types.ErrZeroAmount
	}
	product := new(big.Int).Mul(record.Amount.BigInt(), balance)
	if product.Cmp(types.MaxUint256()) > 0 {
		return ctx, sdk.Int{}, types.ErrOverflow
	}
	amount := sdk.NewIntFromBigInt(product.Quo(product, record.EligibleSupply.BigInt()))
	claimedSupply := record.ClaimedSupply.Add(sdk.NewIntFromBigInt(balance))
	if !uint256Amount(claimedSupply) {
		return ctx, sdk.Int{}, types.ErrOverflow
	}
	if claimedSupply.Equal(record.EligibleSupply) {
		amount = record.Remaining
	}
	if record.Remaining.LT(amount) || market.AirdropBalance.LT(amount) {
		return ctx, sdk.Int{}, types.ErrOverflow
	}
	record.ClaimedSupply = claimedSupply
	record.Remaining = record.Remaining.Sub(amount)
	market.AirdropBalance = market.AirdropBalance.Sub(amount)
	if err := k.writePoolAirdropEpoch(ctx, record); err != nil {
		return ctx, sdk.Int{}, err
	}
	return k.payAirdrop(ctx, caller, market, accumulator, holder, epoch, amount)
}

// ClaimLegacyAirdrop is FeeAccumulator.claimLegacyAirdrop: amount is paid to
// holder once when the sorted-pair Merkle proof of the leaf
// keccak256(abi.encode(pool, epoch, holder, amount)) reaches legacyAirdropRoot
// and legacyAirdropRemaining covers it.
func (k *Keeper) ClaimLegacyAirdrop(ctx sdk.Context, caller NestedCaller, pool common.Address, epoch uint64,
	holder common.Address, amount sdk.Int, proof []common.Hash) (sdk.Context, error) {
	if !uint256Amount(amount) {
		return ctx, types.ErrOverflow
	}
	if amount.IsZero() || holder == (common.Address{}) {
		return ctx, types.ErrUnauthorized
	}
	if k.HasClaimedPoolAirdrop(ctx, pool, holder, epoch) {
		return ctx, types.ErrAlreadyClaimed
	}
	hash := crypto.Keccak256Hash(addressWord(pool), amountWord(new(big.Int).SetUint64(epoch)), addressWord(holder),
		amountWord(amount.BigInt()))
	for _, sibling := range proof {
		if hash.Cmp(sibling) < 0 {
			hash = crypto.Keccak256Hash(hash.Bytes(), sibling.Bytes())
		} else {
			hash = crypto.Keccak256Hash(sibling.Bytes(), hash.Bytes())
		}
	}
	market, found, err := k.poolMarket(ctx, pool)
	if err != nil {
		return ctx, err
	}
	if !found {
		return ctx, types.ErrUnauthorized
	}
	record, found, err := k.GetPoolAirdropEpoch(ctx, pool, epoch)
	if err != nil {
		return ctx, err
	}
	if !found || record.LegacyRoot == "" || hash != common.HexToHash(record.LegacyRoot) ||
		amount.GT(record.LegacyRemaining) {
		return ctx, types.ErrUnauthorized
	}
	if market.AirdropBalance.LT(amount) {
		return ctx, types.ErrOverflow
	}
	record.LegacyRemaining = record.LegacyRemaining.Sub(amount)
	market.AirdropBalance = market.AirdropBalance.Sub(amount)
	if err := k.writePoolAirdropEpoch(ctx, record); err != nil {
		return ctx, err
	}
	ctx, _, err = k.payAirdrop(ctx, caller, market, common.HexToAddress(market.FeeAccumulator), holder, epoch, amount)
	return ctx, err
}

// payAirdrop marks holder's claim, writes the market's airdrop balance and
// pays amount from FeeHeld of accumulator. A claim whose share rounds to zero
// still makes the zero USDL.transfer the contract makes.
func (k *Keeper) payAirdrop(ctx sdk.Context, caller NestedCaller, market types.Market, accumulator, holder common.Address,
	epoch uint64, amount sdk.Int) (sdk.Context, sdk.Int, error) {
	pool := common.HexToAddress(market.PoolAddress)
	k.store(ctx).Set(poolAirdropClaimKey(pool, holder, epoch), []byte{1})
	k.setMarket(ctx, market)
	if err := k.subLedgerAmount(ctx, totalLiabilitiesKey, amount); err != nil {
		return ctx, sdk.Int{}, err
	}
	var err error
	if amount.IsZero() {
		ctx, err = callQuote(ctx, caller, k.ModuleEVMAddress(ctx), k.quoteToken(ctx),
			erc20Call(erc20Transfer, addressWord(holder), amountWord(new(big.Int))), types.ErrTransferFailed)
	} else {
		ctx, err = k.PayQuote(ctx, caller, FeeHeldLedger(accumulator), holder, amount)
	}
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAirdropClaimed,
		sdk.NewAttribute(types.AttributeKeyDenom, market.Denom),
		sdk.NewAttribute(types.AttributeKeyHolder, holder.Hex()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		sdk.NewAttribute(types.AttributeKeyEpoch, strconv.FormatUint(epoch, 10))))
	return ctx, amount, nil
}
