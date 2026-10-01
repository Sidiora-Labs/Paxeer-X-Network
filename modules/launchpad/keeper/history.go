package keeper

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
)

// holdingHistoryVersion tags the holding-history marker encoding.
const holdingHistoryVersion = 1

// holdingHistory is a launched denom's marker: balances are checkpointed
// from boundary FirstEpoch on, and CurrentEpoch is the epoch being recorded.
type holdingHistory struct {
	FirstEpoch   uint64
	CurrentEpoch uint64
}

type holdingEntry struct {
	Epoch   uint64
	Balance sdk.Int
}

func (k *Keeper) isLaunchedDenom(denom string) bool {
	return strings.HasPrefix(denom, "factory/"+k.ModuleAddress().String()+"/")
}

func (k *Keeper) getHoldingHistory(ctx sdk.Context, denom string) (holdingHistory, bool, error) {
	bz := k.store(ctx).Get(types.HoldingHistoryKey(denom))
	if bz == nil {
		return holdingHistory{}, false, nil
	}
	if len(bz) != 17 || bz[0] != holdingHistoryVersion {
		return holdingHistory{}, false, fmt.Errorf("%w: malformed marker for %s", types.ErrHoldingHistory, denom)
	}
	history := holdingHistory{FirstEpoch: binary.BigEndian.Uint64(bz[1:9]), CurrentEpoch: binary.BigEndian.Uint64(bz[9:17])}
	if history.FirstEpoch == 0 || history.FirstEpoch > history.CurrentEpoch {
		return holdingHistory{}, false, fmt.Errorf("%w: marker epochs out of order for %s", types.ErrHoldingHistory, denom)
	}
	return history, true, nil
}

func (k *Keeper) setHoldingHistory(ctx sdk.Context, denom string, history holdingHistory) {
	bz := []byte{holdingHistoryVersion}
	bz = binary.BigEndian.AppendUint64(bz, history.FirstEpoch)
	bz = binary.BigEndian.AppendUint64(bz, history.CurrentEpoch)
	k.store(ctx).Set(types.HoldingHistoryKey(denom), bz)
}

// HoldingCheckpointCount is the number of balance checkpoints recorded for
// holder's denom.
func (k *Keeper) HoldingCheckpointCount(ctx sdk.Context, denom string, holder sdk.AccAddress) (uint64, error) {
	bz := k.store(ctx).Get(types.HoldingCountKey(denom, holder))
	if bz == nil {
		return 0, nil
	}
	if len(bz) != 8 {
		return 0, fmt.Errorf("%w: malformed checkpoint count", types.ErrHoldingHistory)
	}
	return binary.BigEndian.Uint64(bz), nil
}

func (k *Keeper) getHoldingEntry(ctx sdk.Context, denom string, holder sdk.AccAddress, index uint64) (holdingEntry, error) {
	bz := k.store(ctx).Get(types.HoldingEntryKey(denom, holder, index))
	if len(bz) < 9 {
		return holdingEntry{}, fmt.Errorf("%w: missing or malformed checkpoint %d", types.ErrHoldingHistory, index)
	}
	var balance sdk.Int
	if err := balance.Unmarshal(bz[8:]); err != nil || balance.IsNil() || balance.IsNegative() {
		return holdingEntry{}, fmt.Errorf("%w: malformed checkpoint %d balance", types.ErrHoldingHistory, index)
	}
	return holdingEntry{Epoch: binary.BigEndian.Uint64(bz[:8]), Balance: balance}, nil
}

// OpenHoldingHistory starts recording denom's holder balances at the airdrop
// epoch boundary the market has just reached. The first call opens the
// history at the market's current epoch; each later call must advance it by
// exactly one epoch.
func (k *Keeper) OpenHoldingHistory(ctx sdk.Context, denom string, epoch uint64) error {
	market, found := k.GetMarket(ctx, denom)
	if !found {
		return fmt.Errorf("%w: %s", types.ErrUnknownMarket, denom)
	}
	if epoch == 0 || epoch != market.AirdropEpoch {
		return fmt.Errorf("%w: epoch %d is not market epoch %d", types.ErrHoldingHistory, epoch, market.AirdropEpoch)
	}
	history, found, err := k.getHoldingHistory(ctx, denom)
	if err != nil {
		return err
	}
	if !found {
		k.setHoldingHistory(ctx, denom, holdingHistory{FirstEpoch: epoch, CurrentEpoch: epoch})
		return nil
	}
	if history.CurrentEpoch == math.MaxUint64 || epoch != history.CurrentEpoch+1 {
		return fmt.Errorf("%w: epoch %d does not follow recorded epoch %d", types.ErrHoldingHistory, epoch, history.CurrentEpoch)
	}
	history.CurrentEpoch = epoch
	k.setHoldingHistory(ctx, denom, history)
	return nil
}

