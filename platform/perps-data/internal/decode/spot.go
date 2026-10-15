package decode

import (
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
)

// The spot rails of contracts/spot.md. PECORQuoter 0x1024 is view-only and
// emits no log of its own; only the fee events are matched there.
var (
	SpotPoolAddress   = common.HexToAddress("0x0000000000000000000000000000000000001021")
	SpotOrdersAddress = common.HexToAddress("0x0000000000000000000000000000000000001022")
	SpotStopAddress   = common.HexToAddress("0x0000000000000000000000000000000000001023")
	SpotQuoterAddress = common.HexToAddress("0x0000000000000000000000000000000000001024")
	SpotVaultAddress  = common.HexToAddress("0x0000000000000000000000000000000000001025")
)

// Spot order types of IPECOROrders.OrderType that the created events carry.
const (
	SpotLimitBuy      uint8 = 0
	SpotLimitSell     uint8 = 1
	SpotStopLimitBuy  uint8 = 3
	SpotStopLimitSell uint8 = 4
)

// orderRail is one order view: getLimitOrder(uint256) on 0x1022,
// getStopLimitOrder(uint256) and getStopLossOrder(uint256) on 0x1023, each
// returning a static tuple of words fields led by (id, user, stablecoin,
// token).
type orderRail struct {
	name     string
	address  common.Address
	selector []byte
	words    int
}

var (
	limitRail     = orderRail{"getLimitOrder", SpotOrdersAddress, common.FromHex("0x53061026"), 10}
	stopLimitRail = orderRail{"getStopLimitOrder", SpotStopAddress, common.FromHex("0x307ff324"), 11}
	stopLossRail  = orderRail{"getStopLossOrder", SpotStopAddress, common.FromHex("0x4c34cf02"), 9}

	getRegisteredTokensSelector = common.FromHex("0x14cbd993")
	shareTokensSelector         = common.FromHex("0x96153f07")
)

const shareTokenQuery = `SELECT token FROM perps.spot_token_registry WHERE kind = 'share_token_created' AND share_token = $1`

type spotOrderKey struct {
	rail common.Address
	id   string
}

type spotOrder struct {
	user, stablecoin, token common.Address
}

func registerSpot(r *Registry) error {
	rails := []struct {
		name     string
		address  common.Address
		load     func() (abi.ABI, error)
		decoders []eventDecoder
	}{
		{"PECOR", SpotPoolAddress, spotabi.PECOR, pecorEvents},
		{"PECOROrders", SpotOrdersAddress, spotabi.Orders, ordersEvents},
		{"PECORStopOrders", SpotStopAddress, spotabi.StopOrders, stopOrdersEvents},
		{"PECORVault", SpotVaultAddress, spotabi.Vault, vaultEvents},
	}
	var pecor abi.ABI
	for _, rail := range rails {
		a, err := rail.load()
		if err != nil {
			return fmt.Errorf("decode: %s ABI: %w", rail.name, err)
		}
		if rail.address == SpotPoolAddress {
			pecor = a
		}
		if err := r.register(rail.address, r.opts.SpotModule, a, rail.decoders); err != nil {
			return err
		}
	}
	r.modules[SpotQuoterAddress] = r.opts.SpotModule
	for _, d := range feeEvents {
		ev := pecor.Events[d.name]
		for _, address := range []common.Address{SpotOrdersAddress, SpotStopAddress, SpotQuoterAddress, SpotVaultAddress} {
			k := key{address, ev.ID}
			if _, dup := r.entries[k]; dup {
				return fmt.Errorf("decode: duplicate decoder %s at %s", d.name, address)
			}
			r.entries[k] = entry{event: ev, build: d.build}
		}
	}
	share, err := spotabi.Share()
	if err != nil {
		return fmt.Errorf("decode: share ABI: %w", err)
	}
	if len(share.Events) != len(shareEvents) {
		return fmt.Errorf("decode: share pointers declare %d events, %d decoders", len(share.Events), len(shareEvents))
	}
	for _, d := range shareEvents {
		ev, ok := share.Events[d.name]
		if !ok {
			return fmt.Errorf("decode: share pointers declare no event %s", d.name)
		}
		r.share[ev.ID] = entry{event: ev, build: d.build}
	}
	return nil
}

