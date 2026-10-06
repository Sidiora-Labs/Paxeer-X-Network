package types

import (
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	cryptotypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"

	clienttypes "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/02-client/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/exported"
)

// Interface implementation checks.
var _, _, _, _ codectypes.UnpackInterfacesMessage = &ClientState{}, &ConsensusState{}, &Header{}, &HeaderData{}

// Data is an interface used for all the signature data bytes proto definitions.
type Data interface{}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (cs ClientState) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	if cs.ConsensusState == nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidConsensus, "consensus state cannot be nil")
	}

	return cs.ConsensusState.UnpackInterfaces(unpacker)
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (cs ConsensusState) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(cs.PublicKey, new(cryptotypes.PubKey))
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (h Header) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(h.NewPublicKey, new(cryptotypes.PubKey))
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (hd HeaderData) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(hd.NewPubKey, new(cryptotypes.PubKey))
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (csd ClientStateData) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(csd.ClientState, new(exported.ClientState))
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (csd ConsensusStateData) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(csd.ConsensusState, new(exported.ConsensusState))
}
