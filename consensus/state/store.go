package state

import (
	tmstate "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

func ABCIResponsesResultsHash(ar *tmstate.ABCIResponses) []byte {
	return types.NewResults(ar.DeliverTxs).Hash()
}