// orderTokenLookup resolves the user, stablecoin and token of order id on rail by
// eth_call at the event height, at the latest height when every node pruned
// it. An order the rail does not know is an order_lookup failure.
func (b *batch) orderTokenLookup(r *reader, rail orderRail, id string, height uint64) spotOrder {
	if r.err != nil {
		return spotOrder{}
	}
	k := spotOrderKey{rail.address, id}
	if o, ok := b.spotOrders[k]; ok {
		return o
	}
	state := b.reg.opts.State
	if state == nil {
		r.fail(fmt.Errorf("%s(%s): %w", rail.name, id, ErrNoState))
		return spotOrder{}
	}
	n, ok := new(big.Int).SetString(id, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 {
		r.fail(fmt.Errorf("%s: order id %q", rail.name, id))
		return spotOrder{}
	}
	data := append(append([]byte(nil), rail.selector...), common.LeftPadBytes(n.Bytes(), 32)...)
	out, err := state.CallAt(b.ctx, rail.address, data, height)
	if errors.Is(err, ErrStatePruned) {
		out, err = state.Call(b.ctx, rail.address, data)
	}
	if err != nil {
		r.fail(fmt.Errorf("%s(%s): %w", rail.name, id, err))
		return spotOrder{}
	}
	if len(out) != 32*rail.words {
		r.fail(fmt.Errorf("%s(%s) returned %d bytes, want %d", rail.name, id, len(out), 32*rail.words))
		return spotOrder{}
	}
	var words [3]common.Address
	for i := range words {
		w := out[32*(i+1) : 32*(i+2)]
		if new(big.Int).SetBytes(w[:12]).Sign() != 0 {
			r.fail(fmt.Errorf("%s(%s) word %d is not an address", rail.name, id, i+1))
			return spotOrder{}
		}
		words[i] = common.BytesToAddress(w[12:])
	}
	got := new(big.Int).SetBytes(out[:32])
	if got.Sign() == 0 || words[0] == (common.Address{}) {
		r.fail(&lookupFailure{FailureOrderLookup, fmt.Sprintf("%s(%s) returned a zero record", rail.name, id)})
		return spotOrder{}
	}
	if got.Cmp(n) != 0 {
		r.fail(fmt.Errorf("%s(%s) returned order %s", rail.name, id, got))
		return spotOrder{}
	}
	o := spotOrder{user: words[0], stablecoin: words[1], token: words[2]}
	b.spotOrders[k] = o
	return o
}

// pointerToken resolves the token of a share pointer: a ShareTokenCreated
// earlier in the batch, the pointers the registry resolved before, the stored
// spot_token_registry row, then getRegisteredTokens and shareTokens on 0x1025.
func (b *batch) pointerToken(pointer common.Address) (common.Address, bool, error) {
	if t, ok := b.created[pointer]; ok {
		return t, true, nil
	}
	reg := b.reg
	reg.mu.Lock()
	t, ok := reg.pointers[pointer]
	reg.mu.Unlock()
	if ok {
		return t, true, nil
	}
	if b.q != nil {
		var raw []byte
		err := b.q.QueryRowContext(b.ctx, shareTokenQuery, pointer.Bytes()).Scan(&raw)
		switch {
		case err == nil && len(raw) == common.AddressLength:
			t := common.BytesToAddress(raw)
			b.created[pointer] = t
			return t, true, nil
		case err == nil:
			return common.Address{}, false, fmt.Errorf("stored share token has %d bytes", len(raw))
		case !errors.Is(err, sql.ErrNoRows):
			return common.Address{}, false, fmt.Errorf("read share token: %w", err)
		}
	}
	if reg.opts.State == nil {
		return common.Address{}, false, nil
	}
	found, err := reg.scanPointers(b)
	if err != nil {
		return common.Address{}, false, err
	}
	t, ok = found[pointer]
	return t, ok, nil
}

// scanPointers reads every registered token's share pointer at the latest
// height into the registry cache.
func (r *Registry) scanPointers(b *batch) (map[common.Address]common.Address, error) {
	state := r.opts.State
	out, err := state.Call(b.ctx, SpotVaultAddress, getRegisteredTokensSelector)
	if err != nil {
		return nil, fmt.Errorf("getRegisteredTokens: %w", err)
	}
	list, err := abi.NewType("address[]", "", nil)
	if err != nil {
		return nil, err
	}
	values, err := abi.Arguments{{Type: list}}.Unpack(out)
	if err != nil {
		return nil, fmt.Errorf("getRegisteredTokens: %w", err)
	}
	tokens, ok := values[0].([]common.Address)
	if !ok {
		return nil, errors.New("getRegisteredTokens: not an address list")
	}
	found := make(map[common.Address]common.Address, len(tokens))
	for _, token := range tokens {
		word, err := state.Call(b.ctx, SpotVaultAddress, append(append([]byte(nil), shareTokensSelector...), common.LeftPadBytes(token.Bytes(), 32)...))
		if err != nil {
			return nil, fmt.Errorf("shareTokens(%s): %w", token.Hex(), err)
		}
		if len(word) != 32 || new(big.Int).SetBytes(word[:12]).Sign() != 0 {
			return nil, fmt.Errorf("shareTokens(%s) returned no address word", token.Hex())
		}
		if pointer := common.BytesToAddress(word[12:]); pointer != (common.Address{}) {
			found[pointer] = token
		}
	}
	r.mu.Lock()
	for p, t := range found {
		r.pointers[p] = t
	}
	r.mu.Unlock()
	return found, nil
}

func (b *batch) limitType(v uint8) uint8 {
	if v != SpotLimitBuy && v != SpotLimitSell {
		b.enumUnknown++
	}
	return v
}

func (b *batch) stopLimitType(v uint8) uint8 {
	if v != SpotStopLimitBuy && v != SpotStopLimitSell {
		b.enumUnknown++
	}
	return v
}

func ownershipTransferred(_ *batch, f Fact, r *reader) []Row {
	f.Kind = "ownership_transferred"
	return []Row{SpotOwnershipRow{f, r.addr("previousOwner"), r.addr("newOwner")}}
}

func paused(_ *batch, f Fact, r *reader) []Row {
	f.Kind = "paused"
	return []Row{SpotPauseRow{f, r.addr("account")}}
}

func unpaused(_ *batch, f Fact, r *reader) []Row {
	f.Kind = "unpaused"
	return []Row{SpotPauseRow{f, r.addr("account")}}
}

// feeEvents are matched by topic0 at any of 0x1021 to 0x1025.
var feeEvents = []eventDecoder{
	{"PriceImpactApplied", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "price_impact"
		f.Account = r.addr("user")
		return []Row{SpotPriceImpactRow{f, f.Account, r.addr("tokenOut"), r.u256("impactBps"), r.u256("deductedAmount")}}
	}},
	{"TieredFeeApplied", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "tiered_fee"
		f.Account = r.addr("user")
		return []Row{SpotTieredFeeRow{f, f.Account, r.addr("tokenOut"), r.u256("volumeUSD"), r.u256("feeBps"), r.u256("feeAmount")}}
	}},
}

