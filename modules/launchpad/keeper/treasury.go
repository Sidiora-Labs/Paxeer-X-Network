package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TreasuryDepositorRole is the Treasury DEPOSITOR_ROLE.
var TreasuryDepositorRole = crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE"))

// TreasuryGetBalance is getBalance(token): the Treasury _balances ledger of
// token, TreasuryLedger.
func (k *Keeper) TreasuryGetBalance(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	return k.getLedgerAmount(ctx, types.TreasuryLedgerKey(token))
}

// TreasuryHeld is the amount of token held in custody at the launchpad module
// EVM address for the Treasury; it never falls below TreasuryLedger.
func (k *Keeper) TreasuryHeld(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	return k.getLedgerAmount(ctx, types.TreasuryHeldKey(token))
}

// treasuryBalance is _actualBalance: token.balanceOf(holder) through a nested
// staticcall, refusing a token without code, or whose balanceOf fails or
// returns other than 32 bytes, with InvalidToken().
func (k *Keeper) treasuryBalance(ctx sdk.Context, caller NestedCaller, token, holder common.Address) (sdk.Int, error) {
	if len(k.evmKeeper.GetCode(ctx, token)) == 0 {
		return sdk.Int{}, fmt.Errorf("%w: %s has no code", types.ErrInvalidToken, token.Hex())
	}
	ret, err := caller.StaticCall(ctx, launchpadAddress, token, erc20Call(erc20BalanceOf, addressWord(holder)))
	if err != nil {
		return sdk.Int{}, fmt.Errorf("%w: %s balanceOf: %w", types.ErrInvalidToken, token.Hex(), err)
	}
	if len(ret) != 32 {
		return sdk.Int{}, fmt.Errorf("%w: %s balanceOf returned %d bytes", types.ErrInvalidToken, token.Hex(), len(ret))
	}
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret)), nil
}

// treasuryCall is TransferHelper's safe call: token must have code, the call
// must succeed and return nothing or true, or it refuses with failure.
func (k *Keeper) treasuryCall(ctx sdk.Context, caller NestedCaller, from, token common.Address, input []byte,
	failure error) (sdk.Context, error) {
	next, ret, err := caller.Call(ctx, from, token, input)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", failure, err)
	}
	if len(k.evmKeeper.GetCode(next, token)) == 0 {
		return ctx, fmt.Errorf("%w: %s has no code", failure, token.Hex())
	}
	if len(ret) != 0 && (len(ret) != 32 || new(big.Int).SetBytes(ret).Cmp(big.NewInt(1)) != 0) {
		return ctx, fmt.Errorf("%w: return data %x", failure, ret)
	}
	return next, nil
}

// treasuryReceive runs a token call that pays the launchpad module EVM
// address and returns the rise of its token balance.
func (k *Keeper) treasuryReceive(ctx sdk.Context, caller NestedCaller, from, token common.Address, input []byte,
	failure error) (sdk.Context, sdk.Int, error) {
	custody := k.ModuleEVMAddress(ctx)
	before, err := k.treasuryBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx, err = k.treasuryCall(ctx, caller, from, token, input, failure)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	after, err := k.treasuryBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if after.LT(before) {
		return ctx, sdk.Int{}, fmt.Errorf("%w: custody balance fell from %s to %s", failure, before, after)
	}
	return ctx, after.Sub(before), nil
}

