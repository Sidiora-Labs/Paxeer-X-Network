package types

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/msgservice"
)

// RegisterCodec registers the module messages on the legacy amino codec.
func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitPrices{}, "pricefeed/MsgSubmitPrices", nil)
	cdc.RegisterConcrete(&MsgSetParams{}, "pricefeed/MsgSetParams", nil)
}

// RegisterInterfaces registers both messages as sdk.Msg under their type URLs
// and the Msg service that executes them.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitPrices{},
		&MsgSetParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
