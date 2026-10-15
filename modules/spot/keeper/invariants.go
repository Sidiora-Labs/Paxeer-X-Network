package keeper

import (
	"bytes"
	"fmt"
	"math/big"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	SolvencyInvariantName     = "spot-solvency"
	ShareBackingInvariantName = "spot-share-backing"
	EscrowInvariantName       = "spot-escrow"
	ConservationInvariantName = "spot-conservation"
)

// RegisterInvariants registers the spot invariants that hold over the
// store. Conservation holds per settlement, so it is built for each trade or
// fill by ConservationInvariant.
func RegisterInvariants(registry sdk.InvariantRegistry, k *Keeper) {
	registry.RegisterRoute(types.ModuleName, SolvencyInvariantName, SolvencyInvariant(k))
	registry.RegisterRoute(types.ModuleName, ShareBackingInvariantName, ShareBackingInvariant(k))
	registry.RegisterRoute(types.ModuleName, EscrowInvariantName, EscrowInvariant(k))
}

// invariantReport collects the violations of one invariant.
type invariantReport struct {
	message string
	broken  bool
}

func (r *invariantReport) fail(format string, args ...interface{}) {
	r.broken = true
	r.message += fmt.Sprintf(format, args...)
}

func (r *invariantReport) result(name string) (string, bool) {
	return sdk.FormatInvariant(types.ModuleName, name, r.message), r.broken
}

// SolvencyInvariant checks, for every registered token, that custody
// (balanceOf of the custody EVM address for an ERC20 token, the module bank
// balance for a NATIVE token) covers the book reserves plus the escrow, and
// that the book reserves cover the swap-rail and order-rail fee accruals.
func SolvencyInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		report := &invariantReport{}
		params, err := k.GetParams(ctx)
		if err != nil {
			report.fail("params unreadable: %v\n", err)
			return report.result(SolvencyInvariantName)
		}
		for _, token := range params.Tokens {
			address := token.Address.Hex()
			_, custody, err := k.CustodyBalance(ctx, token.Address)
			if err != nil {
				report.fail("token %s: custody unreadable: %v\n", address, err)
				continue
			}
			book, err := k.GetBook(ctx, token.Address)
			if err != nil {
				report.fail("token %s: book unreadable: %v\n", address, err)
				continue
			}
			escrow, err := k.GetEscrow(ctx, token.Address)
			if err != nil {
				report.fail("token %s: escrow unreadable: %v\n", address, err)
				continue
			}
			reserved, err := k.reservedOf(ctx, token.Address)
			if err != nil {
				report.fail("token %s: fee accruals unreadable: %v\n", address, err)
				continue
			}
			owed := new(big.Int).Add(book.Reserves.BigInt(), escrow.BigInt())
			if amountOrZero(custody).Cmp(owed) < 0 {
				report.fail("token %s: custody %s below book %s plus escrow %s\n", address, custody, book.Reserves,
					escrow)
			}
			if book.Reserves.BigInt().Cmp(reserved) < 0 {
				report.fail("token %s: book %s below fee accruals %s\n", address, book.Reserves, reserved)
			}
		}
		return report.result(SolvencyInvariantName)
	}
}

// ShareBackingInvariant checks, for every registered token, the bank supply
// of its share denom, the totalSupply its share pointer reads: zero while
// its pool never took a deposit, and at least MINIMUM_SHARES, held by the
// spot module account, after the first.
func ShareBackingInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		report := &invariantReport{}
		params, err := k.GetParams(ctx)
		if err != nil {
			report.fail("params unreadable: %v\n", err)
			return report.result(ShareBackingInvariantName)
		}
		minimum := big.NewInt(MinimumShares)
		for _, token := range params.Tokens {
			address := token.Address.Hex()
			book, found, err := k.tokenBook(ctx, token.Address)
			if err != nil {
				report.fail("token %s: book unreadable: %v\n", address, err)
				continue
			}
			deposited := found && book.MinimumSharesLocked
			supply := k.shareSupply(ctx, token.Address)
			switch {
			case !deposited && supply.Sign() != 0:
				report.fail("token %s: share supply %s without a deposit\n", address, supply)
			case deposited && supply.Cmp(minimum) < 0:
				report.fail("token %s: share supply %s below MINIMUM_SHARES %d after the first deposit\n", address,
					supply, MinimumShares)
			}
			if !deposited {
				continue
			}
			if held := k.bankBalance(ctx, CustodyAddress(), k.ShareDenom(token.Address)); amountOrZero(held).Cmp(minimum) < 0 {
				report.fail("token %s: spot module account holds %s shares, below MINIMUM_SHARES %d\n", address, held,
					MinimumShares)
			}
		}
		return report.result(ShareBackingInvariantName)
	}
}

