package keeper

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// NestedCaller runs a call into an EVM contract with an explicit caller: the
// precompile run serves it through evm.Call, the upgrade handler through
// CallEVM. Call returns the context the run continues on, because a nested
// call may leave the state on a new branch.
type NestedCaller interface {
	Call(ctx sdk.Context, caller common.Address, to common.Address, input []byte) (sdk.Context, []byte, error)
	StaticCall(ctx sdk.Context, caller common.Address, to common.Address, input []byte) ([]byte, error)
}

// evmKeeperCaller serves NestedCaller outside an EVM run through CallEVM and
// StaticCallEVM, which both finish on the context they are given.
type evmKeeperCaller struct{ evm types.EVMKeeper }

func (c evmKeeperCaller) Call(ctx sdk.Context, caller common.Address, to common.Address,
	input []byte) (sdk.Context, []byte, error) {
	ret, err := c.evm.CallEVM(ctx, caller, &to, nil, input)
	return ctx, ret, err
}

func (c evmKeeperCaller) StaticCall(ctx sdk.Context, caller common.Address, to common.Address,
	input []byte) ([]byte, error) {
	return c.evm.StaticCallEVM(ctx, c.evm.GetPaxAddressOrDefault(ctx, caller), &to, input)
}

// ModuleCaller is the NestedCaller of module context, the upgrade handler.
func (k *Keeper) ModuleCaller() NestedCaller { return evmKeeperCaller{evm: k.evmKeeper} }

// ModuleEVMAddress is the EVM address of the launchpad module account, which
// holds every launchpad USDL as its ERC20 balance.
func (k *Keeper) ModuleEVMAddress(ctx sdk.Context) common.Address {
	return k.evmKeeper.GetEVMAddressOrDefault(ctx, k.ModuleAddress())
}

type quoteLedgerKind uint8

const (
	ledgerHeldQuote quoteLedgerKind = iota + 1
	ledgerFeeHeld
	ledgerTreasuryHeld
)

// QuoteLedger names one custody ledger of the USDL at the launchpad module
// EVM address: HeldQuote of a pool, FeeHeld of a FeeAccumulator or
// TreasuryHeld of the quote token.
type QuoteLedger struct {
	kind  quoteLedgerKind
	owner common.Address
}

func HeldQuoteLedger(pool common.Address) QuoteLedger {
	return QuoteLedger{kind: ledgerHeldQuote, owner: pool}
}

func FeeHeldLedger(accumulator common.Address) QuoteLedger {
	return QuoteLedger{kind: ledgerFeeHeld, owner: accumulator}
}

func TreasuryHeldLedger() QuoteLedger { return QuoteLedger{kind: ledgerTreasuryHeld} }

func (l QuoteLedger) key(token common.Address) ([]byte, error) {
	switch l.kind {
	case ledgerHeldQuote:
		return types.HeldQuoteKey(l.owner), nil
	case ledgerFeeHeld:
		return types.FeeHeldKey(l.owner), nil
	case ledgerTreasuryHeld:
		return types.TreasuryHeldKey(token), nil
	default:
		return nil, fmt.Errorf("%w: unknown quote ledger %d", types.ErrInvalidConfiguration, l.kind)
	}
}

func (k *Keeper) quoteToken(ctx sdk.Context) common.Address {
	return common.HexToAddress(k.GetParams(ctx).QuoteToken)
}

// GetQuoteLedger reads one custody ledger.
func (k *Keeper) GetQuoteLedger(ctx sdk.Context, ledger QuoteLedger) (sdk.Int, error) {
	key, err := ledger.key(k.quoteToken(ctx))
	if err != nil {
		return sdk.Int{}, err
	}
	return k.getLedgerAmount(ctx, key)
}

func (k *Keeper) getLedgerAmount(ctx sdk.Context, key []byte) (sdk.Int, error) {
	bz := k.store(ctx).Get(key)
	if bz == nil {
		return sdk.ZeroInt(), nil
	}
	var out sdk.Int
	if err := out.Unmarshal(bz); err != nil {
		return sdk.Int{}, fmt.Errorf("%w: quote ledger %x: %v", types.ErrOverflow, key, err)
	}
	if !uint256Amount(out) {
		return sdk.Int{}, fmt.Errorf("%w: quote ledger %x", types.ErrOverflow, key)
	}
	return out, nil
}