// pecorEvents is the decoding table of contracts/data-services.md section 6
// for PECOR 0x1021.
var pecorEvents = append([]eventDecoder{
	{"MarketOrderExecuted", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "market_order"
		f.Account = r.addr("user")
		return []Row{SpotMarketOrderRow{f, f.Account, r.addr("stablecoin"), r.addr("token"), r.u256("stablecoinAmount"), r.u256("tokenAmount"), r.flag("isBuy")}}
	}},
	{"NativeSwap", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "native_swap"
		f.Account = r.addr("user")
		return []Row{SpotNativeSwapRow{f, f.Account, r.addr("tokenOther"), r.u256("nativeAmount"), r.u256("tokenAmount"), r.flag("nativeIsInput")}}
	}},
	{"SimpleSwap", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "simple_swap"
		f.Account = r.addr("user")
		return []Row{SpotSimpleSwapRow{f, f.Account, r.addr("tokenIn"), r.addr("tokenOut"), r.u256("amountIn"), r.u256("amountOut"), r.u256("executionPrice")}}
	}},
	{"TwapSanityUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "twap_sanity_updated"
		return []Row{SpotTwapSanityRow{f, r.flag("enabled"), r.u256("period"), r.u256("maxDeviationBps")}}
	}},
	{"OwnershipTransferred", ownershipTransferred},
	{"Paused", paused},
	{"Unpaused", unpaused},
}, feeEvents...)

