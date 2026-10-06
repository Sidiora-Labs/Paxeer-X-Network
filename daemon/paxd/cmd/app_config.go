package cmd

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/admin"
	gigaconfig "github.com/Sidiora-Labs/Paxeer-X-Network/engine/executor/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/blocktest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/querier"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/replay"
	paxapp "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	evmrpcconfig "github.com/Sidiora-Labs/Paxeer-X-Network/rpc/config"
	srvconfig "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/config"
	paxdbconfig "github.com/Sidiora-Labs/Paxeer-X-Network/storage/config"
)

// WASMConfig defines configuration for the wasm module.
type WASMConfig struct {
	QueryGasLimit uint64 `mapstructure:"query_gas_limit"`
	LruSize       uint64 `mapstructure:"lru_size"`
}

// CustomAppConfig extends the Cosmos SDK's Config with custom fields
// This structure is used for generating app.toml with custom sections
type CustomAppConfig struct {
	srvconfig.Config

	StateCommit     paxdbconfig.StateCommitConfig  `mapstructure:"state-commit"`
	StateStore      paxdbconfig.StateStoreConfig   `mapstructure:"state-store"`
	ReceiptStore    paxdbconfig.ReceiptStoreConfig `mapstructure:"receipt-store"`
	WASM            WASMConfig                     `mapstructure:"wasm"`
	EVM             evmrpcconfig.Config            `mapstructure:"evm"`
	GigaExecutor    gigaconfig.Config              `mapstructure:"giga_executor"`
	ETHReplay       replay.Config                  `mapstructure:"eth_replay"`
	ETHBlockTest    blocktest.Config               `mapstructure:"eth_block_test"`
	EvmQuery        querier.Config                 `mapstructure:"evm_query"`
	LightInvariance paxapp.LightInvarianceConfig   `mapstructure:"light_invariance"`
	Admin           admin.Config                   `mapstructure:"admin_server"`
}

// NewCustomAppConfig creates a CustomAppConfig with the given base config and EVM config
func NewCustomAppConfig(baseConfig *srvconfig.Config, evmConfig evmrpcconfig.Config) CustomAppConfig {
	return CustomAppConfig{
		Config:       *baseConfig,
		StateCommit:  paxdbconfig.DefaultStateCommitConfig(),
		StateStore:   paxdbconfig.DefaultStateStoreConfig(),
		ReceiptStore: paxdbconfig.DefaultReceiptStoreConfig(),
		WASM: WASMConfig{
			QueryGasLimit: 300000,
			LruSize:       1,
		},
		EVM:             evmConfig,
		GigaExecutor:    gigaconfig.DefaultConfig,
		ETHReplay:       replay.DefaultConfig,
		ETHBlockTest:    blocktest.DefaultConfig,
		EvmQuery:        querier.DefaultConfig,
		LightInvariance: paxapp.DefaultLightInvarianceConfig,
		Admin:           admin.DefaultConfig,
	}
}