func (k *Keeper) setLedgerAmount(ctx sdk.Context, key []byte, value sdk.Int) error {
	if !uint256Amount(value) {
		return types.ErrOverflow
	}
	bz, err := value.Marshal()
	if err != nil {
		return err
	}
	k.store(ctx).Set(key, bz)
	return nil
}

func (k *Keeper) addLedgerAmount(ctx sdk.Context, key []byte, amount sdk.Int) error {
	current, err := k.getLedgerAmount(ctx, key)
	if err != nil {
		return err
	}
	total := current.Add(amount)
	if !uint256Amount(total) {
		return types.ErrOverflow
	}
	return k.setLedgerAmount(ctx, key, total)
}

func (k *Keeper) subLedgerAmount(ctx sdk.Context, key []byte, amount sdk.Int) error {
	current, err := k.getLedgerAmount(ctx, key)
	if err != nil {
		return err
	}
	if current.LT(amount) {
		return fmt.Errorf("%w: quote ledger %x holds %s, needs %s", types.ErrInsufficientBalance, key, current, amount)
	}
	return k.setLedgerAmount(ctx, key, current.Sub(amount))
}

func uint256Amount(value sdk.Int) bool {
	return !value.IsNil() && !value.IsNegative() && value.BigInt().Cmp(types.MaxUint256()) <= 0
}

func positiveAmount(amount sdk.Int) error {
	if !uint256Amount(amount) {
		return types.ErrOverflow
	}
	if amount.IsZero() {
		return types.ErrZeroAmount
	}
	return nil
}

var (
	erc20Transfer     = crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	erc20TransferFrom = crypto.Keccak256([]byte("transferFrom(address,address,uint256)"))[:4]
	erc20BalanceOf    = crypto.Keccak256([]byte("balanceOf(address)"))[:4]
)

func addressWord(address common.Address) []byte { return common.LeftPadBytes(address.Bytes(), 32) }

func amountWord(amount *big.Int) []byte { return common.LeftPadBytes(amount.Bytes(), 32) }

func erc20Call(selector []byte, words ...[]byte) []byte {
	out := append([]byte(nil), selector...)
	for _, word := range words {
		out = append(out, word...)
	}
	return out
}

// quoteBalance reads USDL.balanceOf(holder) through a nested staticcall from
// the launchpad precompile.
func (k *Keeper) quoteBalance(ctx sdk.Context, caller NestedCaller, token, holder common.Address) (sdk.Int, error) {
	ret, err := caller.StaticCall(ctx, common.HexToAddress(types.LaunchpadAddress), token,
		erc20Call(erc20BalanceOf, addressWord(holder)))
	if err != nil {
		return sdk.Int{}, fmt.Errorf("%w: balanceOf %s: %w", types.ErrInvalidBalance, holder, err)
	}
	if len(ret) != 32 {
		return sdk.Int{}, fmt.Errorf("%w: balanceOf %s returned %d bytes", types.ErrInvalidBalance, holder, len(ret))
	}
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret)), nil
}

// callQuote runs one USDL call that must return true and refuses with failure
// otherwise.
func callQuote(ctx sdk.Context, caller NestedCaller, from, token common.Address, input []byte,
	failure error) (sdk.Context, error) {
	next, ret, err := caller.Call(ctx, from, token, input)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", failure, err)
	}
	if len(ret) != 32 || new(big.Int).SetBytes(ret).Cmp(big.NewInt(1)) != 0 {
		return ctx, fmt.Errorf("%w: return data %x", failure, ret)
	}
	return next, nil
}

// receive runs a USDL call that pays the launchpad module EVM address and
// credits ledger with the rise of its balance.
func (k *Keeper) receive(ctx sdk.Context, caller NestedCaller, from common.Address, input []byte,
	ledger QuoteLedger, failure error) (sdk.Context, sdk.Int, error) {
	token := k.quoteToken(ctx)
	key, err := ledger.key(token)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	custody := k.ModuleEVMAddress(ctx)
	before, err := k.quoteBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx, err = callQuote(ctx, caller, from, token, input, failure)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	after, err := k.quoteBalance(ctx, caller, token, custody)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if !after.GT(before) {
		return ctx, sdk.Int{}, fmt.Errorf("%w: custody balance %s did not rise", failure, after)
	}
	received := after.Sub(before)
	if err := k.addLedgerAmount(ctx, key, received); err != nil {
		return ctx, sdk.Int{}, err
	}
	return ctx, received, nil
}

