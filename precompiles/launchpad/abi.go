package launchpad

import (
	"embed"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

// roles lists the eleven role files under abi/. Every role except
// SidioraERC20 is served by the precompile; SidioraERC20 encodes the nested
// calls the precompile makes to the pool-token contracts.
var roles = []string{
	"SidioraPool", "SidioraFactory", "SidioraERC20", "SidioraNFT", "Router", "Quoter",
	"FeesRouter", "FeeAccumulator", "PoolRegistry", "ProtocolConfig", "Treasury",
}

//go:embed abi/*.json
var roleFS embed.FS

// roleABIs holds the parsed ABI of each role file keyed by role.
var roleABIs = loadRoleABIs()

func loadRoleABIs() map[string]abi.ABI {
	abis := make(map[string]abi.ABI, len(roles))
	for _, role := range roles {
		abis[role] = pcommon.MustGetABI(roleFS, "abi/"+role+".json")
	}
	return abis
}