// activeOrder is the part of an order record of any kind the escrow
// invariant checks.
type activeOrder struct {
	id     uint64
	user   common.Address
	escrow common.Address
	amount sdk.Int
	status types.OrderStatus
	active bool
}

// EscrowInvariant checks that every entry of the three active indexes has a
// record of its kind that is PENDING (or ACTIVATED for a stop-limit), that
// the escrow of every token equals the sum of the amounts of the active
// orders it backs, and that the active count of every user equals the number
// of their active orders.
func EscrowInvariant(k *Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		report := &invariantReport{}
		store := k.store(ctx)
		sums := map[common.Address]*big.Int{}
		counts := map[common.Address]uint64{}
		for _, index := range []struct {
			name   string
			prefix []byte
			read   func(uint64) (activeOrder, error)
		}{
			{"limit", types.ActiveLimitPrefix, func(id uint64) (activeOrder, error) {
				o, err := k.GetLimitOrder(ctx, id)
				return activeOrder{o.ID, o.User, o.EscrowToken(), o.Amount, o.Status, o.IsActive()}, err
			}},
			{"stop-limit", types.ActiveStopLimitPrefix, func(id uint64) (activeOrder, error) {
				o, err := k.GetStopLimitOrder(ctx, id)
				return activeOrder{o.ID, o.User, o.EscrowToken(), o.Amount, o.Status, o.IsActive()}, err
			}},
			{"stop-loss", types.ActiveStopLossPrefix, func(id uint64) (activeOrder, error) {
				o, err := k.GetStopLossOrder(ctx, id)
				return activeOrder{o.ID, o.User, o.EscrowToken(), o.Amount, o.Status, o.IsActive()}, err
			}},
		} {
			ids, err := exportIDs(store, index.prefix)
			if err != nil {
				report.fail("active %s index unreadable: %v\n", index.name, err)
				continue
			}
			for _, id := range ids {
				order, err := index.read(id)
				switch {
				case err != nil:
					report.fail("active %s index entry %d: record unreadable: %v\n", index.name, id, err)
				case order.id != id:
					report.fail("active %s index entry %d has no %s record\n", index.name, id, index.name)
				case !order.active:
					report.fail("active %s index entry %d has status %d\n", index.name, id, order.status)
				default:
					if sums[order.escrow] == nil {
						sums[order.escrow] = new(big.Int)
					}
					sums[order.escrow].Add(sums[order.escrow], amountOrZero(order.amount))
					counts[order.user]++
				}
			}
		}

		escrows, err := exportRecords[types.TokenEscrow](store, types.EscrowPrefix)
		if err != nil {
			report.fail("escrow unreadable: %v\n", err)
		}
		escrowed := map[common.Address]bool{}
		for _, entry := range escrows {
			sum := sums[entry.Token]
			if sum == nil {
				sum = new(big.Int)
			}
			if entry.Amount.IsNil() || entry.Amount.BigInt().Cmp(sum) != 0 {
				report.fail("escrow of %s is %s, its active orders sum to %s\n", entry.Token.Hex(), entry.Amount, sum)
			}
			escrowed[entry.Token] = true
		}
		for _, token := range sortedAddresses(sums) {
			if !escrowed[token] {
				report.fail("escrow of %s is 0, its active orders sum to %s\n", token.Hex(), sums[token])
			}
		}

		recorded, err := exportRecords[types.ActiveOrderCount](store, types.ActiveCountPrefix)
		if err != nil {
			report.fail("active counts unreadable: %v\n", err)
		}
		counted := map[common.Address]bool{}
		for _, entry := range recorded {
			if entry.Count != counts[entry.User] {
				report.fail("active count of %s is %d, it has %d active orders\n", entry.User.Hex(), entry.Count,
					counts[entry.User])
			}
			counted[entry.User] = true
		}
		for _, user := range sortedAddresses(counts) {
			if !counted[user] {
				report.fail("active count of %s is 0, it has %d active orders\n", user.Hex(), counts[user])
			}
		}
		return report.result(EscrowInvariantName)
	}
}

