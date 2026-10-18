package keeper

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type SwapResult struct {
	AmountIn       *big.Int
	AmountOut      *big.Int
	GrossOutput    *big.Int
	ExecutionPrice *big.Int
	Logs           []*ethtypes.Log
}

func (k *Keeper) MarketBuy(ctx sdk.Context, user, stablecoin, token common.Address, amount, minimum, deadline *big.Int) (SwapResult, error) {
	return k.marketSwap(ctx, user, stablecoin, token, amount, minimum, deadline, true)
}

func (k *Keeper) MarketSell(ctx sdk.Context, user, token, stablecoin common.Address, amount, minimum, deadline *big.Int) (SwapResult, error) {
	return k.marketSwap(ctx, user, stablecoin, token, amount, minimum, deadline, false)
}

func (k *Keeper) marketSwap(ctx sdk.Context, user, stablecoin, token common.Address, amount, minimum, deadline *big.Int, buy bool) (SwapResult, error) {
	cache, write := ctx.CacheContext()
	if err := k.swapDeadline(cache, deadline); err != nil {
		return SwapResult{}, err
	}
	params, err := k.GetParams(cache)
	if err != nil {
		return SwapResult{}, err
	}
	registeredStable := false
	for _, token := range params.Tokens {
		if token.Address == stablecoin && token.IsStablecoin {
			registeredStable = true
			break
		}
	}
	if !registeredStable {
		return SwapResult{}, types.ErrNotAStablecoin
	}
	base, err := k.tokenOf(cache, token)
	if err != nil {
		return SwapResult{}, err
	}
	if base.IsStablecoin {
		return SwapResult{}, types.ErrTokenIsStablecoin
	}
	input, output := stablecoin, token
	if !buy {
		input, output = token, stablecoin
	}
	result, err := k.swapExactInput(cache, user, input, output, amount, minimum, false, false)
	if err != nil {
		return SwapResult{}, err
	}
	stableAmount, tokenAmount := result.AmountIn, result.AmountOut
	if !buy {
		stableAmount, tokenAmount = result.AmountOut, result.AmountIn
	}
	side := new(big.Int)
	if buy {
		side.SetUint64(1)
	}
	result.Logs = append(result.Logs, swapLog(types.PECORAddress,
		"MarketOrderExecuted(address,address,address,uint256,uint256,bool)",
		[]common.Address{user, stablecoin, token}, stableAmount, tokenAmount, side))
	write()
	return result, nil
}

func (k *Keeper) SwapExactIn(ctx sdk.Context, user, tokenIn, tokenOut common.Address, amount, minimum, deadline *big.Int) (SwapResult, error) {
	cache, write := ctx.CacheContext()
	if err := k.swapDeadline(cache, deadline); err != nil {
		return SwapResult{}, err
	}
	if tokenIn == tokenOut {
		return SwapResult{}, types.ErrSameToken
	}
	result, err := k.swapExactInput(cache, user, tokenIn, tokenOut, amount, minimum, false, false)
	if err != nil {
		return SwapResult{}, err
	}
	result.Logs = append(result.Logs, swapLog(types.PECORAddress,
		"SimpleSwap(address,address,address,uint256,uint256,uint256)",
		[]common.Address{user, tokenIn, tokenOut}, result.AmountIn, result.AmountOut, result.ExecutionPrice))
	write()
	return result, nil
}

