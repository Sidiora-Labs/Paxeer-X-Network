package keeper

import (
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// ShareDenomPrefix opens every vault share denom; the spot module account
// mints and burns no other denom.
const ShareDenomPrefix = "spot/share/"

// ShareTransferTopic is topic0 of the ERC20 Transfer(address,address,uint256)
// log the spot keeper writes at a share pointer on each share mint and burn.
var ShareTransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// ShareDenom is the bank denom of the vault shares of token,
// spot/share/<lowercase hex address>.
func (k *Keeper) ShareDenom(token common.Address) string {
	return ShareDenomPrefix + strings.ToLower(token.Hex())
}

// isShareDenom reports whether denom is the share denom of some token.
func (k *Keeper) isShareDenom(denom string) bool {
	hex, ok := strings.CutPrefix(denom, ShareDenomPrefix)
	return ok && common.IsHexAddress(hex) && k.ShareDenom(common.HexToAddress(hex)) == denom
}

// CreateShareToken writes the bank metadata of the share denom of token,
// named "Spot Vault Share <symbol>" with symbol "s<symbol>" and the token
// decimals, and creates its ERC20 native pointer, NativePaxTokensERC20, whose
// balanceOf and totalSupply read the bank module. It returns the pointer
// address. evm is the instance the pointer is deployed in, the one-off
// instance of the activation (RunWithOneOffEVMInstance).
func (k *Keeper) CreateShareToken(ctx sdk.Context, evm *vm.EVM, token common.Address, symbol string,
	decimals uint8) (common.Address, error) {
	if token == (common.Address{}) {
		return common.Address{}, types.ErrZeroAddress
	}
	if evm == nil {
		return common.Address{}, sdkerrors.ErrInvalidRequest.Wrap("spot: share pointer needs an EVM instance")
	}
	if strings.TrimSpace(symbol) == "" || decimals > types.MaxTokenDecimals {
		return common.Address{}, types.ErrUnsupportedToken
	}
	denom := k.ShareDenom(token)
	if _, _, exists := k.evmKeeper.GetERC20NativePointer(ctx, denom); exists {
		return common.Address{}, types.ErrTokenAlreadyRegistered
	}
	name := "Spot Vault Share " + symbol
	shareSymbol := "s" + symbol
	metadata := banktypes.Metadata{
		Description: name,
		DenomUnits:  []*banktypes.DenomUnit{{Denom: denom, Exponent: 0}},
		Base:        denom,
		Display:     denom,
		Name:        name,
		Symbol:      shareSymbol,
	}
	if decimals > 0 {
		metadata.DenomUnits = append(metadata.DenomUnits,
			&banktypes.DenomUnit{Denom: shareSymbol, Exponent: uint32(decimals)})
		metadata.Display = shareSymbol
	}
	if err := metadata.Validate(); err != nil {
		return common.Address{}, types.ErrUnsupportedToken
	}
	k.bankKeeper.SetDenomMetaData(ctx, metadata)
	return k.evmKeeper.UpsertERCNativePointer(ctx, evm, denom,
		utils.ERCMetadata{Name: name, Symbol: shareSymbol, Decimals: decimals})
}

// sharePointer checks a share mint or burn of amount of denom in evm and
// returns the share pointer and the coins it moves.
func (k *Keeper) sharePointer(ctx sdk.Context, evm *vm.EVM, denom string, amount sdk.Int) (common.Address, sdk.Coins, error) {
	if !k.isShareDenom(denom) {
		return common.Address{}, nil, types.ErrUnsupportedToken
	}
	if evm == nil {
		return common.Address{}, nil, sdkerrors.ErrInvalidRequest.Wrap("spot: shares are minted and burned only inside a transaction")
	}
	if amount.IsNil() || !amount.IsPositive() {
		return common.Address{}, nil, types.ErrZeroAmount
	}
	if amount.BigInt().BitLen() > 256 {
		return common.Address{}, nil, sdkerrors.ErrInvalidRequest.Wrap("spot: share amount exceeds uint256")
	}
	pointer, _, exists := k.evmKeeper.GetERC20NativePointer(ctx, denom)
	if !exists {
		return common.Address{}, nil, types.ErrTokenNotRegistered
	}
	return pointer, sdk.NewCoins(sdk.NewCoin(denom, amount)), nil
}

// emitShareTransfer writes Transfer(from, to, amount) at the share pointer,
// the log the pointer's _update would write, which a bank mint or burn never
// runs.
func emitShareTransfer(evm *vm.EVM, pointer, from, to common.Address, amount sdk.Int) error {
	return pcommon.EmitEVMLog(evm, pointer,
		[]common.Hash{ShareTransferTopic, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		common.LeftPadBytes(amount.BigInt().Bytes(), 32))
}

// MintShares mints amount of the share denom to to through the bank keeper
// with the spot module account and writes Transfer(address(0), to, amount)
// at the share pointer. It refuses any denom outside spot/share/* and runs
// only inside a transaction, evm being the instance of that transaction.
func (k *Keeper) MintShares(ctx sdk.Context, evm *vm.EVM, denom string, to common.Address, amount sdk.Int) error {
	pointer, coins, err := k.sharePointer(ctx, evm, denom, amount)
	if err != nil {
		return err
	}
	if to == (common.Address{}) {
		return types.ERC20InvalidReceiver(to)
	}
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}
	if recipient := k.evmKeeper.GetPaxAddressOrDefault(ctx, to); !recipient.Equals(k.CustodyAddress()) {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, coins); err != nil {
			return err
		}
	}
	return emitShareTransfer(evm, pointer, common.Address{}, to, amount)
}

// BurnShares burns amount of the share denom held by from through the bank
// keeper with the spot module account and writes Transfer(from, address(0),
// amount) at the share pointer. It refuses any denom outside spot/share/*,
// returns ERC20InsufficientBalance when from holds less than amount, and runs
// only inside a transaction, evm being the instance of that transaction.
func (k *Keeper) BurnShares(ctx sdk.Context, evm *vm.EVM, denom string, from common.Address, amount sdk.Int) error {
	pointer, coins, err := k.sharePointer(ctx, evm, denom, amount)
	if err != nil {
		return err
	}
	if from == (common.Address{}) {
		return types.ERC20InvalidSender(from)
	}
	holder := k.evmKeeper.GetPaxAddressOrDefault(ctx, from)
	if balance := k.bankKeeper.GetBalance(ctx, holder, denom).Amount; balance.LT(amount) {
		held, _ := uint256.FromBig(balance.BigInt())
		needed, _ := uint256.FromBig(amount.BigInt())
		return types.ERC20InsufficientBalance(from, held, needed)
	}
	if !holder.Equals(k.CustodyAddress()) {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, holder, types.ModuleName, coins); err != nil {
			return err
		}
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}
	return emitShareTransfer(evm, pointer, from, common.Address{}, amount)
}
