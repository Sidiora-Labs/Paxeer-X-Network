package bindings

import "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"

type PaxEpochQuery struct {
	// queries the current Epoch
	Epoch *types.QueryEpochRequest `json:"epoch,omitempty"`
}