// orderEvent builds the id-only order events of a rail: the user, token and
// stablecoin come from the rail's order view.
func orderEvent(rail orderRail, kind string, build func(Fact, *reader, string, spotOrder) Row) func(*batch, Fact, *reader) []Row {
	return func(b *batch, f Fact, r *reader) []Row {
		f.Kind = kind
		id := r.u256("orderId")
		o := b.orderTokenLookup(r, rail, id, f.Block)
		f.Account = o.user
		return []Row{build(f, r, id, o)}
	}
}

func orderID(f Fact, _ *reader, id string, o spotOrder) Row {
	return SpotOrderIDRow{f, id, o.token, o.stablecoin}
}

func orderExecuted(f Fact, r *reader, id string, o spotOrder) Row {
	return SpotOrderExecutedRow{f, id, o.token, o.stablecoin, r.u256("executionPrice")}
}

// ordersEvents is the decoding table for PECOROrders 0x1022.
var ordersEvents = []eventDecoder{
	{"LimitOrderCreated", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "limit_created"
		id := r.u256("orderId")
		f.Account = r.addr("user")
		o := b.orderTokenLookup(r, limitRail, id, f.Block)
		return []Row{SpotLimitCreatedRow{f, id, f.Account, b.limitType(r.u8("orderType")), r.u256("amount"), r.u256("targetPrice"), o.token, o.stablecoin}}
	}},
	{"LimitOrderCancelled", orderEvent(limitRail, "limit_cancelled", orderID)},
	{"LimitOrderExecuted", orderEvent(limitRail, "limit_executed", orderExecuted)},
	{"LimitOrderExpired", orderEvent(limitRail, "limit_expired", orderID)},
	{"LimitOrderModified", orderEvent(limitRail, "limit_modified", func(f Fact, r *reader, id string, o spotOrder) Row {
		return SpotLimitModifiedRow{f, id, o.token, o.stablecoin, r.u256("targetPrice"), r.u256("expiresAt")}
	})},
	{"OwnershipTransferred", ownershipTransferred},
	{"Paused", paused},
	{"Unpaused", unpaused},
}

