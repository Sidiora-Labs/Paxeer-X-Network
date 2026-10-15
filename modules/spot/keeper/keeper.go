package keeper

import (
	"encoding/json"
	"fmt"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// BankKeeper moves and reads the native balances of the spot module account.
type BankKeeper interface {
	SendCoins(ctx sdk.Context, from, to sdk.AccAddress, amount sdk.Coins) error
	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	SetDenomMetaData(ctx sdk.Context, denomMetaData banktypes.Metadata)
	MintCoins(ctx sdk.Context, moduleName string, amounts sdk.Coins) error
	BurnCoins(ctx sdk.Context, moduleName string, amounts sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// EVMKeeper resolves addresses and pointers and runs EVM calls outside a
// transaction.
type EVMKeeper interface {
	GetEVMAddressOrDefault(ctx sdk.Context, paxAddress sdk.AccAddress) common.Address
	GetPaxAddressOrDefault(ctx sdk.Context, evmAddress common.Address) sdk.AccAddress
	GetNativePointee(ctx sdk.Context, erc20Address string) (token string, version uint16, exists bool)
	GetERC20NativePointer(ctx sdk.Context, token string) (addr common.Address, version uint16, exists bool)
	GetERC20CW20Pointer(ctx sdk.Context, cw20Address string) (addr common.Address, version uint16, exists bool)
	GetBaseDenom(ctx sdk.Context) string
	GetCode(ctx sdk.Context, addr common.Address) []byte
	CallEVM(ctx sdk.Context, from common.Address, to *common.Address, val *sdk.Int, data []byte) ([]byte, error)
	StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error)
	RunWithOneOffEVMInstance(ctx sdk.Context, runner func(*vm.EVM) error, logger func(string, string)) error
	UpsertERCNativePointer(ctx sdk.Context, evm *vm.EVM, token string, metadata utils.ERCMetadata) (common.Address, error)
	GetTransientReceipt(ctx sdk.Context, txHash common.Hash, txIndex uint64) (*evmtypes.Receipt, error)
	SetTransientReceipt(ctx sdk.Context, txHash common.Hash, receipt *evmtypes.Receipt) error
}

// Keeper owns the PECOR engine, order engine and vault: every registered
// token's custody, escrow and fee accruals sit in the spot module account.
type Keeper struct {
	storeKey sdk.StoreKey

	bankKeeper      BankKeeper
	evmKeeper       EVMKeeper
	pricefeedKeeper pricefeedkeeper.Keeper

	authority string
}

// NewKeeper wires the spot keeper. Params change only through UpdateParams
// signed by the governance module account.
func NewKeeper(storeKey sdk.StoreKey, bankKeeper BankKeeper, evmKeeper EVMKeeper,
	pricefeedKeeper pricefeedkeeper.Keeper) *Keeper {
	return &Keeper{storeKey: storeKey, bankKeeper: bankKeeper, evmKeeper: evmKeeper, pricefeedKeeper: pricefeedKeeper,
		authority: authtypes.NewModuleAddress(govtypes.ModuleName).String()}
}

func (k *Keeper) store(ctx sdk.Context) sdk.KVStore { return ctx.KVStore(k.storeKey) }

// Authority is the bech32 account allowed to change params.
func (k *Keeper) Authority() string { return k.authority }

func (k *Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	bz := k.store(ctx).Get(types.ParamsKey)
	if bz == nil {
		return types.DefaultParams(), nil
	}
	var params types.Params
	if err := json.Unmarshal(bz, &params); err != nil {
		return types.Params{}, fmt.Errorf("spot: corrupt params: %w", err)
	}
	return params, nil
}

func (k *Keeper) setParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	bz, err := json.Marshal(params)
	if err != nil {
		return err
	}
	k.store(ctx).Set(types.ParamsKey, bz)
	return nil
}

// UpdateParams replaces the params when signed by the governance module
// account.
func (k *Keeper) UpdateParams(ctx sdk.Context, authority string, params types.Params) error {
	if authority != k.authority {
		return fmt.Errorf("%w: %s", sdkerrors.ErrUnauthorized, authority)
	}
	return k.setParams(ctx, params)
}
