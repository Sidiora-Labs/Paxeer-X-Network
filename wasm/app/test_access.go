package app

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"

	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/app/params"

	ibctransferkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/keeper"
	ibckeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	capabilitykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/capability/keeper"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm"
)

type TestSupport struct {
	t   testing.TB
	app *WasmApp
}

func NewTestSupport(t testing.TB, app *WasmApp) *TestSupport {
	return &TestSupport{t: t, app: app}
}

func (s TestSupport) IBCKeeper() *ibckeeper.Keeper {
	return s.app.ibcKeeper
}

func (s TestSupport) WasmKeeper() wasm.Keeper {
	return s.app.wasmKeeper
}

func (s TestSupport) AppCodec() codec.Codec {
	return s.app.appCodec
}

func (s TestSupport) ScopedWasmIBCKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedWasmKeeper
}

func (s TestSupport) ScopeIBCKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedIBCKeeper
}

func (s TestSupport) ScopedTransferKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedTransferKeeper
}

func (s TestSupport) StakingKeeper() stakingkeeper.Keeper {
	return s.app.stakingKeeper
}

func (s TestSupport) BankKeeper() bankkeeper.Keeper {
	return s.app.bankKeeper
}

func (s TestSupport) TransferKeeper() ibctransferkeeper.Keeper {
	return s.app.transferKeeper
}

func (s TestSupport) GetBaseApp() *baseapp.BaseApp {
	return s.app.BaseApp
}

func (s TestSupport) GetTxConfig() client.TxConfig {
	return params.MakeEncodingConfig().TxConfig
}