// stopOrdersEvents is the decoding table for PECORStopOrders 0x1023.
var stopOrdersEvents = []eventDecoder{
	{"StopLimitOrderCreated", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "stop_limit_created"
		id := r.u256("orderId")
		f.Account = r.addr("user")
		o := b.orderTokenLookup(r, stopLimitRail, id, f.Block)
		return []Row{SpotStopLimitCreatedRow{f, id, f.Account, b.stopLimitType(r.u8("orderType")), r.u256("amount"), r.u256("stopPrice"), r.u256("limitPrice"), o.token, o.stablecoin}}
	}},
	{"StopLimitActivated", orderEvent(stopLimitRail, "stop_limit_activated", func(f Fact, r *reader, id string, o spotOrder) Row {
		return SpotStopLimitActivatedRow{f, id, o.token, o.stablecoin, r.u256("activationPrice")}
	})},
	{"StopLimitCancelled", orderEvent(stopLimitRail, "stop_limit_cancelled", orderID)},
	{"StopLimitExecuted", orderEvent(stopLimitRail, "stop_limit_executed", orderExecuted)},
	{"StopLossOrderCreated", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "stop_loss_created"
		id := r.u256("orderId")
		f.Account = r.addr("user")
		o := b.orderTokenLookup(r, stopLossRail, id, f.Block)
		return []Row{SpotStopLossCreatedRow{f, id, f.Account, r.u256("amount"), r.u256("triggerPrice"), o.token, o.stablecoin}}
	}},
	{"StopLossOrderCancelled", orderEvent(stopLossRail, "stop_loss_cancelled", orderID)},
	{"StopLossOrderExecuted", orderEvent(stopLossRail, "stop_loss_executed", orderExecuted)},
	{"OwnershipTransferred", ownershipTransferred},
	{"Paused", paused},
	{"Unpaused", unpaused},
}

// vaultEvents is the decoding table for PECORVault 0x1025.
var vaultEvents = []eventDecoder{
	{"Deposit", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "deposit"
		f.Account = r.addr("depositor")
		return []Row{SpotDepositRow{f, r.addr("token"), f.Account, r.u256("amount"), r.u256("newReserve")}}
	}},
	{"LPWithdrawal", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "lp_withdrawal"
		f.Account = r.addr("provider")
		return []Row{SpotLPWithdrawalRow{f, r.addr("token"), f.Account, r.u256("shares"), r.u256("amount")}}
	}},
	{"NativeDeposit", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "native_deposit"
		f.Account = r.addr("depositor")
		return []Row{SpotNativeDepositRow{f, f.Account, r.u256("amount"), r.u256("newReserve")}}
	}},
	{"NativeWithdrawal", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "native_withdrawal"
		f.Account = r.addr("recipient")
		return []Row{SpotNativeWithdrawalRow{f, f.Account, r.u256("amount"), r.u256("newReserve")}}
	}},
	{"Withdrawal", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "withdrawal"
		f.Account = r.addr("recipient")
		return []Row{SpotWithdrawalRow{f, r.addr("token"), f.Account, r.u256("amount"), r.u256("newReserve")}}
	}},
	{"ReservesSync", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "reserves_sync"
		return []Row{SpotReservesSyncRow{f, r.addr("token"), r.u256("oldReserves"), r.u256("newReserves"), r.u256("recovered")}}
	}},
	{"ReservesUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "reserves_updated"
		return []Row{SpotReservesUpdatedRow{f, r.addr("token"), r.u256("oldReserve"), r.u256("newReserve")}}
	}},
	{"TokenRegistered", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "token_registered"
		return []Row{SpotTokenRegisteredRow{f, r.addr("token"), r.u8("decimals"), r.flag("isStablecoin")}}
	}},
	{"ShareTokenCreated", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "share_token_created"
		token, pointer := r.addr("token"), r.addr("shareToken")
		if r.err == nil {
			b.created[pointer] = token
		}
		return []Row{SpotShareTokenCreatedRow{f, token, pointer}}
	}},
	{"OperatorUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "operator_updated"
		return []Row{SpotOperatorRow{f, r.addr("operator"), r.flag("authorized")}}
	}},
	{"TokenPauseUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "token_pause_updated"
		return []Row{SpotTokenPauseRow{f, r.addr("token"), r.flag("paused")}}
	}},
	{"TransactionTrackerUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "tracker_updated"
		return []Row{SpotTrackerRow{f, r.addr("tracker")}}
	}},
	{"UtilizationCapUpdated", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "utilization_cap_updated"
		return []Row{SpotUtilizationCapRow{f, r.addr("token"), r.u256("maxUtilizationBps")}}
	}},
	{"OwnershipTransferred", ownershipTransferred},
}

