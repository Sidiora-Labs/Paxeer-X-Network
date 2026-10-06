package types

import codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"

func (m *QueryAccountResponse) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	var account AccountI
	return unpacker.UnpackAny(m.Account, &account)
}

var _ codectypes.UnpackInterfacesMessage = &QueryAccountResponse{}
