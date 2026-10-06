package utils

import (
	"fmt"

	ibctransfertypes "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/types"
	ibchost "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/24-host"
	epochmoduletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	oracletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	tokenfactorytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	authzkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/authz/keeper"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	capabilitytypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/capability/types"
	distrtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/distribution/types"
	evidencetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/evidence/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/feegrant"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	paramstypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	slashingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/slashing/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm"
)

var ModuleKeys = sdk.NewKVStoreKeys(
	authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
	minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
	govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
	evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
	evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
)

var Modules = []string{
	"authz",
	"acc",
	"bank",
	"capability",
	"distribution",
	"epoch",
	"evidence",
	"evm",
	"feegrant",
	"gov",
	"ibc",
	"mint",
	"oracle",
	"params",
	"slashing",
	"staking",
	"tokenfactory",
	"transfer",
	"upgrade",
	"wasm"}

func BuildRawPrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/n", moduleName)
}

func BuildTreePrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/", moduleName)
}