func (k *Keeper) SwapExactOut(ctx sdk.Context, user, tokenIn, tokenOut common.Address, amountOut, maximum, deadline *big.Int) (SwapResult, error) {
	cache, write := ctx.CacheContext()
	if err := k.swapDeadline(cache, deadline); err != nil {
		return SwapResult{}, err
	}
	if tokenIn == tokenOut {
		return SwapResult{}, types.ErrSameToken
	}
	if err := swapAmount(amountOut, true); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(maximum, false); err != nil {
		return SwapResult{}, err
	}
	if err := k.swapTokens(cache, tokenIn, tokenOut); err != nil {
		return SwapResult{}, err
	}
	rail := common.HexToAddress(types.PECORAddress)
	required, err := k.QuoteExactOut(cache, rail, tokenIn, tokenOut, sdk.NewIntFromBigInt(amountOut))
	if err != nil {
		return SwapResult{}, err
	}
	amountIn := required.BigInt()
	if err := swapAmount(amountIn, true); err != nil {
		return SwapResult{}, err
	}
	if amountIn.Cmp(maximum) > 0 {
		return SwapResult{}, types.ErrExcessiveInput
	}
	quote, err := k.Price(cache, rail, tokenIn, tokenOut, required)
	if err != nil {
		return SwapResult{}, err
	}
	gross, net := quote.Gross.BigInt(), quote.Net.BigInt()
	if err := swapAmount(gross, false); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(net, false); err != nil {
		return SwapResult{}, err
	}
	if net.Cmp(amountOut) < 0 || gross.Cmp(amountOut) < 0 {
		return SwapResult{}, types.ErrInsufficientOutput
	}
	cache, received, err := k.pullTokens(cache, rail, tokenIn, user, required)
	if err != nil {
		return SwapResult{}, err
	}
	if received.IsNil() || !received.Equal(required) {
		return SwapResult{}, types.ErrUnsupportedInputTransfer
	}
	liquid, err := k.HasLiquidity(cache, tokenOut, quote.Gross)
	if err != nil {
		return SwapResult{}, err
	}
	if !liquid {
		return SwapResult{}, types.ErrInsufficientLiquidity
	}
	price, err := k.IndexPrice(cache, tokenOut)
	if err != nil {
		return SwapResult{}, err
	}
	result := SwapResult{AmountIn: new(big.Int).Set(amountIn), AmountOut: new(big.Int).Set(amountOut), GrossOutput: new(big.Int).Set(gross), ExecutionPrice: price.BigInt()}
	if err := k.settleSwap(cache, user, tokenIn, tokenOut, &result, false); err != nil {
		return SwapResult{}, err
	}
	result.Logs = append(result.Logs, swapLog(types.PECORAddress,
		"SimpleSwap(address,address,address,uint256,uint256,uint256)",
		[]common.Address{user, tokenIn, tokenOut}, result.AmountIn, result.AmountOut, result.ExecutionPrice))
	write()
	return result, nil
}

func (k *Keeper) SwapNative(ctx sdk.Context, user, tokenOther common.Address, amount, minimum, deadline *big.Int, nativeIsInput bool) (SwapResult, error) {
	cache, write := ctx.CacheContext()
	if err := k.swapDeadline(cache, deadline); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(amount, true); err != nil {
		return SwapResult{}, err
	}
	pax, _, exists := k.evmKeeper.GetERC20NativePointer(cache, "uhpx")
	if !exists {
		return SwapResult{}, types.ErrUnsupportedTransfer
	}
	if tokenOther == pax {
		if nativeIsInput {
			return SwapResult{}, types.ErrUseWethDeposit
		}
		return SwapResult{}, types.ErrUseWethWithdraw
	}
	native, err := k.tokenOf(cache, pax)
	if err != nil {
		return SwapResult{}, err
	}
	if native.Kind != types.TokenKindNative {
		return SwapResult{}, types.ErrUnsupportedTransfer
	}
	tokenIn, tokenOut := pax, tokenOther
	if !nativeIsInput {
		tokenIn, tokenOut = tokenOther, pax
	}
	result, err := k.swapExactInput(cache, user, tokenIn, tokenOut, amount, minimum, nativeIsInput, !nativeIsInput)
	if err != nil {
		return SwapResult{}, err
	}
	nativeAmount, tokenAmount, direction := result.AmountOut, result.AmountIn, new(big.Int)
	if nativeIsInput {
		nativeAmount, tokenAmount = result.AmountIn, result.AmountOut
		direction.SetUint64(1)
	}
	result.Logs = append(result.Logs, swapLog(types.PECORAddress,
		"NativeSwap(address,address,uint256,uint256,bool)",
		[]common.Address{user, tokenOther}, nativeAmount, tokenAmount, direction))
	write()
	return result, nil
}

