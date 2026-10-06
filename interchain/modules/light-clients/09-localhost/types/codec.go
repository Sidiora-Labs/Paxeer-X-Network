package types

import (
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/exported"
)

// RegisterInterfaces register the ibc interfaces submodule implementations to protobuf
// Any.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*exported.ClientState)(nil),
		&ClientState{},
	)
}