// shareEvents decode the logs of the share pointers; batch.shareToken holds
// the token of the pointer being decoded.
var shareEvents = []eventDecoder{
	{"Transfer", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "transfer"
		from, to := r.addr("from"), r.addr("to")
		f.Account = from
		if from == (common.Address{}) {
			f.Account = to
		}
		return []Row{SpotShareTransferRow{f, f.Emitter, b.shareToken, from, to, r.u256("value")}}
	}},
	{"Approval", func(b *batch, f Fact, r *reader) []Row {
		f.Kind = "approval"
		f.Account = r.addr("owner")
		return []Row{SpotShareApprovalRow{f, f.Emitter, b.shareToken, f.Account, r.addr("spender"), r.u256("value")}}
	}},
	{"OwnershipTransferred", ownershipTransferred},
}

// SpotMarketOrderRow is a perps.spot_swaps row of kind market_order.
type SpotMarketOrderRow struct {
	Fact
	User             common.Address
	Stablecoin       common.Address
	Token            common.Address
	StablecoinAmount string
	TokenAmount      string
	IsBuy            bool
}

// SpotNativeSwapRow is a perps.spot_swaps row of kind native_swap.
type SpotNativeSwapRow struct {
	Fact
	User          common.Address
	TokenOther    common.Address
	NativeAmount  string
	TokenAmount   string
	NativeIsInput bool
}

// SpotSimpleSwapRow is a perps.spot_swaps row of kind simple_swap.
type SpotSimpleSwapRow struct {
	Fact
	User           common.Address
	TokenIn        common.Address
	TokenOut       common.Address
	AmountIn       string
	AmountOut      string
	ExecutionPrice string
}

// SpotPriceImpactRow is a perps.spot_fee_events row of kind price_impact.
type SpotPriceImpactRow struct {
	Fact
	User           common.Address
	TokenOut       common.Address
	ImpactBps      string
	DeductedAmount string
}

// SpotTieredFeeRow is a perps.spot_fee_events row of kind tiered_fee.
type SpotTieredFeeRow struct {
	Fact
	User      common.Address
	TokenOut  common.Address
	VolumeUSD string
	FeeBps    string
	FeeAmount string
}

// SpotLimitCreatedRow is a perps.spot_order_events row of kind limit_created.
type SpotLimitCreatedRow struct {
	Fact
	OrderID     string
	User        common.Address
	OrderType   uint8
	Amount      string
	TargetPrice string
	Token       common.Address
	Stablecoin  common.Address
}

// SpotStopLimitCreatedRow is a perps.spot_order_events row of kind
// stop_limit_created.
type SpotStopLimitCreatedRow struct {
	Fact
	OrderID    string
	User       common.Address
	OrderType  uint8
	Amount     string
	StopPrice  string
	LimitPrice string
	Token      common.Address
	Stablecoin common.Address
}

// SpotStopLossCreatedRow is a perps.spot_order_events row of kind
// stop_loss_created.
type SpotStopLossCreatedRow struct {
	Fact
	OrderID      string
	User         common.Address
	Amount       string
	TriggerPrice string
	Token        common.Address
	Stablecoin   common.Address
}

// SpotOrderIDRow is a perps.spot_order_events row of an id-only event:
// limit_cancelled, limit_expired, stop_limit_cancelled or stop_loss_cancelled.
type SpotOrderIDRow struct {
	Fact
	OrderID    string
	Token      common.Address
	Stablecoin common.Address
}