func (k *Keeper) swapDeadline(ctx sdk.Context, deadline *big.Int) error {
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if params.SwapPaused {
		return types.ErrEnforcedPause
	}
	if err := swapAmount(deadline, false); err != nil {
		return err
	}
	if deadline.Sign() != 0 && deadline.Cmp(big.NewInt(ctx.BlockTime().Unix())) < 0 {
		return types.ErrExpired
	}
	return nil
}

func swapAmount(amount *big.Int, positive bool) error {
	if amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 {
		return types.ErrUnsupportedInputTransfer
	}
	if positive && amount.Sign() == 0 {
		return types.ErrZeroAmount
	}
	return nil
}

func (k *Keeper) swapTokens(ctx sdk.Context, input, output common.Address) error {
	for _, address := range []common.Address{input, output} {
		token, err := k.tokenOf(ctx, address)
		if err != nil {
			return err
		}
		if token.Paused {
			return types.ErrTokenPaused
		}
	}
	for _, address := range []common.Address{input, output} {
		token, err := k.tokenOf(ctx, address)
		if err != nil {
			return err
		}
		if token.IsStablecoin {
			continue
		}
		index, err := k.IndexPrice(ctx, address)
		if err != nil {
			return err
		}
		if err := k.CheckTwapSanity(ctx, address, index); err != nil {
			return err
		}
	}
	return nil
}

func (k *Keeper) swapExactInput(ctx sdk.Context, user, input, output common.Address, amount, minimum *big.Int, nativeInput, nativeOutput bool) (SwapResult, error) {
	if err := swapAmount(amount, true); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(minimum, false); err != nil {
		return SwapResult{}, err
	}
	if err := k.swapTokens(ctx, input, output); err != nil {
		return SwapResult{}, err
	}
	rail := common.HexToAddress(types.PECORAddress)
	var pulled sdk.Int
	var err error
	if nativeInput {
		converted, remainder := new(big.Int), new(big.Int)
		converted.QuoRem(amount, big.NewInt(1_000_000_000_000), remainder)
		if remainder.Sign() != 0 {
			return SwapResult{}, types.ErrUnsupportedInputTransfer
		}
		ctx, pulled, err = k.pullNative(ctx, rail, amount)
		if err == nil && (pulled.IsNil() || pulled.BigInt().Cmp(converted) != 0) {
			return SwapResult{}, types.ErrUnsupportedInputTransfer
		}
	} else {
		ctx, pulled, err = k.pullTokens(ctx, rail, input, user, sdk.NewIntFromBigInt(amount))
	}
	if err != nil {
		return SwapResult{}, err
	}
	if pulled.IsNil() {
		return SwapResult{}, types.ErrZeroAmount
	}
	received := pulled.BigInt()
	if err := swapAmount(received, true); err != nil {
		return SwapResult{}, err
	}
	quote, err := k.Price(ctx, rail, input, output, pulled)
	if err != nil {
		return SwapResult{}, err
	}
	gross, net, feeBps, fee, impactBps, deduction := quote.Gross.BigInt(), quote.Net.BigInt(), quote.FeeBps,
		quote.FeeAmount.BigInt(), quote.ImpactBps, quote.Deduction.BigInt()
	if err := swapAmount(gross, false); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(net, false); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(fee, false); err != nil {
		return SwapResult{}, err
	}
	if err := swapAmount(deduction, false); err != nil {
		return SwapResult{}, err
	}
	if net.Cmp(minimum) < 0 || net.Cmp(gross) > 0 {
		return SwapResult{}, types.ErrInsufficientOutput
	}
	liquid, err := k.HasLiquidity(ctx, output, quote.Gross)
	if err != nil {
		return SwapResult{}, err
	}
	if !liquid {
		return SwapResult{}, types.ErrInsufficientLiquidity
	}
	priceOut, err := k.IndexPrice(ctx, output)
	if err != nil {
		return SwapResult{}, err
	}
	result := SwapResult{AmountIn: new(big.Int).Set(received), AmountOut: new(big.Int).Set(net), GrossOutput: new(big.Int).Set(gross), ExecutionPrice: priceOut.BigInt()}
	if fee.Sign() > 0 {
		priceIn, err := k.IndexPrice(ctx, input)
		if err != nil {
			return SwapResult{}, err
		}
		token, err := k.tokenOf(ctx, input)
		if err != nil {
			return SwapResult{}, err
		}
		volume, err := VolumeUSD(pulled, priceIn, token.Decimals)
		if err != nil {
			return SwapResult{}, err
		}
		result.Logs = append(result.Logs, swapLog(types.PECORAddress,
			"TieredFeeApplied(address,address,uint256,uint256,uint256)",
			[]common.Address{user, output}, volume.BigInt(), new(big.Int).SetUint64(feeBps), fee))
	}
	if impactBps > 0 {
		result.Logs = append(result.Logs, swapLog(types.PECORAddress,
			"PriceImpactApplied(address,address,uint256,uint256)",
			[]common.Address{user, output}, new(big.Int).SetUint64(impactBps), deduction))
	}
	if err := k.settleSwap(ctx, user, input, output, &result, nativeOutput); err != nil {
		return SwapResult{}, err
	}
	return result, nil
}

