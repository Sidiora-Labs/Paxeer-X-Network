package utils

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"

	oracletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
)

func IsTxPrioritized(tx sdk.Tx) bool {
	if pricefeedtypes.IsSubmissionTx(tx) {
		return true
	}
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			continue
		case *oracletypes.MsgDelegateFeedConsent:
			continue
		default:
			return false
		}
	}
	return true
}