// BeforeBalanceChange is the bank balance-change hook. For a launched denom
// with an open history it records the holder's balance before its first
// write in the current epoch, on the caller's metered context.
func (k *Keeper) BeforeBalanceChange(ctx sdk.Context, addr sdk.AccAddress, denom string, before func() sdk.Int) error {
	if !k.isLaunchedDenom(denom) {
		return nil
	}
	history, found, err := k.getHoldingHistory(ctx, denom)
	if err != nil || !found {
		return err
	}
	count, err := k.HoldingCheckpointCount(ctx, denom, addr)
	if err != nil {
		return err
	}
	if count > 0 {
		last, err := k.getHoldingEntry(ctx, denom, addr, count-1)
		if err != nil {
			return err
		}
		if last.Epoch > history.CurrentEpoch {
			return fmt.Errorf("%w: checkpoint epoch %d after recorded epoch %d", types.ErrHoldingHistory, last.Epoch, history.CurrentEpoch)
		}
		if last.Epoch == history.CurrentEpoch {
			return nil
		}
	}
	if count == math.MaxUint64 {
		return fmt.Errorf("%w: checkpoint count overflow", types.ErrHoldingHistory)
	}
	balance := before()
	if balance.IsNil() || balance.IsNegative() {
		return fmt.Errorf("%w: invalid balance before change", types.ErrHoldingHistory)
	}
	amount, err := balance.Marshal()
	if err != nil {
		return err
	}
	store := k.store(ctx)
	store.Set(types.HoldingEntryKey(denom, addr, count), append(binary.BigEndian.AppendUint64(nil, history.CurrentEpoch), amount...))
	store.Set(types.HoldingCountKey(denom, addr), binary.BigEndian.AppendUint64(nil, count+1))
	return nil
}

// EpochBalance is holder's denom balance at the boundary of airdrop epoch
// epoch: the balance before the first write at or after that boundary, or
// the live balance when no write happened since. Only epochs inside the
// recorded interval are answered.
func (k *Keeper) EpochBalance(ctx sdk.Context, denom string, holder sdk.AccAddress, epoch uint64) (sdk.Int, error) {
	history, found, err := k.getHoldingHistory(ctx, denom)
	if err != nil {
		return sdk.Int{}, err
	}
	if !found {
		return sdk.Int{}, fmt.Errorf("%w: no holding history for %s", types.ErrUnsupportedHoldingEpoch, denom)
	}
	if epoch < history.FirstEpoch || epoch > history.CurrentEpoch {
		return sdk.Int{}, fmt.Errorf("%w: epoch %d outside [%d, %d]", types.ErrUnsupportedHoldingEpoch, epoch,
			history.FirstEpoch, history.CurrentEpoch)
	}
	count, err := k.HoldingCheckpointCount(ctx, denom, holder)
	if err != nil {
		return sdk.Int{}, err
	}
	// Binary search for the first checkpoint at or after epoch; every probe
	// must lie strictly between the epochs of the probes bracketing it.
	lo, hi := uint64(0), count
	var below, above *holdingEntry
	for lo < hi {
		mid := lo + (hi-lo)/2
		entry, err := k.getHoldingEntry(ctx, denom, holder, mid)
		if err != nil {
			return sdk.Int{}, err
		}
		if entry.Epoch > history.CurrentEpoch || (below != nil && entry.Epoch <= below.Epoch) ||
			(above != nil && entry.Epoch >= above.Epoch) {
			return sdk.Int{}, fmt.Errorf("%w: checkpoint %d out of order", types.ErrHoldingHistory, mid)
		}
		if entry.Epoch >= epoch {
			above, hi = &entry, mid
		} else {
			below, lo = &entry, mid+1
		}
	}
	if above != nil {
		return above.Balance, nil
	}
	return k.bankKeeper.GetBalance(ctx, holder, denom).Amount, nil
}
