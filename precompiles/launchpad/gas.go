package launchpad

import "github.com/ethereum/go-ethereum/accounts/abi"

const (
	BaseGas     uint64 = 3000
	GasPerByte  uint64 = 16
	GasPerWrite uint64 = 5000
)

func Writes(method string) uint64 {
	switch method {
	case "createMarket", "createMarketFor", "createMarketWithPermit",
		"initialize", "initialize0", "initialize1", "initialize2", "initialize3", "initialize4":
		return 12
	case "swapTokenForToken", "swapTokenForTokenWithPermit":
		return 14
	case "buy", "buyWithPermit", "swap", "swapFor", "mint":
		return 8
	case "sell", "sellWithPermit", "register":
		return 6
	case "transferFrom", "safeTransferFrom", "safeTransferFrom0":
		return 9
	case "executeAirdrop", "triggerAirdrop", "claimAirdrop", "claimAirdrop0",
		"claimAirdropEpoch", "claimAirdropEpoch0", "allocateLegacyAirdrop", "claimLegacyAirdrop",
		"recordFee":
		return 4
	case "claimFees", "claim", "executeBurn", "burn", "syncReserves", "setFeeWeights":
		return 3
	case "acceptGuardian", "permit", "transfer", "setVirtualDefaults", "recordOpticalSurplus",
		"claimOpticalSurplus", "deposit", "recordDeposit", "withdraw":
		return 2
	case "setFeeStrategy", "executeLpRewards", "sendLpRewards", "pause", "unpause",
		"transferGuardian", "migrateFactory", "approve", "bindPool", "setApprovalForAll",
		"authorizeOptical", "authorizePool", "grantRole", "renounceRole", "revokeRole",
		"upgradeToAndCall", "migrateAccounting", "migrateAdditionalAccounting", "migratePoolIndex",
		"setBaseFeeBps", "setCreationFee", "setProtocolFeeBps", "recoverSurplus", "multicall":
		return 1
	default:
		return 0
	}
}

func Gas(inputBytes, writes uint64) uint64 {
	return BaseGas + GasPerByte*inputBytes + GasPerWrite*writes
}

func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return Gas(uint64(len(input))+uint64(len(method.ID)), Writes(method.RawName))
}
