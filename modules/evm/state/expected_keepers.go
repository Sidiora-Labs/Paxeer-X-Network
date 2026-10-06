package state

import (
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/common"
)

type EVMKeeper interface {
	ConvertFeeToDenom(sdk.Int, sdk.Dec, bool) (sdk.Int, error)
	ConvertFeeFromDenom(sdk.Int, sdk.Dec, bool) (sdk.Int, error)
	PrefixStore(sdk.Context, []byte) sdk.KVStore
	PurgePrefix(sdk.Context, []byte)
	GetPaxAddress(sdk.Context, common.Address) (sdk.AccAddress, bool)
	GetPaxAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	BankKeeper() bankkeeper.Keeper
	GetBaseDenom(sdk.Context) string
	DeleteAddressMapping(sdk.Context, sdk.AccAddress, common.Address)
	GetCode(sdk.Context, common.Address) []byte
	SetCode(sdk.Context, common.Address, []byte)
	GetCodeHash(sdk.Context, common.Address) common.Hash
	GetCodeSize(sdk.Context, common.Address) int
	GetState(sdk.Context, common.Address, common.Hash) common.Hash
	SetState(sdk.Context, common.Address, common.Hash, common.Hash)
	AccountKeeper() *authkeeper.AccountKeeper
	GetFeeCollectorAddress(sdk.Context) (common.Address, error)
	GetNonce(sdk.Context, common.Address) uint64
	SetNonce(sdk.Context, common.Address, uint64)
	PrepareReplayedAddr(ctx sdk.Context, addr common.Address)
	GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int
	UpgradeKeeper() *upgradekeeper.Keeper
}
