package app

import (
	gigaprecompiles "github.com/Sidiora-Labs/Paxeer-X-Network/engine/executor/precompiles"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// gigaV6120PricefeedAddress is the pricefeed precompile the v6.12.0 plan
// gates beside the spot precompiles.
const gigaV6120PricefeedAddress = "0x0000000000000000000000000000000000001020"

// gigaV6120FailFast is AllCustomPrecompilesFailFastLate plus every precompile
// the v6.12.0 plan gates (0x1020-0x1025), each mapped to the fail-fast
// singleton so a giga call to any of them re-executes on the ordinary path.
var gigaV6120FailFast = map[common.Address]vm.PrecompiledContract{}

func init() {
	for addr, contract := range gigaprecompiles.AllCustomPrecompilesFailFastLate {
		gigaV6120FailFast[addr] = contract
	}
	for _, addr := range []string{
		gigaV6120PricefeedAddress,
		spottypes.PECORAddress,
		spottypes.PECOROrdersAddress,
		spottypes.PECORStopOrdersAddress,
		spottypes.PECORQuoterAddress,
		spottypes.PECORVaultAddress,
	} {
		gigaV6120FailFast[common.HexToAddress(addr)] = gigaprecompiles.FailFastSingleton
	}
}
