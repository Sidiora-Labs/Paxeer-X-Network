package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// TargetHeight is the height a submission is for: the next block in CheckTx,
// whose context carries the last committed height, and the block height in
// DeliverTx.
func TargetHeight(ctx sdk.Context) int64 {
	if ctx.IsCheckTx() {
		return ctx.BlockHeight() + 1
	}
	return ctx.BlockHeight()
}

// IsSubmissionTx reports whether tx carries exactly one message and it is a
// MsgSubmitPrices.
func IsSubmissionTx(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	_, ok := msgs[0].(*MsgSubmitPrices)
	return ok
}

// CheckSubmissionAlone reports whether msgs carry a MsgSubmitPrices and
// refuses one that stands beside any other message, a second MsgSubmitPrices
// included, the rule the oracle vote follows in CosmosStatelessChecks.
func CheckSubmissionAlone(msgs []sdk.Msg) (bool, error) {
	submissions := 0
	for _, msg := range msgs {
		if _, ok := msg.(*MsgSubmitPrices); ok {
			submissions++
		}
	}
	if submissions == 0 {
		return false, nil
	}
	if len(msgs) > 1 {
		return true, ErrSubmissionAlone
	}
	return true, nil
}