func sortedAddresses[V any](values map[common.Address]V) []common.Address {
	addresses := make([]common.Address, 0, len(values))
	for address := range values {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return bytes.Compare(addresses[i][:], addresses[j][:]) < 0 })
	return addresses
}

// Settlement is one settled trade or fill as the books saw it: the pulled
// input, the gross and net output, and the book reserves of both tokens
// before and after it.
type Settlement struct {
	TokenIn   common.Address
	TokenOut  common.Address
	AmountIn  sdk.Int
	Gross     sdk.Int
	Net       sdk.Int
	InBefore  sdk.Int
	InAfter   sdk.Int
	OutBefore sdk.Int
	OutAfter  sdk.Int
}

// ConservationInvariant checks one settlement at the index prices of the
// context it runs on: the input book rose by amountIn, the output book fell
// by the net output, the net output is at most gross, and gross is the
// rounded-down conversion of amountIn, so the USD value of amountIn equals
// the USD value of gross within the rounding of gross:
// gross x priceOut x 10^decIn <= amountIn x priceIn x 10^decOut <
// (gross + 1) x priceOut x 10^decIn.
func ConservationInvariant(k *Keeper, s Settlement) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		report := &invariantReport{}
		for _, amount := range []sdk.Int{s.AmountIn, s.Gross, s.Net, s.InBefore, s.InAfter, s.OutBefore, s.OutAfter} {
			if !pricingUint256(amount) {
				report.fail("settlement amounts must be uint256 integers\n")
				return report.result(ConservationInvariantName)
			}
		}
		if s.TokenIn == s.TokenOut {
			report.fail("settlement trades %s against itself\n", s.TokenIn.Hex())
			return report.result(ConservationInvariantName)
		}
		in, out := s.TokenIn.Hex(), s.TokenOut.Hex()
		if rise := new(big.Int).Sub(s.InAfter.BigInt(), s.InBefore.BigInt()); rise.Cmp(s.AmountIn.BigInt()) != 0 {
			report.fail("input book of %s moved by %s, amountIn %s\n", in, rise, s.AmountIn)
		}
		if fall := new(big.Int).Sub(s.OutBefore.BigInt(), s.OutAfter.BigInt()); fall.Cmp(s.Net.BigInt()) != 0 {
			report.fail("output book of %s fell by %s, net output %s\n", out, fall, s.Net)
		}
		if s.Net.GT(s.Gross) {
			report.fail("net output %s above gross %s\n", s.Net, s.Gross)
		}
		tokenIn, err := k.registered(ctx, s.TokenIn)
		if err != nil {
			report.fail("token %s: %v\n", in, err)
			return report.result(ConservationInvariantName)
		}
		tokenOut, err := k.registered(ctx, s.TokenOut)
		if err != nil {
			report.fail("token %s: %v\n", out, err)
			return report.result(ConservationInvariantName)
		}
		priceIn, err := k.IndexPrice(ctx, s.TokenIn)
		if err != nil {
			report.fail("token %s: index price unreadable: %v\n", in, err)
			return report.result(ConservationInvariantName)
		}
		priceOut, err := k.IndexPrice(ctx, s.TokenOut)
		if err != nil {
			report.fail("token %s: index price unreadable: %v\n", out, err)
			return report.result(ConservationInvariantName)
		}
		value := new(big.Int).Mul(s.AmountIn.BigInt(), priceIn.BigInt())
		value.Mul(value, pricingScale(tokenOut.Decimals))
		unit := new(big.Int).Mul(priceOut.BigInt(), pricingScale(tokenIn.Decimals))
		low := new(big.Int).Mul(s.Gross.BigInt(), unit)
		high := new(big.Int).Add(low, unit)
		if value.Cmp(low) < 0 || value.Cmp(high) >= 0 {
			valueIn, errIn := VolumeUSD(s.AmountIn, priceIn, tokenIn.Decimals)
			valueOut, errOut := VolumeUSD(s.Gross, priceOut, tokenOut.Decimals)
			if errIn != nil || errOut != nil {
				report.fail("amountIn %s of %s at %s and gross %s of %s at %s differ beyond the rounding of gross\n",
					s.AmountIn, in, priceIn, s.Gross, out, priceOut)
			} else {
				report.fail("USD value %s of amountIn %s of %s differs from USD value %s of gross %s of %s beyond the rounding of gross\n",
					valueIn, s.AmountIn, in, valueOut, s.Gross, out)
			}
		}
		return report.result(ConservationInvariantName)
	}
}