// SpotOrderExecutedRow is a perps.spot_order_events row of kind
// limit_executed, stop_limit_executed or stop_loss_executed.
type SpotOrderExecutedRow struct {
	Fact
	OrderID        string
	Token          common.Address
	Stablecoin     common.Address
	ExecutionPrice string
}

// SpotLimitModifiedRow is a perps.spot_order_events row of kind
// limit_modified.
type SpotLimitModifiedRow struct {
	Fact
	OrderID     string
	Token       common.Address
	Stablecoin  common.Address
	TargetPrice string
	ExpiresAt   string
}

// SpotStopLimitActivatedRow is a perps.spot_order_events row of kind
// stop_limit_activated.
type SpotStopLimitActivatedRow struct {
	Fact
	OrderID         string
	Token           common.Address
	Stablecoin      common.Address
	ActivationPrice string
}

// SpotDepositRow is a perps.spot_vault_events row of kind deposit.
type SpotDepositRow struct {
	Fact
	Token      common.Address
	Depositor  common.Address
	Amount     string
	NewReserve string
}

// SpotLPWithdrawalRow is a perps.spot_vault_events row of kind lp_withdrawal.
type SpotLPWithdrawalRow struct {
	Fact
	Token    common.Address
	Provider common.Address
	Shares   string
	Amount   string
}

// SpotNativeDepositRow is a perps.spot_vault_events row of kind
// native_deposit.
type SpotNativeDepositRow struct {
	Fact
	Depositor  common.Address
	Amount     string
	NewReserve string
}

// SpotNativeWithdrawalRow is a perps.spot_vault_events row of kind
// native_withdrawal.
type SpotNativeWithdrawalRow struct {
	Fact
	Recipient  common.Address
	Amount     string
	NewReserve string
}

// SpotWithdrawalRow is a perps.spot_vault_events row of kind withdrawal.
type SpotWithdrawalRow struct {
	Fact
	Token      common.Address
	Recipient  common.Address
	Amount     string
	NewReserve string
}

// SpotReservesSyncRow is a perps.spot_vault_events row of kind reserves_sync.
type SpotReservesSyncRow struct {
	Fact
	Token       common.Address
	OldReserves string
	NewReserves string
	Recovered   string
}

// SpotReservesUpdatedRow is a perps.spot_vault_events row of kind
// reserves_updated.
type SpotReservesUpdatedRow struct {
	Fact
	Token      common.Address
	OldReserve string
	NewReserve string
}

// SpotTokenRegisteredRow is a perps.spot_token_registry row of kind
// token_registered.
type SpotTokenRegisteredRow struct {
	Fact
	Token        common.Address
	Decimals     uint8
	IsStablecoin bool
}

// SpotShareTokenCreatedRow is a perps.spot_token_registry row of kind
// share_token_created.
type SpotShareTokenCreatedRow struct {
	Fact
	Token      common.Address
	ShareToken common.Address
}

// SpotOwnershipRow is a perps.spot_admin row of kind ownership_transferred.
type SpotOwnershipRow struct {
	Fact
	PreviousHolder common.Address
	NewHolder      common.Address
}

// SpotPauseRow is a perps.spot_admin row of kind paused or unpaused.
type SpotPauseRow struct {
	Fact
	AccountArg common.Address
}

// SpotTwapSanityRow is a perps.spot_admin row of kind twap_sanity_updated.
type SpotTwapSanityRow struct {
	Fact
	Enabled         bool
	Period          string
	MaxDeviationBps string
}

// SpotOperatorRow is a perps.spot_admin row of kind operator_updated.
type SpotOperatorRow struct {
	Fact
	Operator   common.Address
	Authorized bool
}

// SpotTokenPauseRow is a perps.spot_admin row of kind token_pause_updated.
type SpotTokenPauseRow struct {
	Fact
	Token  common.Address
	Paused bool
}

// SpotTrackerRow is a perps.spot_admin row of kind tracker_updated.
type SpotTrackerRow struct {
	Fact
	Tracker common.Address
}

