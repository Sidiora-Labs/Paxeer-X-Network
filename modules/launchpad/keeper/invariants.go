package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	SolvencyInvariantName    = "launchpad-solvency"
	LiabilitiesInvariantName = "launchpad-liabilities"
	CustodyInvariantName     = "launchpad-custody"
)

// RegisterInvariants registers the launchpad invariants.
func RegisterInvariants(registry sdk.InvariantRegistry, k *Keeper) {
	registry.RegisterRoute(types.ModuleName, SolvencyInvariantName, SolvencyInvariant(k))
	registry.RegisterRoute(types.ModuleName, LiabilitiesInvariantName, LiabilitiesInvariant(k))
	registry.RegisterRoute(types.ModuleName, CustodyInvariantName, CustodyInvariant(k))
}

// SolvencyInvariant checks, for every market, that HeldQuote(pool) covers
// realUsdlBalance and token.balanceOf(pool) covers tokenReserve, the backing
// check SidioraPool runs after every swap.
func SolvencyInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		caller := k.ModuleCaller()
		message, broken := "", false
		k.IterateMarkets(ctx, func(market types.Market) bool {
			if market.PoolAddress == "" {
				broken = true
				message += fmt.Sprintf("market %s: no pool contract\n", market.Denom)
				return false
			}
			pool := common.HexToAddress(market.PoolAddress)
			held, err := k.GetQuoteLedger(ctx, HeldQuoteLedger(pool))
			switch {
			case err != nil:
				broken = true
				message += fmt.Sprintf("pool %s market %s: held quote unreadable: %v\n", pool.Hex(), market.Denom, err)
			case held.LT(market.RealQuoteBalance):
				broken = true
				message += fmt.Sprintf("pool %s market %s: held quote %s below real quote balance %s\n", pool.Hex(),
					market.Denom, held, market.RealQuoteBalance)
			}
			tokens, err := k.quoteBalance(ctx, caller, common.HexToAddress(market.TokenAddress), pool)
			switch {
			case err != nil:
				broken = true
				message += fmt.Sprintf("pool %s market %s: token balance unreadable: %v\n", pool.Hex(), market.Denom, err)
			case tokens.LT(market.TokenReserve):
				broken = true
				message += fmt.Sprintf("pool %s market %s: token balance %s below reserve %s\n", pool.Hex(),
					market.Denom, tokens, market.TokenReserve)
			}
			return false
		})
		return sdk.FormatInvariant(types.ModuleName, SolvencyInvariantName, message), broken
	}
}

// LiabilitiesInvariant checks that FeeHeld of the generation 2
// FeeAccumulator covers totalLiabilities, as FeeAccumulator._requireBacking
// requires, and that TreasuryHeld covers TreasuryLedger for every token.
func LiabilitiesInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		message, broken := "", false
		liabilities, err := k.TotalLiabilities(ctx)
		if err != nil {
			broken = true
			message += fmt.Sprintf("total liabilities unreadable: %v\n", err)
		}
		feeHeld, err := k.GetQuoteLedger(ctx, FeeHeldLedger(k.generation2.FeeAccumulator))
		switch {
		case err != nil:
			broken = true
			message += fmt.Sprintf("fee held unreadable: %v\n", err)
		case !liabilities.IsNil() && feeHeld.LT(liabilities):
			broken = true
			message += fmt.Sprintf("fee held %s below total liabilities %s\n", feeHeld, liabilities)
		}
		err = k.iterateLedgers(ctx, types.TreasuryLedgerPrefix, func(token common.Address, ledger sdk.Int) {
			held, err := k.getLedgerAmount(ctx, types.TreasuryHeldKey(token))
			switch {
			case err != nil:
				broken = true
				message += fmt.Sprintf("treasury %s: held unreadable: %v\n", token.Hex(), err)
			case held.LT(ledger):
				broken = true
				message += fmt.Sprintf("treasury %s: held %s below ledger %s\n", token.Hex(), held, ledger)
			}
		})
		if err != nil {
			broken = true
			message += fmt.Sprintf("treasury ledger unreadable: %v\n", err)
		}
		return sdk.FormatInvariant(types.ModuleName, LiabilitiesInvariantName, message), broken
	}
}

// CustodyInvariant checks that USDL.balanceOf(launchpad module EVM address),
// read by StaticCallEVM, covers the sum of every HeldQuote, every FeeHeld and
// TreasuryHeld(USDL).
func CustodyInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		token, custody := k.quoteToken(ctx), k.ModuleEVMAddress(ctx)
		refuse := func(format string, args ...interface{}) (string, bool) {
			return sdk.FormatInvariant(types.ModuleName, CustodyInvariantName, fmt.Sprintf(format, args...)), true
		}
		balance, err := k.quoteBalance(ctx, k.ModuleCaller(), token, custody)
		if err != nil {
			return refuse("custody balance unreadable: %v\n", err)
		}
		heldQuote, err := k.sumLedgers(ctx, types.HeldQuotePrefix)
		if err != nil {
			return refuse("held quote unreadable: %v\n", err)
		}
		feeHeld, err := k.sumLedgers(ctx, types.FeeHeldPrefix)
		if err != nil {
			return refuse("fee held unreadable: %v\n", err)
		}
		treasuryHeld, err := k.getLedgerAmount(ctx, types.TreasuryHeldKey(token))
		if err != nil {
			return refuse("treasury held unreadable: %v\n", err)
		}
		owed := new(big.Int).Add(heldQuote, feeHeld)
		owed.Add(owed, treasuryHeld.BigInt())
		if balance.BigInt().Cmp(owed) < 0 {
			return refuse("custody %s holds %s of %s, below ledgers %s (held quote %s, fee held %s, treasury held %s)\n",
				custody.Hex(), balance, token.Hex(), owed, heldQuote, feeHeld, treasuryHeld)
		}
		return sdk.FormatInvariant(types.ModuleName, CustodyInvariantName, ""), false
	}
}

// iterateLedgers calls cb with the address and amount of every custody
// ledger under prefix, whose keys are the prefix and a 20-byte address.
func (k *Keeper) iterateLedgers(ctx sdk.Context, prefix []byte, cb func(common.Address, sdk.Int)) error {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), prefix)
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(prefix)+common.AddressLength {
			return fmt.Errorf("%w: quote ledger key %x", types.ErrInvalidConfiguration, key)
		}
		amount, err := k.getLedgerAmount(ctx, key)
		if err != nil {
			return err
		}
		cb(common.BytesToAddress(key[len(prefix):]), amount)
	}
	return nil
}

// sumLedgers is the sum of every custody ledger under prefix, which may
// exceed a uint256.
func (k *Keeper) sumLedgers(ctx sdk.Context, prefix []byte) (*big.Int, error) {
	sum := new(big.Int)
	err := k.iterateLedgers(ctx, prefix, func(_ common.Address, amount sdk.Int) { sum.Add(sum, amount.BigInt()) })
	return sum, err
}
