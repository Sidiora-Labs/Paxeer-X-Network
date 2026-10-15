package keeper

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// MinimumShares is MINIMUM_SHARES, the shares of a pool's first deposit that
// the spot module account holds, counts in supply and never redeems.
const MinimumShares = 1000

// Topics of the PECORVault logs written at 0x1025.
var (
	DepositTopic          = crypto.Keccak256Hash([]byte("Deposit(address,address,uint256,uint256)"))
	LPWithdrawalTopic     = crypto.Keccak256Hash([]byte("LPWithdrawal(address,address,uint256,uint256)"))
	NativeDepositTopic    = crypto.Keccak256Hash([]byte("NativeDeposit(address,uint256,uint256)"))
	NativeWithdrawalTopic = crypto.Keccak256Hash([]byte("NativeWithdrawal(address,uint256,uint256)"))
	WithdrawalTopic       = crypto.Keccak256Hash([]byte("Withdrawal(address,address,uint256,uint256)"))
	TokenRegisteredTopic  = crypto.Keccak256Hash([]byte("TokenRegistered(address,uint8,bool)"))
)

// decimalsSelector is ERC20 decimals().
var decimalsSelector = []byte{0x31, 0x3c, 0xe5, 0x67}

var (
	vaultAddress = common.HexToAddress(types.PECORVaultAddress)
	maxUint256   = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

func vaultLog(ctx sdk.Context, topic common.Hash, indexed []common.Address, words ...*big.Int) *ethtypes.Log {
	topics := make([]common.Hash, 0, 1+len(indexed))
	topics = append(topics, topic)
	for _, address := range indexed {
		topics = append(topics, common.BytesToHash(address.Bytes()))
	}
	data := make([]byte, 0, 32*len(words))
	for _, word := range words {
		data = append(data, common.LeftPadBytes(word.Bytes(), 32)...)
	}
	return &ethtypes.Log{Address: vaultAddress, Topics: topics, Data: data, BlockNumber: uint64(ctx.BlockHeight())} //nolint:gosec
}

func reservesUpdatedLog(ctx sdk.Context, token common.Address, before, after sdk.Int) *ethtypes.Log {
	return vaultLog(ctx, ReservesUpdatedTopic, []common.Address{token}, before.BigInt(), after.BigInt())
}

// toUint256 converts a non-negative amount of at most 2^256 - 1.
func toUint256(value *big.Int) (sdk.Int, error) {
	if value.Sign() < 0 || value.Cmp(maxUint256) > 0 {
		return sdk.Int{}, fmt.Errorf("spot: amount %s outside uint256", value)
	}
	return sdk.NewIntFromBigInt(value), nil
}

func amountOrZero(value sdk.Int) *big.Int {
	if value.IsNil() {
		return new(big.Int)
	}
	return value.BigInt()
}

// GetBook reads the book of token (prefix 0x02); a token without a book has
// an empty one.
func (k *Keeper) GetBook(ctx sdk.Context, token common.Address) (types.TokenBook, error) {
	book, found, err := k.tokenBook(ctx, token)
	if err != nil {
		return types.TokenBook{}, err
	}
	if !found {
		return types.TokenBook{Token: token, Reserves: sdk.ZeroInt(), TotalDeposited: sdk.ZeroInt(),
			TotalWithdrawn: sdk.ZeroInt()}, nil
	}
	book.Reserves = sdk.NewIntFromBigInt(amountOrZero(book.Reserves))
	book.TotalDeposited = sdk.NewIntFromBigInt(amountOrZero(book.TotalDeposited))
	book.TotalWithdrawn = sdk.NewIntFromBigInt(amountOrZero(book.TotalWithdrawn))
	return book, nil
}

// SetBook writes the book of book.Token.
func (k *Keeper) SetBook(ctx sdk.Context, book types.TokenBook) error {
	for _, amount := range []sdk.Int{book.Reserves, book.TotalDeposited, book.TotalWithdrawn} {
		if _, err := toUint256(amountOrZero(amount)); err != nil {
			return err
		}
	}
	return setRecord(ctx.KVStore(k.storeKey), recordKey(types.BookPrefix, book.Token.Bytes()), book)
}

// reservedOf is the swap-rail plus order-rail fee accrual of token (prefix 0x04).
func (k *Keeper) reservedOf(ctx sdk.Context, token common.Address) (*big.Int, error) {
	store := ctx.KVStore(k.storeKey)
	swap, err := readFeeAccrual(store, types.FeeRailSwap, token)
	if err != nil {
		return nil, err
	}
	order, err := readFeeAccrual(store, types.FeeRailOrder, token)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Add(swap.BigInt(), order.BigInt()), nil
}

// registered returns the TokenParams of token, TokenNotRegistered when the
// tokens parameter does not list it.
func (k *Keeper) registered(ctx sdk.Context, token common.Address) (types.TokenParams, error) {
	all, err := k.GetParams(ctx)
	if err != nil {
		return types.TokenParams{}, err
	}
	for _, params := range all.Tokens {
		if params.Address == token {
			return params, nil
		}
	}
	return types.TokenParams{}, types.ErrTokenNotRegistered
}

// custodyLessEscrow is custody - escrow of token, 0 when escrow exceeds custody.
func (k *Keeper) custodyLessEscrow(ctx sdk.Context, token common.Address) (*big.Int, error) {
	_, custody, err := k.CustodyBalance(ctx, token)
	if err != nil {
		return nil, err
	}
	escrow, err := k.tokenEscrow(ctx, token)
	if err != nil {
		return nil, err
	}
	free := new(big.Int).Sub(amountOrZero(custody), amountOrZero(escrow))
	if free.Sign() < 0 {
		free.SetInt64(0)
	}
	return free, nil
}

// Spendable is max(0, min(custody - escrow, book) - reserved) of token
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:541-550).
func (k *Keeper) Spendable(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	free, err := k.custodyLessEscrow(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	book, err := k.GetBook(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	reserved, err := k.reservedOf(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	backed := free
	if book.Reserves.BigInt().Cmp(backed) < 0 {
		backed = book.Reserves.BigInt()
	}
	spendable := new(big.Int).Sub(backed, reserved)
	if spendable.Sign() < 0 {
		return sdk.ZeroInt(), nil
	}
	return toUint256(spendable)
}

// NAV of token is its spendable reserves.
func (k *Keeper) NAV(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	return k.Spendable(ctx, token)
}

// UntrackedFunds is custody - escrow - book of token, 0 when negative
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:433-437).
func (k *Keeper) UntrackedFunds(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	free, err := k.custodyLessEscrow(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	book, err := k.GetBook(ctx, token)
	if err != nil {
		return sdk.Int{}, err
	}
	untracked := free.Sub(free, book.Reserves.BigInt())
	if untracked.Sign() < 0 {
		return sdk.ZeroInt(), nil
	}
	return toUint256(untracked)
}

// MaxUtilizationBps is the utilization cap of a token,
// DEFAULT_MAX_UTILIZATION_BPS when it has none.
func MaxUtilizationBps(params types.TokenParams) uint64 {
	if params.MaxUtilizationBps == 0 {
		return types.DefaultMaxUtilizationBps
	}
	return params.MaxUtilizationBps
}

// HasLiquidity is true when token is registered and not paused, amount is at
// most its spendable reserves and amount x 10000 is at most spendable x
// maxUtilizationBps.
func (k *Keeper) HasLiquidity(ctx sdk.Context, token common.Address, amount sdk.Int) (bool, error) {
	params, err := k.registered(ctx, token)
	if err != nil {
		if errors.Is(err, types.ErrTokenNotRegistered) {
			return false, nil
		}
		return false, err
	}
	if params.Paused {
		return false, nil
	}
	spendable, err := k.Spendable(ctx, token)
	if err != nil {
		return false, err
	}
	want := amountOrZero(amount)
	if want.Cmp(spendable.BigInt()) > 0 {
		return false, nil
	}
	scaled := new(big.Int).Mul(want, big.NewInt(types.BpsDenominator))
	limit := new(big.Int).Mul(spendable.BigInt(), new(big.Int).SetUint64(MaxUtilizationBps(params)))
	return scaled.Cmp(limit) <= 0, nil
}

// withShareEVM runs fn on the EVM of the running transaction, or on a
// one-off EVM outside one.
func (k *Keeper) withShareEVM(ctx sdk.Context, fn func(*vm.EVM) error) error {
	if run := runOf(ctx); run != nil {
		return fn(run.EVM)
	}
	return k.evmKeeper.RunWithOneOffEVMInstance(ctx, fn, func(string, string) {})
}

func (k *Keeper) shareSupply(ctx sdk.Context, token common.Address) *big.Int {
	return amountOrZero(k.bankKeeper.GetSupply(ctx, k.ShareDenom(token)).Amount)
}

// depositable returns the params of a token open to deposits.
func (k *Keeper) depositable(ctx sdk.Context, token common.Address) (types.TokenParams, error) {
	params, err := k.registered(ctx, token)
	if err != nil {
		return types.TokenParams{}, err
	}
	if params.Paused {
		return types.TokenParams{}, types.ErrTokenPaused
	}
	return params, nil
}

// checkDepositNAV refuses a deposit into a pool with supply above zero and
// NAV 0 before any token moves.
func (k *Keeper) checkDepositNAV(ctx sdk.Context, token common.Address) error {
	if k.shareSupply(ctx, token).Sign() == 0 {
		return nil
	}
	nav, err := k.NAV(ctx, token)
	if err != nil {
		return err
	}
	if nav.IsZero() {
		return types.ErrInsufficientLiquidity
	}
	return nil
}

// creditDeposit mints the shares of a measured receipt to depositor and adds
// the receipt to the book. The share price is the NAV before the receipt
// enters the book, which a pulled custody balance does not change because
// spendable takes the minimum of custody and book.
func (k *Keeper) creditDeposit(ctx sdk.Context, token, depositor common.Address,
	received sdk.Int) (sdk.Int, types.TokenBook, sdk.Int, error) {
	supply := k.shareSupply(ctx, token)
	book, err := k.GetBook(ctx, token)
	if err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	var shares *big.Int
	if supply.Sign() == 0 {
		if received.BigInt().Cmp(big.NewInt(MinimumShares)) <= 0 {
			return sdk.Int{}, types.TokenBook{}, sdk.Int{}, types.ErrZeroAmount
		}
		shares = new(big.Int).Sub(received.BigInt(), big.NewInt(MinimumShares))
		if err := k.withShareEVM(ctx, func(evm *vm.EVM) error {
			return k.MintShares(ctx, evm, k.ShareDenom(token), k.CustodyEVMAddress(ctx), sdk.NewInt(MinimumShares))
		}); err != nil {
			return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
		}
		book.MinimumSharesLocked = true
	} else {
		nav, err := k.NAV(ctx, token)
		if err != nil {
			return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
		}
		if nav.IsZero() {
			return sdk.Int{}, types.TokenBook{}, sdk.Int{}, types.ErrInsufficientLiquidity
		}
		shares = new(big.Int).Mul(received.BigInt(), supply)
		shares.Quo(shares, nav.BigInt())
		if shares.Sign() == 0 {
			return sdk.Int{}, types.TokenBook{}, sdk.Int{}, types.ErrZeroAmount
		}
	}
	minted, err := toUint256(shares)
	if err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	if err := k.withShareEVM(ctx, func(evm *vm.EVM) error {
		return k.MintShares(ctx, evm, k.ShareDenom(token), depositor, minted)
	}); err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	before := book.Reserves
	reserves, err := toUint256(new(big.Int).Add(before.BigInt(), received.BigInt()))
	if err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	deposited, err := toUint256(new(big.Int).Add(book.TotalDeposited.BigInt(), received.BigInt()))
	if err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	book.Reserves, book.TotalDeposited = reserves, deposited
	if err := k.SetBook(ctx, book); err != nil {
		return sdk.Int{}, types.TokenBook{}, sdk.Int{}, err
	}
	return minted, book, before, nil
}

// Deposit pulls amount of a registered unpaused token from depositor through
// rail 0x1025, credits the measured receipt to the book and mints the shares
// of the share rule (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:232-242).
// It returns the minted shares and the Deposit and ReservesUpdated logs.
func (k *Keeper) Deposit(ctx sdk.Context, depositor, token common.Address,
	amount sdk.Int) (sdk.Int, []*ethtypes.Log, error) {
	if amount.IsNil() || !amount.IsPositive() {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	if _, err := k.depositable(ctx, token); err != nil {
		return sdk.Int{}, nil, err
	}
	if err := k.checkDepositNAV(ctx, token); err != nil {
		return sdk.Int{}, nil, err
	}
	ctx, received, err := k.pullTokens(ctx, vaultAddress, token, depositor, amount)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	shares, book, before, err := k.creditDeposit(ctx, token, depositor, received)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	return shares, []*ethtypes.Log{
		reservesUpdatedLog(ctx, token, before, book.Reserves),
		vaultLog(ctx, DepositTopic, []common.Address{token, depositor}, received.BigInt(), book.Reserves.BigInt()),
	}, nil
}

// DepositBatch deposits each (token, amount) pair; lists of different
// lengths, or empty ones, return InvalidArrayLength
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:246-266).
func (k *Keeper) DepositBatch(ctx sdk.Context, depositor common.Address, tokens []common.Address,
	amounts []sdk.Int) ([]sdk.Int, []*ethtypes.Log, error) {
	if len(tokens) == 0 || len(tokens) != len(amounts) {
		return nil, nil, types.ErrInvalidArrayLength
	}
	minted := make([]sdk.Int, 0, len(tokens))
	var logs []*ethtypes.Log
	for i, token := range tokens {
		shares, depositLogs, err := k.Deposit(ctx, depositor, token, amounts[i])
		if err != nil {
			return nil, nil, err
		}
		minted = append(minted, shares)
		logs = append(logs, depositLogs...)
	}
	return minted, logs, nil
}

// PAXToken is the registered NATIVE token whose denom is uhpx, the token of
// depositNative and withdrawNative.
func (k *Keeper) PAXToken(ctx sdk.Context) (types.TokenParams, error) {
	all, err := k.GetParams(ctx)
	if err != nil {
		return types.TokenParams{}, err
	}
	for _, params := range all.Tokens {
		if params.Kind != types.TokenKindNative {
			continue
		}
		if denom, _, ok := k.evmKeeper.GetNativePointee(ctx, params.Address.Hex()); ok && denom == evmkeeper.BaseDenom {
			return params, nil
		}
	}
	return types.TokenParams{}, types.ErrTokenNotRegistered
}

// DepositNative credits the value attached to rail 0x1025 as uhpx at
// value / 1e12 and mints PAX shares by the share rule
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:270-281). It
// returns the minted shares and the NativeDeposit and ReservesUpdated logs.
func (k *Keeper) DepositNative(ctx sdk.Context, depositor common.Address, value *big.Int) (sdk.Int, []*ethtypes.Log, error) {
	if value == nil || value.Sign() <= 0 {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	pax, err := k.PAXToken(ctx)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	if _, err := k.depositable(ctx, pax.Address); err != nil {
		return sdk.Int{}, nil, err
	}
	if err := k.checkDepositNAV(ctx, pax.Address); err != nil {
		return sdk.Int{}, nil, err
	}
	ctx, received, err := k.pullNative(ctx, vaultAddress, value)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	if !received.IsPositive() {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	shares, book, before, err := k.creditDeposit(ctx, pax.Address, depositor, received)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	return shares, []*ethtypes.Log{
		reservesUpdatedLog(ctx, pax.Address, before, book.Reserves),
		vaultLog(ctx, NativeDepositTopic, []common.Address{depositor}, received.BigInt(), book.Reserves.BigInt()),
	}, nil
}

// debitPayout takes amount out of the book of token after checking it
// against the spendable reserves.
func (k *Keeper) debitPayout(ctx sdk.Context, token common.Address, amount *big.Int) (types.TokenBook, sdk.Int, error) {
	spendable, err := k.Spendable(ctx, token)
	if err != nil {
		return types.TokenBook{}, sdk.Int{}, err
	}
	if amount.Cmp(spendable.BigInt()) > 0 {
		return types.TokenBook{}, sdk.Int{}, types.ErrInsufficientLiquidity
	}
	book, err := k.GetBook(ctx, token)
	if err != nil {
		return types.TokenBook{}, sdk.Int{}, err
	}
	before := book.Reserves
	reserves, err := toUint256(new(big.Int).Sub(before.BigInt(), amount))
	if err != nil {
		return types.TokenBook{}, sdk.Int{}, types.ErrInsufficientLiquidity
	}
	withdrawn, err := toUint256(new(big.Int).Add(book.TotalWithdrawn.BigInt(), amount))
	if err != nil {
		return types.TokenBook{}, sdk.Int{}, err
	}
	book.Reserves, book.TotalWithdrawn = reserves, withdrawn
	if err := k.SetBook(ctx, book); err != nil {
		return types.TokenBook{}, sdk.Int{}, err
	}
	return book, before, nil
}

// Withdraw burns shares of provider and pays shares x NAV / supply of token,
// rounded down. It returns the amount paid and the ReservesUpdated,
// LPWithdrawal and Withdrawal logs.
func (k *Keeper) Withdraw(ctx sdk.Context, provider, token common.Address, shares sdk.Int) (sdk.Int, []*ethtypes.Log, error) {
	if shares.IsNil() || !shares.IsPositive() {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	if _, err := k.registered(ctx, token); err != nil {
		return sdk.Int{}, nil, err
	}
	supply := k.shareSupply(ctx, token)
	if supply.Sign() == 0 {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	nav, err := k.NAV(ctx, token)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	payout := new(big.Int).Mul(shares.BigInt(), nav.BigInt())
	payout.Quo(payout, supply)
	if payout.Sign() == 0 {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	book, before, err := k.debitPayout(ctx, token, payout)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	if err := k.withShareEVM(ctx, func(evm *vm.EVM) error {
		return k.BurnShares(ctx, evm, k.ShareDenom(token), provider, shares)
	}); err != nil {
		return sdk.Int{}, nil, err
	}
	amount := sdk.NewIntFromBigInt(payout)
	if _, err := k.pushTokens(ctx, token, provider, amount); err != nil {
		return sdk.Int{}, nil, err
	}
	return amount, []*ethtypes.Log{
		reservesUpdatedLog(ctx, token, before, book.Reserves),
		vaultLog(ctx, LPWithdrawalTopic, []common.Address{token, provider}, shares.BigInt(), payout),
		vaultLog(ctx, WithdrawalTopic, []common.Address{token, provider}, payout, book.Reserves.BigInt()),
	}, nil
}

// WithdrawNative burns the PAX shares of provider worth amount, amount x
// supply / NAV rounded up, and pays amount uhpx to recipient
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:320-339). It
// returns the burned shares and the ReservesUpdated and NativeWithdrawal logs.
func (k *Keeper) WithdrawNative(ctx sdk.Context, provider common.Address, amount sdk.Int,
	recipient common.Address) (sdk.Int, []*ethtypes.Log, error) {
	if recipient == (common.Address{}) {
		return sdk.Int{}, nil, types.ErrZeroAddress
	}
	if amount.IsNil() || !amount.IsPositive() {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	pax, err := k.PAXToken(ctx)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	nav, err := k.NAV(ctx, pax.Address)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	if nav.IsZero() || amount.BigInt().Cmp(nav.BigInt()) > 0 {
		return sdk.Int{}, nil, types.ErrInsufficientLiquidity
	}
	supply := k.shareSupply(ctx, pax.Address)
	burned := new(big.Int).Mul(amount.BigInt(), supply)
	burned.Add(burned, new(big.Int).Sub(nav.BigInt(), big.NewInt(1)))
	burned.Quo(burned, nav.BigInt())
	if burned.Sign() == 0 {
		return sdk.Int{}, nil, types.ErrZeroAmount
	}
	shares, err := toUint256(burned)
	if err != nil {
		return sdk.Int{}, nil, err
	}
	book, before, err := k.debitPayout(ctx, pax.Address, amount.BigInt())
	if err != nil {
		return sdk.Int{}, nil, err
	}
	if err := k.withShareEVM(ctx, func(evm *vm.EVM) error {
		return k.BurnShares(ctx, evm, k.ShareDenom(pax.Address), provider, shares)
	}); err != nil {
		return sdk.Int{}, nil, err
	}
	if _, err := k.pushTokens(ctx, pax.Address, recipient, amount); err != nil {
		return sdk.Int{}, nil, err
	}
	return shares, []*ethtypes.Log{
		reservesUpdatedLog(ctx, pax.Address, before, book.Reserves),
		vaultLog(ctx, NativeWithdrawalTopic, []common.Address{recipient}, amount.BigInt(), book.Reserves.BigInt()),
	}, nil
}

// ApplyTrade adds amountIn to the book of tokenIn and takes netOut from the
// book of tokenOut, the retained fee and impact staying in the book of
// tokenOut as a reservation (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:295,
// :313, Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:719-720). It
// returns the ReservesUpdated log of each book.
func (k *Keeper) ApplyTrade(ctx sdk.Context, tokenIn common.Address, amountIn sdk.Int, tokenOut common.Address,
	netOut sdk.Int) ([]*ethtypes.Log, error) {
	if tokenIn == tokenOut {
		return nil, types.ErrSameToken
	}
	if amountIn.IsNil() || !amountIn.IsPositive() || netOut.IsNil() || netOut.IsNegative() {
		return nil, types.ErrZeroAmount
	}
	in, err := k.GetBook(ctx, tokenIn)
	if err != nil {
		return nil, err
	}
	out, err := k.GetBook(ctx, tokenOut)
	if err != nil {
		return nil, err
	}
	inBefore, outBefore := in.Reserves, out.Reserves
	if in.Reserves, err = toUint256(new(big.Int).Add(inBefore.BigInt(), amountIn.BigInt())); err != nil {
		return nil, err
	}
	if out.Reserves, err = toUint256(new(big.Int).Sub(outBefore.BigInt(), netOut.BigInt())); err != nil {
		return nil, types.ErrInsufficientLiquidity
	}
	if err := k.SetBook(ctx, in); err != nil {
		return nil, err
	}
	if err := k.SetBook(ctx, out); err != nil {
		return nil, err
	}
	return []*ethtypes.Log{
		reservesUpdatedLog(ctx, tokenIn, inBefore, in.Reserves),
		reservesUpdatedLog(ctx, tokenOut, outBefore, out.Reserves),
	}, nil
}

// SyncReserves reconciles the book of token with custody - escrow under the
// governance authority, the reconciliation of
// Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:464-482: a
// surplus raises the book and totalDeposited by the recovered amount, a
// deficit lowers the book with 0 recovered, parity changes nothing. Custody
// below the escrow refuses with InsufficientLiquidity(), as no book can back
// it. It returns the ReservesSync and ReservesUpdated logs.
func (k *Keeper) SyncReserves(ctx sdk.Context, authority string, token common.Address) ([]*ethtypes.Log, error) {
	if authority != k.authority {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("spot: invalid authority %s, expected %s", authority, k.authority)
	}
	if _, err := k.registered(ctx, token); err != nil {
		return nil, err
	}
	book, found, err := k.tokenBook(ctx, token)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, types.ErrTokenNotRegistered
	}
	escrow, err := k.tokenEscrow(ctx, token)
	if err != nil {
		return nil, err
	}
	ctx, custody, err := k.CustodyBalance(ctx, token)
	if err != nil {
		return nil, err
	}
	if custody.LT(escrow) {
		return nil, types.ErrInsufficientLiquidity
	}
	balance, onBook := custody.Sub(escrow), sdk.NewIntFromBigInt(amountOrZero(book.Reserves))
	if balance.Equal(onBook) {
		return nil, nil
	}
	recovered := sdk.ZeroInt()
	if balance.GT(onBook) {
		recovered = balance.Sub(onBook)
		book.TotalDeposited = sdk.NewIntFromBigInt(amountOrZero(book.TotalDeposited)).Add(recovered)
	}
	book.Reserves = balance
	if err := k.SetBook(ctx, book); err != nil {
		return nil, err
	}
	return []*ethtypes.Log{
		vaultLog(ctx, ReservesSyncTopic, []common.Address{token}, onBook.BigInt(), balance.BigInt(), recovered.BigInt()),
		reservesUpdatedLog(ctx, token, onBook, book.Reserves),
	}, nil
}

// tokenDecimals reads the decimals of a token being registered: decimals()
// of an ERC20 contract, refused unless it returns one word
// (Launch-x:contracts/contracts/meta-ag/vault/PECORVault.sol:499-509), and
// the largest denom metadata exponent of a NATIVE pointer.
func (k *Keeper) tokenDecimals(ctx sdk.Context, params types.TokenParams) (uint64, error) {
	switch params.Kind {
	case types.TokenKindERC20:
		if len(k.evmKeeper.GetCode(ctx, params.Address)) == 0 {
			return 0, types.ErrUnsupportedToken
		}
		ret, err := k.evmKeeper.StaticCallEVM(ctx, CustodyAddress(), &params.Address, decimalsSelector)
		if err != nil || len(ret) != 32 {
			return 0, types.ErrUnsupportedToken
		}
		value := new(big.Int).SetBytes(ret)
		if !value.IsUint64() {
			return 0, types.ErrUnsupportedToken
		}
		return value.Uint64(), nil
	case types.TokenKindNative:
		denom, _, ok := k.evmKeeper.GetNativePointee(ctx, params.Address.Hex())
		if !ok {
			return 0, types.ErrUnsupportedToken
		}
		metadata, ok := k.bankKeeper.GetDenomMetaData(ctx, denom)
		if !ok {
			return 0, types.ErrUnsupportedToken
		}
		var decimals uint64
		for _, unit := range metadata.DenomUnits {
			if uint64(unit.Exponent) > decimals && unit.Exponent <= math.MaxUint8 {
				decimals = uint64(unit.Exponent)
			}
		}
		return decimals, nil
	default:
		return 0, types.ErrUnsupportedTransfer
	}
}

// RegisterToken records a new token of the tokens parameter: it reads the
// decimals of its kind, refuses more than 36, or decimals that differ from
// a pinned non-zero params.Decimals, with UnsupportedToken, and writes its
// empty book. It returns params with the decimals read and the
// TokenRegistered and UtilizationCapUpdated logs; the caller writes the
// tokens parameter.
func (k *Keeper) RegisterToken(ctx sdk.Context, params types.TokenParams) (types.TokenParams, []*ethtypes.Log, error) {
	if params.Address == (common.Address{}) {
		return types.TokenParams{}, nil, types.ErrZeroAddress
	}
	if _, err := k.registered(ctx, params.Address); err == nil {
		return types.TokenParams{}, nil, types.ErrTokenAlreadyRegistered
	} else if !errors.Is(err, types.ErrTokenNotRegistered) {
		return types.TokenParams{}, nil, err
	}
	decimals, err := k.tokenDecimals(ctx, params)
	if err != nil {
		return types.TokenParams{}, nil, err
	}
	if decimals > types.MaxTokenDecimals || (params.Decimals != 0 && uint64(params.Decimals) != decimals) {
		return types.TokenParams{}, nil, types.ErrUnsupportedToken
	}
	params.Decimals = uint32(decimals)
	params.MaxUtilizationBps = MaxUtilizationBps(params)
	book, err := k.GetBook(ctx, params.Address)
	if err != nil {
		return types.TokenParams{}, nil, err
	}
	book.Decimals = uint8(decimals)
	book.ShareDenom = k.ShareDenom(params.Address)
	if err := k.SetBook(ctx, book); err != nil {
		return types.TokenParams{}, nil, err
	}
	return params, []*ethtypes.Log{
		vaultLog(ctx, TokenRegisteredTopic, []common.Address{params.Address}, new(big.Int).SetUint64(decimals),
			new(big.Int).SetBytes(boolWord(params.IsStablecoin))),
		vaultLog(ctx, UtilizationCapUpdatedTopic, []common.Address{params.Address},
			new(big.Int).SetUint64(params.MaxUtilizationBps)),
	}, nil
}

// TokenParamLogs are the logs of a change of the tokens parameter from
// before to after: TokenPauseUpdated for each token whose paused flag
// changes and UtilizationCapUpdated for each token whose cap changes. A
// token new in after logs through RegisterToken.
func TokenParamLogs(ctx sdk.Context, before, after []types.TokenParams) []*ethtypes.Log {
	previous := make(map[common.Address]types.TokenParams, len(before))
	for _, params := range before {
		previous[params.Address] = params
	}
	var logs []*ethtypes.Log
	for _, params := range after {
		old, ok := previous[params.Address]
		if !ok {
			continue
		}
		if old.Paused != params.Paused {
			logs = append(logs, vaultLog(ctx, TokenPauseUpdatedTopic, []common.Address{params.Address},
				new(big.Int).SetBytes(boolWord(params.Paused))))
		}
		if MaxUtilizationBps(old) != MaxUtilizationBps(params) {
			logs = append(logs, vaultLog(ctx, UtilizationCapUpdatedTopic, []common.Address{params.Address},
				new(big.Int).SetUint64(MaxUtilizationBps(params))))
		}
	}
	return logs
}