// SpotUtilizationCapRow is a perps.spot_admin row of kind
// utilization_cap_updated.
type SpotUtilizationCapRow struct {
	Fact
	Token             common.Address
	MaxUtilizationBps string
}

// SpotShareTransferRow is a perps.spot_share_transfers row of kind transfer.
type SpotShareTransferRow struct {
	Fact
	ShareToken  common.Address
	Token       common.Address
	FromAccount common.Address
	ToAccount   common.Address
	Value       string
}

// SpotShareApprovalRow is a perps.spot_share_transfers row of kind approval.
type SpotShareApprovalRow struct {
	Fact
	ShareToken common.Address
	Token      common.Address
	Holder     common.Address
	Spender    common.Address
	Value      string
}

// Side is the side of a limit order: orderType 0 buy, 1 sell.
func (r SpotLimitCreatedRow) Side() (string, bool) {
	switch r.OrderType {
	case SpotLimitBuy:
		return "buy", true
	case SpotLimitSell:
		return "sell", true
	}
	return "", false
}

// Side is the side of a stop-limit order: orderType 3 buy, 4 sell.
func (r SpotStopLimitCreatedRow) Side() (string, bool) {
	switch r.OrderType {
	case SpotStopLimitBuy:
		return "buy", true
	case SpotStopLimitSell:
		return "sell", true
	}
	return "", false
}

// Side is the side of a stop-loss order, always a sell.
func (SpotStopLossCreatedRow) Side() (string, bool) { return "sell", true }

func (SpotMarketOrderRow) Table() string        { return "spot_swaps" }
func (SpotNativeSwapRow) Table() string         { return "spot_swaps" }
func (SpotSimpleSwapRow) Table() string         { return "spot_swaps" }
func (SpotPriceImpactRow) Table() string        { return "spot_fee_events" }
func (SpotTieredFeeRow) Table() string          { return "spot_fee_events" }
func (SpotLimitCreatedRow) Table() string       { return "spot_order_events" }
func (SpotStopLimitCreatedRow) Table() string   { return "spot_order_events" }
func (SpotStopLossCreatedRow) Table() string    { return "spot_order_events" }
func (SpotOrderIDRow) Table() string            { return "spot_order_events" }
func (SpotOrderExecutedRow) Table() string      { return "spot_order_events" }
func (SpotLimitModifiedRow) Table() string      { return "spot_order_events" }
func (SpotStopLimitActivatedRow) Table() string { return "spot_order_events" }
func (SpotDepositRow) Table() string            { return "spot_vault_events" }
func (SpotLPWithdrawalRow) Table() string       { return "spot_vault_events" }
func (SpotNativeDepositRow) Table() string      { return "spot_vault_events" }
func (SpotNativeWithdrawalRow) Table() string   { return "spot_vault_events" }
func (SpotWithdrawalRow) Table() string         { return "spot_vault_events" }
func (SpotReservesSyncRow) Table() string       { return "spot_vault_events" }
func (SpotReservesUpdatedRow) Table() string    { return "spot_vault_events" }
func (SpotTokenRegisteredRow) Table() string    { return "spot_token_registry" }
func (SpotShareTokenCreatedRow) Table() string  { return "spot_token_registry" }
func (SpotOwnershipRow) Table() string          { return "spot_admin" }
func (SpotPauseRow) Table() string              { return "spot_admin" }
func (SpotTwapSanityRow) Table() string         { return "spot_admin" }
func (SpotOperatorRow) Table() string           { return "spot_admin" }
func (SpotTokenPauseRow) Table() string         { return "spot_admin" }
func (SpotTrackerRow) Table() string            { return "spot_admin" }
func (SpotUtilizationCapRow) Table() string     { return "spot_admin" }
func (SpotShareTransferRow) Table() string      { return "spot_share_transfers" }
func (SpotShareApprovalRow) Table() string      { return "spot_share_transfers" }
