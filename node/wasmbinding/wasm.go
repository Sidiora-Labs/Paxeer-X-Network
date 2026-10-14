package wasmbinding

import (
	epochwasm "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/client/wasm"
	epochkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/keeper"
	evmwasm "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/client/wasm"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	oraclewasm "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/client/wasm"
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	tokenfactorywasm "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/client/wasm"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm"
	wasmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/keeper"
	wasmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/types"
)

func RegisterCustomPlugins(
	oracle *oraclekeeper.Keeper,
	epoch *epochkeeper.Keeper,
	tokenfactory *tokenfactorykeeper.Keeper,
	_ *authkeeper.AccountKeeper,
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
	evmKeeper *evmkeeper.Keeper,
	stakingKeeper stakingkeeper.Keeper,
) []wasmkeeper.Option {
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	epochHandler := epochwasm.NewEpochWasmQueryHandler(epoch)
	tokenfactoryHandler := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(tokenfactory)
	evmHandler := evmwasm.NewEVMQueryHandler(evmKeeper)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler, epochHandler, tokenfactoryHandler, evmHandler, stakingKeeper)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})
	messengerHandlerOpt := wasmkeeper.WithMessageHandler(
		CustomMessageHandler(router, channelKeeper, capabilityKeeper, bankKeeper, evmKeeper, unpacker, portSource),
	)

	return []wasm.Option{
		queryPluginOpt,
		messengerHandlerOpt,
	}
}