// treasurySurplus is TreasuryHeld minus TreasuryLedger of token, refusing a
// ledger above the held amount with InsufficientBalance().
func (k *Keeper) treasurySurplus(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	ledger, err := k.TreasuryGetBalance(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	held, err := k.TreasuryHeld(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	if held.LT(ledger) {
		return sdk.Int{}, fmt.Errorf("%w: treasury %s ledger %s above held %s", types.ErrInsufficientBalance,
			token.Hex(), ledger, held)
	}
	return held.Sub(ledger), nil
}

// treasuryCredit credits amount of the surplus of token to TreasuryLedger,
// refusing an amount above the surplus with InsufficientBalance().
func (k *Keeper) treasuryCredit(ctx sdk.Context, token common.Address, amount sdk.Int) error {
	surplus, err := k.treasurySurplus(ctx, token)
	if err != nil {
		return err
	}
	if amount.GT(surplus) {
		return fmt.Errorf("%w: treasury %s surplus %s, needs %s", types.ErrInsufficientBalance, token.Hex(), surplus,
			amount)
	}
	return k.addLedgerAmount(ctx, types.TreasuryLedgerKey(token), amount)
}

// TreasuryDeposit is deposit(token, amount) sent by depositor, a
// DEPOSITOR_ROLE holder at the Treasury: token.transferFrom(depositor,
// launchpad module EVM address, amount) called by 0x1017, spending the
// depositor's allowance to 0x1017, with the rise of the custody balance
// credited to TreasuryLedger and TreasuryHeld. It returns the received
// amount, the amount of Deposited(token, depositor, received).
func (k *Keeper) TreasuryDeposit(ctx sdk.Context, caller NestedCaller, depositor, token common.Address,
	amount sdk.Int) (sdk.Context, sdk.Int, error) {
	if err := k.CheckRole(ctx, k.TreasuryContract(), TreasuryDepositorRole, depositor); err != nil {
		return ctx, sdk.Int{}, err
	}
	if err := positiveAmount(amount); err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx, received, err := k.treasuryReceive(ctx, caller, launchpadAddress, token,
		erc20Call(erc20TransferFrom, addressWord(depositor), addressWord(k.ModuleEVMAddress(ctx)),
			amountWord(amount.BigInt())), types.ErrTransferFromFailed)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if received.IsZero() {
		return ctx, sdk.Int{}, fmt.Errorf("%w: deposit of %s received nothing", types.ErrZeroAmount, token.Hex())
	}
	if err := k.addLedgerAmount(ctx, types.TreasuryHeldKey(token), received); err != nil {
		return ctx, sdk.Int{}, err
	}
	if err := k.addLedgerAmount(ctx, types.TreasuryLedgerKey(token), received); err != nil {
		return ctx, sdk.Int{}, err
	}
	return ctx, received, nil
}

// TreasuryRecordDeposit is recordDeposit(token, amount) sent by depositor, a
// DEPOSITOR_ROLE holder at the Treasury: the token sitting at the Treasury
// address is swept into custody with token.transfer(launchpad module EVM
// address, balanceOf(Treasury)) called by the Treasury and credited to
// TreasuryHeld, then amount, at most TreasuryHeld minus TreasuryLedger, is
// credited to TreasuryLedger.
func (k *Keeper) TreasuryRecordDeposit(ctx sdk.Context, caller NestedCaller, depositor, token common.Address,
	amount sdk.Int) (sdk.Context, error) {
	treasury := k.TreasuryContract()
	if err := k.CheckRole(ctx, treasury, TreasuryDepositorRole, depositor); err != nil {
		return ctx, err
	}
	if err := positiveAmount(amount); err != nil {
		return ctx, err
	}
	balance, err := k.treasuryBalance(ctx, caller, token, treasury)
	if err != nil {
		return ctx, err
	}
	if balance.IsPositive() {
		var received sdk.Int
		ctx, received, err = k.treasuryReceive(ctx, caller, treasury, token,
			erc20Call(erc20Transfer, addressWord(k.ModuleEVMAddress(ctx)), amountWord(balance.BigInt())),
			types.ErrTransferFailed)
		if err != nil {
			return ctx, err
		}
		if err := k.addLedgerAmount(ctx, types.TreasuryHeldKey(token), received); err != nil {
			return ctx, err
		}
	}
	return ctx, k.treasuryCredit(ctx, token, amount)
}

// TreasuryCreditFee credits a creation fee or a protocol cut of the quote
// token to TreasuryLedger. The amount must already be in TreasuryHeld, put
// there by PullQuote or MoveQuote, so the ledger never exceeds the held
// amount.
func (k *Keeper) TreasuryCreditFee(ctx sdk.Context, amount sdk.Int) error {
	if err := positiveAmount(amount); err != nil {
		return err
	}
	return k.treasuryCredit(ctx, k.quoteToken(ctx), amount)
}

// TreasuryWithdraw is withdraw(token, to, amount) under the governance
// authority: amount is debited from TreasuryLedger and TreasuryHeld and paid
// to to with token.transfer(to, amount) called by the launchpad module EVM
// address. The debit is written before the call so the nested call sees it.
func (k *Keeper) TreasuryWithdraw(ctx sdk.Context, caller NestedCaller, authority string, token, to common.Address,
	amount sdk.Int) (sdk.Context, error) {
	if err := k.checkTreasuryPayout(authority, to, amount); err != nil {
		return ctx, err
	}
	ledger, err := k.TreasuryGetBalance(ctx, token)
	if err != nil {
		return ctx, err
	}
	if ledger.LT(amount) {
		return ctx, fmt.Errorf("%w: treasury %s ledger %s, needs %s", types.ErrInsufficientBalance, token.Hex(), ledger,
			amount)
	}
	if err := k.subLedgerAmount(ctx, types.TreasuryHeldKey(token), amount); err != nil {
		return ctx, err
	}
	if err := k.setLedgerAmount(ctx, types.TreasuryLedgerKey(token), ledger.Sub(amount)); err != nil {
		return ctx, err
	}
	return k.treasuryPay(ctx, caller, token, to, amount)
}

// TreasuryRecoverSurplus is recoverSurplus(token, to, amount) under the
// governance authority: amount, at most TreasuryHeld minus TreasuryLedger, is
// debited from TreasuryHeld and paid to to with token.transfer(to, amount)
// called by the launchpad module EVM address.
func (k *Keeper) TreasuryRecoverSurplus(ctx sdk.Context, caller NestedCaller, authority string, token,
	to common.Address, amount sdk.Int) (sdk.Context, error) {
	if err := k.checkTreasuryPayout(authority, to, amount); err != nil {
		return ctx, err
	}
	if _, err := k.treasuryBalance(ctx, caller, token, k.ModuleEVMAddress(ctx)); err != nil {
		return ctx, err
	}
	surplus, err := k.treasurySurplus(ctx, token)
	if err != nil {
		return ctx, err
	}
	if amount.GT(surplus) {
		return ctx, fmt.Errorf("%w: treasury %s surplus %s, needs %s", types.ErrInsufficientBalance, token.Hex(),
			surplus, amount)
	}
	if err := k.subLedgerAmount(ctx, types.TreasuryHeldKey(token), amount); err != nil {
		return ctx, err
	}
	return k.treasuryPay(ctx, caller, token, to, amount)
}

func (k *Keeper) checkTreasuryPayout(authority string, to common.Address, amount sdk.Int) error {
	if authority != k.authority {
		return fmt.Errorf("%w: %s", types.ErrUnauthorized, authority)
	}
	if to == (common.Address{}) {
		return types.ErrZeroAddress
	}
	return positiveAmount(amount)
}

func (k *Keeper) treasuryPay(ctx sdk.Context, caller NestedCaller, token, to common.Address,
	amount sdk.Int) (sdk.Context, error) {
	return k.treasuryCall(ctx, caller, k.ModuleEVMAddress(ctx), token,
		erc20Call(erc20Transfer, addressWord(to), amountWord(amount.BigInt())), types.ErrTransferFailed)
}