func (k *Keeper) settleSwap(ctx sdk.Context, user, input, output common.Address, result *SwapResult, nativeOutput bool) error {
	retained := new(big.Int).Sub(result.GrossOutput, result.AmountOut)
	if retained.Sign() > 0 {
		if err := k.AccrueFees(ctx, common.HexToAddress(types.PECORAddress), output, sdk.NewIntFromBigInt(retained)); err != nil {
			return err
		}
	}
	inputLog, err := k.swapBook(ctx, input, result.AmountIn)
	if err != nil {
		return err
	}
	outputLog, err := k.swapBook(ctx, output, new(big.Int).Neg(result.AmountOut))
	if err != nil {
		return err
	}
	if _, err := k.pushTokens(ctx, output, user, sdk.NewIntFromBigInt(result.AmountOut)); err != nil {
		if nativeOutput {
			return types.ErrNativeTransferFailed
		}
		return err
	}
	result.Logs = append(result.Logs, inputLog, outputLog)
	return nil
}

func (k *Keeper) swapBook(ctx sdk.Context, token common.Address, delta *big.Int) (*ethtypes.Log, error) {
	key := append(append([]byte(nil), types.BookPrefix...), token.Bytes()...)
	store := ctx.KVStore(k.storeKey)
	book := types.TokenBook{Token: token, Reserves: sdk.ZeroInt(), TotalDeposited: sdk.ZeroInt(), TotalWithdrawn: sdk.ZeroInt()}
	if encoded := store.Get(key); encoded != nil {
		if err := json.Unmarshal(encoded, &book); err != nil {
			return nil, fmt.Errorf("spot book: %w", err)
		}
	} else {
		params, err := k.tokenOf(ctx, token)
		if err != nil {
			return nil, err
		}
		if params.Decimals > types.MaxTokenDecimals {
			return nil, types.ErrUnsupportedToken
		}
		book.Decimals = uint8(params.Decimals)
	}
	if book.Token != token || book.Reserves.IsNil() || book.Reserves.IsNegative() {
		return nil, types.ErrInsufficientLiquidity
	}
	old := book.Reserves.BigInt()
	reserves := new(big.Int).Add(old, delta)
	if reserves.Sign() < 0 || reserves.BitLen() > 256 {
		return nil, types.ErrInsufficientLiquidity
	}
	book.Reserves = sdk.NewIntFromBigInt(reserves)
	encoded, err := json.Marshal(book)
	if err != nil {
		return nil, err
	}
	store.Set(key, encoded)
	return swapLog(types.PECORVaultAddress, "ReservesUpdated(address,uint256,uint256)", []common.Address{token}, old, reserves), nil
}

func swapLog(rail, signature string, indexed []common.Address, values ...*big.Int) *ethtypes.Log {
	topics := []common.Hash{crypto.Keccak256Hash([]byte(signature))}
	for _, address := range indexed {
		topics = append(topics, common.BytesToHash(address.Bytes()))
	}
	data := make([]byte, 32*len(values))
	for i, value := range values {
		value.FillBytes(data[i*32 : (i+1)*32])
	}
	return &ethtypes.Log{Address: common.HexToAddress(rail), Topics: topics, Data: data}
}