// SweepPool moves the USDL sitting at a pool address into custody with
// USDL.transfer(launchpad module EVM address, balanceOf(pool)) called by the
// pool and credits HeldQuote(pool), so USDL sent straight to the pool counts
// as prepaid input. A pool without USDL makes no call.
func (k *Keeper) SweepPool(ctx sdk.Context, caller NestedCaller, pool common.Address) (sdk.Context, sdk.Int, error) {
	balance, err := k.quoteBalance(ctx, caller, k.quoteToken(ctx), pool)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if balance.IsZero() {
		return ctx, balance, nil
	}
	return k.receive(ctx, caller, pool,
		erc20Call(erc20Transfer, addressWord(k.ModuleEVMAddress(ctx)), amountWord(balance.BigInt())),
		HeldQuoteLedger(pool), types.ErrTransferFailed)
}

// PullQuote pulls amount from user with USDL.transferFrom(user, launchpad
// module EVM address, amount) called by 0x1017, spending the user's allowance
// to 0x1017, and credits ledger with the rise of the custody balance.
func (k *Keeper) PullQuote(ctx sdk.Context, caller NestedCaller, user common.Address, ledger QuoteLedger,
	amount sdk.Int) (sdk.Context, sdk.Int, error) {
	if err := positiveAmount(amount); err != nil {
		return ctx, sdk.Int{}, err
	}
	return k.receive(ctx, caller, common.HexToAddress(types.LaunchpadAddress),
		erc20Call(erc20TransferFrom, addressWord(user), addressWord(k.ModuleEVMAddress(ctx)), amountWord(amount.BigInt())),
		ledger, types.ErrTransferFromFailed)
}

// PayQuote debits ledger and pays amount to to with USDL.transfer(to, amount)
// called by the launchpad module EVM address. The debit is written before the
// call so the nested call sees it.
func (k *Keeper) PayQuote(ctx sdk.Context, caller NestedCaller, ledger QuoteLedger, to common.Address,
	amount sdk.Int) (sdk.Context, error) {
	if err := positiveAmount(amount); err != nil {
		return ctx, err
	}
	if to == (common.Address{}) {
		return ctx, types.ErrZeroAddress
	}
	token := k.quoteToken(ctx)
	key, err := ledger.key(token)
	if err != nil {
		return ctx, err
	}
	if err := k.subLedgerAmount(ctx, key, amount); err != nil {
		return ctx, err
	}
	return callQuote(ctx, caller, k.ModuleEVMAddress(ctx), token,
		erc20Call(erc20Transfer, addressWord(to), amountWord(amount.BigInt())), types.ErrTransferFailed)
}

// MoveQuote moves amount between two custody ledgers. The USDL stays at the
// launchpad module EVM address, so no call is made and no Transfer log is
// written.
func (k *Keeper) MoveQuote(ctx sdk.Context, from, to QuoteLedger, amount sdk.Int) error {
	if err := positiveAmount(amount); err != nil {
		return err
	}
	token := k.quoteToken(ctx)
	fromKey, err := from.key(token)
	if err != nil {
		return err
	}
	toKey, err := to.key(token)
	if err != nil {
		return err
	}
	held, err := k.getLedgerAmount(ctx, fromKey)
	if err != nil {
		return err
	}
	if held.LT(amount) {
		return fmt.Errorf("%w: quote ledger %x holds %s, needs %s", types.ErrInsufficientBalance, fromKey, held, amount)
	}
	if bytes.Equal(fromKey, toKey) {
		return nil
	}
	current, err := k.getLedgerAmount(ctx, toKey)
	if err != nil {
		return err
	}
	total := current.Add(amount)
	if !uint256Amount(total) {
		return types.ErrOverflow
	}
	if err := k.setLedgerAmount(ctx, fromKey, held.Sub(amount)); err != nil {
		return err
	}
	return k.setLedgerAmount(ctx, toKey, total)
}
