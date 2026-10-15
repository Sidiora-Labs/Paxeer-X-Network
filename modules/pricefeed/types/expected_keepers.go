package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
)

// StakingKeeper resolves validator operators and walks the bonded set by
// power, as the oracle vote module builds its claim map.
type StakingKeeper interface {
	Validator(ctx sdk.Context, address sdk.ValAddress) stakingtypes.ValidatorI // nil when the validator is not found
	ValidatorsPowerStoreIterator(ctx sdk.Context) sdk.Iterator
	MaxValidators(ctx sdk.Context) uint32
	GetLastTotalPower(ctx sdk.Context) sdk.Int
}

// AccountKeeper creates the auth account of a feed account before its first
// submission.
type AccountKeeper interface {
	HasAccount(ctx sdk.Context, addr sdk.AccAddress) bool
	NewAccountWithAddress(ctx sdk.Context, addr sdk.AccAddress) authtypes.AccountI
	SetAccount(ctx sdk.Context, acc authtypes.AccountI)
}

// EVMKeeper maps between EVM addresses and the accounts they are associated
// with.
type EVMKeeper interface {
	GetPaxAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	GetEVMAddress(ctx sdk.Context, paxAddress sdk.AccAddress) (common.Address, bool)
}
