package legacytx

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(StdTx{}, "cosmos-sdk/StdTx", nil)
}
