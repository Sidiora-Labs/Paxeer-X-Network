// Package pricefeed holds the functions the live CheckTx and DeliverTx paths
// call for a price submission: the fee exemption, the read-only refusals made
// before signature verification and the check-state rate mark.
package pricefeed

import (
	"sync"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// markLocks serialises the check and write of MarkSubmission per validator,
// as CheckAndSetSpamPreventionCounter serialises the oracle vote counter.
var markLocks sync.Map

// submissionOf returns the MsgSubmitPrices of a submission tx, nil for any
// other tx.
func submissionOf(tx sdk.Tx) *types.MsgSubmitPrices {
	if !types.IsSubmissionTx(tx) {
		return nil
	}
	msg, _ := tx.GetMsgs()[0].(*types.MsgSubmitPrices)
	return msg
}

// feederValidator returns the validator a submission's feeder is registered
// to, refusing an unregistered feeder and a validator outside the bonded set.
func feederValidator(ctx sdk.Context, msg *types.MsgSubmitPrices, k keeper.Keeper) (sdk.ValAddress, error) {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return nil, types.ErrUnregisteredFeeder.Wrapf("feeder %q: %v", msg.Feeder, err)
	}
	operator, ok := k.GetValidatorByFeedAccount(ctx, feeder)
	if !ok {
		return nil, types.ErrUnregisteredFeeder.Wrapf("feeder %s", feeder)
	}
	if err := k.RequireBonded(ctx, operator); err != nil {
		return nil, err
	}
	return operator, nil
}

// checkRate refuses a validator whose last submission is at the target height.
func checkRate(ctx sdk.Context, operator sdk.ValAddress, k keeper.Keeper) error {
	target := types.TargetHeight(ctx)
	if last, ok := k.GetLastSubmissionHeight(ctx, operator); ok && last == target {
		return types.ErrRateLimited.Wrapf("validator %s at height %d", operator, target)
	}
	return nil
}

// SubmissionIsGasless reports whether msg pays no fee: its feeder is the
// registered feed account of a bonded validator that has not submitted at the
// target height. An unregistered feeder or an unbonded validator pays the fee;
// a validator that has submitted is refused, as oracleVoteIsGasless refuses a
// second vote. It reads only.
func SubmissionIsGasless(ctx sdk.Context, msg *types.MsgSubmitPrices, k keeper.Keeper) (bool, error) {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return false, err
	}
	operator, ok := k.GetValidatorByFeedAccount(ctx, feeder)
	if !ok {
		return false, nil
	}
	if err := k.RequireBonded(ctx, operator); err != nil {
		return false, nil
	}
	if err := checkRate(ctx, operator, k); err != nil {
		return false, err
	}
	return true, nil
}

// CheckSubmission refuses, before signature verification and in this order, a
// submission over max_submission_bytes, from an unregistered feeder, of a
// validator outside the bonded set, with more than
// max_observations_per_submission observations, or of a validator that has
// submitted at the target height. It returns nil for a tx that is not a
// submission and reads only.
func CheckSubmission(ctx sdk.Context, tx sdk.Tx, k keeper.Keeper) error {
	msg := submissionOf(tx)
	if msg == nil {
		return nil
	}
	params := k.GetParams(ctx)
	if size := len(ctx.TxBytes()); uint64(size) > uint64(params.MaxSubmissionBytes) {
		return types.ErrSubmissionTooLarge.Wrapf("%d bytes, at most %d", size, params.MaxSubmissionBytes)
	}
	operator, err := feederValidator(ctx, msg, k)
	if err != nil {
		return err
	}
	if count := len(msg.Observations); uint64(count) > uint64(params.MaxObservationsPerSubmission) {
		return types.ErrTooManyObservations.Wrapf("%d observations, at most %d", count, params.MaxObservationsPerSubmission)
	}
	return checkRate(ctx, operator, k)
}

// MarkSubmission repeats the rate check of a submission after signature
// verification and writes the target height into the validator's last
// submission height in the check state, which the next commit drops. It does
// nothing on ReCheckTx or for a tx that is not a submission.
func MarkSubmission(ctx sdk.Context, tx sdk.Tx, k keeper.Keeper) error {
	msg := submissionOf(tx)
	if ctx.IsReCheckTx() || msg == nil {
		return nil
	}
	operator, err := feederValidator(ctx, msg, k)
	if err != nil {
		return err
	}
	lock, _ := markLocks.LoadOrStore(operator.String(), &sync.Mutex{})
	mtx := lock.(*sync.Mutex)
	mtx.Lock()
	defer mtx.Unlock()
	if err := checkRate(ctx, operator, k); err != nil {
		return err
	}
	k.SetLastSubmissionHeight(ctx, operator, types.TargetHeight(ctx))
	return nil
}
