package wasmbinding

import (
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	wasmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/keeper"
	wasmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/types"
)

type CustomRouter struct {
	wasmkeeper.MessageRouter

	evmKeeper *evmkeeper.Keeper
}

func (r *CustomRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	switch m := msg.(type) {
	case *evmtypes.MsgInternalEVMCall:
		return func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			return r.evmKeeper.HandleInternalEVMCall(ctx, m)
		}
	case *evmtypes.MsgInternalEVMDelegateCall:
		return func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			return r.evmKeeper.HandleInternalEVMDelegateCall(ctx, m)
		}
	default:
		return r.MessageRouter.Handler(msg)
	}
}

// forked from wasm
func CustomMessageHandler(
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	evmKeeper *evmkeeper.Keeper,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
) wasmkeeper.Messenger {
	encoders := wasmkeeper.DefaultEncoders(unpacker, portSource)
	encoders = encoders.Merge(
		&wasmkeeper.MessageEncoders{
			Custom: CustomEncoder,
		})
	return wasmkeeper.NewMessageHandlerChain(
		wasmkeeper.NewSDKMessageHandler(&CustomRouter{MessageRouter: router, evmKeeper: evmKeeper}, encoders),
		wasmkeeper.NewIBCRawPacketHandler(channelKeeper, capabilityKeeper),
		wasmkeeper.NewBurnCoinMessageHandler(bankKeeper),
	)
}
