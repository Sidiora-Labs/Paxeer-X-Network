package decode

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type CorrelationBinding struct {
	NativeEmitter    common.Address
	NativeRole       string
	CompanionEmitter common.Address
	CompanionRole    string
	WrappedNative    common.Address
}

type OptionalEffect struct {
	Identity   RawIdentity      `json:"identity"`
	Family     string           `json:"family"`
	Action     string           `json:"action"`
	Native     *DecodedEnvelope `json:"native"`
	Companions []RawIdentity    `json:"companions"`
	Economic   bool             `json:"economic"`
}

type CorrelatedReceipt struct {
	Decoded     *OptionalBatch   `json:"decoded"`
	Effects     []OptionalEffect `json:"effects"`
	Unmatched   []RawIdentity    `json:"unmatched"`
	Diagnostics []string         `json:"diagnostics"`
}

func (r *Registry) CorrelateOptionalReceipt(receipt *ethtypes.Receipt, bindings []CorrelationBinding) (*CorrelatedReceipt, error) {
	if r == nil || receipt == nil || receipt.Status != ethtypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.TxHash == (common.Hash{}) || receipt.BlockHash == (common.Hash{}) || len(receipt.Logs) > MaxRawBatchLogs {
		return nil, fmt.Errorf("successful bounded receipt required")
	}
	if len(bindings) > 64 {
		return nil, fmt.Errorf("correlation binding bound exceeded")
	}
	seenBindings := map[string]bool{}
	for _, b := range bindings {
		if b.NativeEmitter == (common.Address{}) || b.CompanionEmitter == (common.Address{}) || b.NativeEmitter == b.CompanionEmitter || optionalFamily(b.NativeRole, "") == "" || (b.CompanionRole != "EventEmitter" && b.CompanionRole != "TransactionTracker") {
			return nil, fmt.Errorf("invalid source-attested correlation binding")
		}
		key := b.NativeEmitter.Hex() + "/" + b.CompanionEmitter.Hex()
		if seenBindings[key] {
			return nil, fmt.Errorf("duplicate or conflicting correlation binding")
		}
		seenBindings[key] = true
	}
	logs := make([]ethtypes.Log, 0, len(receipt.Logs))
	for _, l := range receipt.Logs {
		if l == nil || l.BlockHash != receipt.BlockHash || l.TxHash != receipt.TxHash || l.BlockNumber != receipt.BlockNumber.Uint64() || l.TxIndex != receipt.TransactionIndex {
			return nil, fmt.Errorf("receipt log coordinates differ")
		}
		logs = append(logs, *l)
	}
	decoded, err := r.DecodeOptionalBatch(logs)
	if err != nil {
		return nil, err
	}
	out := &CorrelatedReceipt{Decoded: decoded, Effects: []OptionalEffect{}, Unmatched: []RawIdentity{}, Diagnostics: []string{}}
	sort.Slice(decoded.Optional, func(i, j int) bool { return decoded.Optional[i].Raw.Order.less(decoded.Optional[j].Raw.Order) })
	native := []*DecodedEnvelope{}
	companions := []*DecodedEnvelope{}
	for _, e := range decoded.Optional {
		if e.Status != "decoded" || e.Event == nil {
			continue
		}
		if e.EmitterRole == "EventEmitter" || e.EmitterRole == "TransactionTracker" {
			companions = append(companions, e)
		} else {
			native = append(native, e)
		}
	}
	if len(native) > 0 && len(companions) > (1<<20)/len(native) {
		out.Diagnostics = append(out.Diagnostics, "unavailable_correlation:comparison_bound")
		for _, e := range decoded.Optional {
			out.Unmatched = append(out.Unmatched, e.Raw.Identity)
		}
		return out, nil
	}
	matches := make([][]int, len(companions))
	counts := make([]map[string]int, len(native))
	for i := range native {
		counts[i] = map[string]int{}
	}
	for j, c := range companions {
		for i, n := range native {
			admitted := false
			wrapped := common.Address{}
			for _, b := range bindings {
				if common.HexToAddress(n.Raw.Emitter) == b.NativeEmitter && n.EmitterRole == b.NativeRole && common.HexToAddress(c.Raw.Emitter) == b.CompanionEmitter && c.EmitterRole == b.CompanionRole {
					admitted = true
					wrapped = b.WrappedNative
					break
				}
			}
			if admitted && companionMatches(n, c, wrapped) {
				matches[j] = append(matches[j], i)
				counts[i][c.EmitterRole+":"+c.EventName]++
			}
		}
	}
	for _, n := range native {
		out.Effects = append(out.Effects, OptionalEffect{Identity: n.Raw.Identity, Family: optionalFamily(n.EmitterRole, n.EventName), Action: n.EventName, Native: n, Companions: []RawIdentity{}, Economic: optionalEconomic(n)})
	}
	for j, c := range companions {
		if len(matches[j]) != 1 || counts[matches[j][0]][c.EmitterRole+":"+c.EventName] != 1 {
			out.Unmatched = append(out.Unmatched, c.Raw.Identity)
			out.Diagnostics = append(out.Diagnostics, fmt.Sprintf("unavailable_correlation:%s:%d", c.EventName, c.Raw.Identity.LogIndex))
			continue
		}
		out.Effects[matches[j][0]].Companions = append(out.Effects[matches[j][0]].Companions, c.Raw.Identity)
	}
	return out, nil
}

func optionalEconomic(e *DecodedEnvelope) bool {
	switch e.EmitterRole + ":" + e.EventName {
	case "PECOR:SimpleSwap", "PECOR:MarketOrderExecuted", "PECOR:NativeSwap":
		return true
	}
	return false
}

func equalFields(a, b map[string]any, names ...string) bool {
	for _, k := range names {
		av, aok := a[k]
		bv, bok := b[k]
		if !aok || !bok || fmt.Sprint(av) != fmt.Sprint(bv) {
			return false
		}
	}
	return true
}

func companionMatches(n, c *DecodedEnvelope, wrapped common.Address) bool {
	a, b := n.Event.Args, c.Event.Args
	if n.EmitterRole == "PECOROrders" {
		if !equalFields(a, b, "orderId") {
			return false
		}
		if c.EmitterRole == "TransactionTracker" {
			switch n.EventName + ":" + c.EventName {
			case "LimitOrderCreated:LimitOrderPlaced":
				return (fmt.Sprint(a["orderType"]) == "0" || fmt.Sprint(a["orderType"]) == "1") && equalFields(a, b, "user", "amount", "targetPrice") && (fmt.Sprint(a["orderType"]) == "0") == (fmt.Sprint(b["isBuy"]) == "true")
			case "LimitOrderCreated:StopLossPlaced":
				return fmt.Sprint(a["orderType"]) == "2" && equalFields(a, b, "user") && fmt.Sprint(a["amount"]) == fmt.Sprint(b["tokenAmount"]) && fmt.Sprint(a["targetPrice"]) == fmt.Sprint(b["triggerPrice"])
			case "StopLimitOrderCreated:StopLimitPlaced":
				return equalFields(a, b, "user", "amount", "stopPrice", "limitPrice")
			case "LimitOrderExecuted:LimitOrderExecuted", "StopLimitExecuted:StopLimitExecuted":
				return equalFields(a, b, "executionPrice")
			case "StopLimitActivated:StopLimitActivated":
				return equalFields(a, b, "activationPrice")
			case "LimitOrderCancelled:LimitOrderCancelled":
				return true
			}
		}
		if c.EmitterRole == "EventEmitter" {
			switch n.EventName {
			case "LimitOrderCreated":
				return c.EventName == "PecorOrderCreated" && fmt.Sprint(b["orderKind"]) == "0" && equalFields(a, b, "user", "amount", "orderType", "targetPrice")
			case "StopLimitOrderCreated":
				return c.EventName == "PecorOrderCreated" && fmt.Sprint(b["orderKind"]) == "1" && equalFields(a, b, "user", "amount", "orderType", "stopPrice", "limitPrice")
			case "LimitOrderCancelled", "StopLimitCancelled":
				return c.EventName == "PecorOrderLifecycle" && fmt.Sprint(b["phase"]) == "2" && ((n.EventName == "LimitOrderCancelled" && fmt.Sprint(b["orderKind"]) == "0") || (n.EventName == "StopLimitCancelled" && fmt.Sprint(b["orderKind"]) == "1"))
			case "LimitOrderExecuted", "StopLimitExecuted":
				return c.EventName == "PecorOrderLifecycle" && fmt.Sprint(b["phase"]) == "1" && fmt.Sprint(a["executionPrice"]) == fmt.Sprint(b["price"]) && ((n.EventName == "LimitOrderExecuted" && fmt.Sprint(b["orderKind"]) == "0") || (n.EventName == "StopLimitExecuted" && fmt.Sprint(b["orderKind"]) == "1"))
			case "StopLimitActivated":
				return c.EventName == "PecorOrderLifecycle" && fmt.Sprint(b["phase"]) == "0" && fmt.Sprint(b["orderKind"]) == "1" && fmt.Sprint(a["activationPrice"]) == fmt.Sprint(b["price"])
			}
		}
	}
	if n.EmitterRole == "PECOR" {
		switch n.EventName {
		case "SimpleSwap":
			return ((c.EmitterRole == "TransactionTracker" && c.EventName == "TradeExecuted") || (c.EmitterRole == "EventEmitter" && c.EventName == "PecorSwap" && (fmt.Sprint(b["swapKind"]) == "0" || fmt.Sprint(b["swapKind"]) == "1"))) && equalFields(a, b, "user", "tokenIn", "tokenOut", "amountIn", "amountOut")
		case "MarketOrderExecuted":
			if c.EmitterRole == "TransactionTracker" && c.EventName == "MarketTrade" {
				return equalFields(a, b, "user", "stablecoin", "token", "stablecoinAmount", "tokenAmount", "isBuy")
			}
			if (c.EmitterRole == "TransactionTracker" && c.EventName == "TradeExecuted") || (c.EmitterRole == "EventEmitter" && c.EventName == "PecorSwap" && fmt.Sprint(b["swapKind"]) == "2") {
				ti, to, ai, ao := a["stablecoin"], a["token"], a["stablecoinAmount"], a["tokenAmount"]
				if fmt.Sprint(a["isBuy"]) == "false" {
					ti, to, ai, ao = to, ti, ao, ai
				}
				return equalFields(a, b, "user") && fmt.Sprint(ti) == fmt.Sprint(b["tokenIn"]) && fmt.Sprint(to) == fmt.Sprint(b["tokenOut"]) && fmt.Sprint(ai) == fmt.Sprint(b["amountIn"]) && fmt.Sprint(ao) == fmt.Sprint(b["amountOut"])
			}
		case "NativeSwap":
			if wrapped == (common.Address{}) {
				return false
			}
			if (c.EmitterRole == "TransactionTracker" && c.EventName == "TradeExecuted") || (c.EmitterRole == "EventEmitter" && c.EventName == "PecorSwap" && fmt.Sprint(b["swapKind"]) == "3") {
				ti, to, ai, ao := any(strings.ToLower(wrapped.Hex())), a["tokenOther"], a["nativeAmount"], a["tokenAmount"]
				if fmt.Sprint(a["nativeIsInput"]) == "false" {
					ti, to, ai, ao = to, ti, ao, ai
				}
				return equalFields(a, b, "user") && fmt.Sprint(ti) == fmt.Sprint(b["tokenIn"]) && fmt.Sprint(to) == fmt.Sprint(b["tokenOut"]) && fmt.Sprint(ai) == fmt.Sprint(b["amountIn"]) && fmt.Sprint(ao) == fmt.Sprint(b["amountOut"])
			}
		}
	}
	if n.EmitterRole == "MetaAGRouter" && n.EventName == "BestRouteSwap" && c.EmitterRole == "EventEmitter" && c.EventName == "BestRouteSwap" {
		return equalFields(a, b, "user", "tokenIn", "tokenOut", "amountIn", "amountOut") && fmt.Sprint(a["adapterId"]) == fmt.Sprint(b["routeId"])
	}
	if n.EmitterRole == "PECORVault" {
		if c.EmitterRole == "TransactionTracker" {
			if n.EventName == "Deposit" && c.EventName == "LiquidityAdded" {
				return equalFields(a, b, "token", "amount", "newReserve") && fmt.Sprint(a["depositor"]) == fmt.Sprint(b["provider"])
			}
			if n.EventName == "Withdrawal" && c.EventName == "LiquidityRemoved" {
				return equalFields(a, b, "token", "amount", "newReserve") && fmt.Sprint(a["recipient"]) == fmt.Sprint(b["remover"])
			}
		}
		if c.EmitterRole == "EventEmitter" && c.EventName == "VaultFlow" {
			flow, party := "", ""
			switch n.EventName {
			case "Deposit":
				flow, party = "0", "depositor"
			case "Withdrawal":
				flow, party = "1", "recipient"
			case "NativeDeposit":
				flow, party = "2", "depositor"
			case "NativeWithdrawal":
				flow, party = "3", "recipient"
			case "EmergencyWithdraw":
				flow, party = "4", "recipient"
			case "ReservesSync":
				return fmt.Sprint(b["flowType"]) == "5" && equalFields(a, b, "token") && fmt.Sprint(a["newReserves"]) == fmt.Sprint(b["newReserve"]) && fmt.Sprint(a["recovered"]) == fmt.Sprint(b["amount"])
			}
			if flow == "" || fmt.Sprint(b["flowType"]) != flow || fmt.Sprint(a[party]) != fmt.Sprint(b["party"]) || !equalFields(a, b, "amount", "newReserve") {
				return false
			}
			if flow == "2" || flow == "3" {
				return fmt.Sprint(b["token"]) == (common.Address{}).Hex()
			}
			return equalFields(a, b, "token")
		}
	}
	return false
}

func eventActors(e *DecodedEnvelope) *ActorContext {
	out := &ActorContext{AttributionConfidence: "unknown", AttributionMethod: "unavailable", EvidenceIdentity: []RawIdentity{e.Raw.Identity}, Revision: "actor-v1"}
	if e.Event == nil {
		return out
	}
	field := "sender"
	if e.EmitterRole == "PECOR" {
		field = "user"
	}
	if value, ok := e.Event.Args[field].(string); ok && common.IsHexAddress(value) && common.HexToAddress(value) != (common.Address{}) {
		normalized := strings.ToLower(common.HexToAddress(value).Hex())
		out.EventCaller = &normalized
	}
	return out
}

type ActorMarketBinding struct {
	Pool       common.Address
	Token      common.Address
	Settlement common.Address
}

func (r *Registry) DecodeTransactionReceipt(tx *ethtypes.Transaction, receipt *ethtypes.Receipt, markets []ActorMarketBinding) ([]*DecodedEnvelope, error) {
	if r == nil || tx == nil || receipt == nil || !tx.Protected() || tx.ChainId().Cmp(big.NewInt(125)) != 0 || receipt.Status != ethtypes.ReceiptStatusSuccessful || receipt.TxHash != tx.Hash() || receipt.BlockHash == (common.Hash{}) || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || len(receipt.Logs) > MaxRawBatchLogs {
		return nil, fmt.Errorf("signed chain125 transaction and matching successful receipt required")
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(125)), tx)
	if err != nil || sender == (common.Address{}) {
		return nil, fmt.Errorf("signed transaction origin unavailable: %v", err)
	}
	origin := strings.ToLower(sender.Hex())
	if len(markets) > 64 {
		return nil, fmt.Errorf("actor market binding bound exceeded")
	}
	bindings := map[common.Address]ActorMarketBinding{}
	for _, market := range markets {
		if market.Pool == (common.Address{}) || market.Token == (common.Address{}) || market.Settlement == (common.Address{}) || market.Token == market.Settlement {
			return nil, fmt.Errorf("source-attested actor market binding required")
		}
		if _, exists := bindings[market.Pool]; exists {
			return nil, fmt.Errorf("duplicate actor market binding")
		}
		bindings[market.Pool] = market
	}
	logs := make([]ethtypes.Log, 0, len(receipt.Logs))
	for _, l := range receipt.Logs {
		if l == nil || l.TxHash != receipt.TxHash || l.BlockHash != receipt.BlockHash || l.BlockNumber != receipt.BlockNumber.Uint64() || l.TxIndex != receipt.TransactionIndex {
			return nil, fmt.Errorf("receipt log coordinates differ")
		}
		logs = append(logs, *l)
	}
	logs, err = DeduplicateRaw(logs)
	if err != nil {
		return nil, err
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Index < logs[j].Index })
	out := make([]*DecodedEnvelope, 0, len(logs))
	for _, l := range logs {
		e, decodeErr := r.Decode(l)
		if decodeErr != nil && e == nil {
			return nil, decodeErr
		}
		out = append(out, e)
		if e.Actors != nil {
			e.Actors.TransactionOrigin = &origin
		}
	}
	economicCount := 0
	for _, e := range out {
		if e.Actors != nil {
			economicCount++
		}
	}
	if economicCount > 0 && len(out) > (1<<20)/economicCount {
		for _, e := range out {
			if e.Actors != nil {
				e.Alerts = append(e.Alerts, "actor_unavailable:comparison_bound")
			}
		}
		return out, nil
	}
	for _, e := range out {
		if e.Actors == nil || e.Event == nil || e.EmitterRole != "EventEmitter" || e.EventName != "Swap" {
			continue
		}
		poolID, ok := e.Event.Args["poolId"].(string)
		if !ok || len(poolID) != 66 {
			continue
		}
		poolWord, ok := new(big.Int).SetString(strings.TrimPrefix(poolID, "0x"), 16)
		if !ok || poolWord.Sign() == 0 || poolWord.BitLen() > 160 {
			continue
		}
		poolAddress := common.BigToAddress(poolWord)
		market, admitted := bindings[poolAddress]
		if !admitted || !r.roleAt(poolAddress, "SidioraPool", e.Raw.Order) {
			continue
		}
		outputToken := market.Token
		isBuy, ok := e.Event.Args["isBuy"].(bool)
		if !ok {
			continue
		}
		if !isBuy {
			outputToken = market.Settlement
		}
		pool := strings.ToLower(poolAddress.Hex())
		matches := []*DecodedEnvelope{}
		for _, transfer := range out {
			if transfer.Event == nil || transfer.Status != "decoded" || transfer.EmitterRole != "SidioraERC20" || common.HexToAddress(transfer.Raw.Emitter) != outputToken || transfer.EventName != "Transfer" || transfer.Raw.Order.Log >= e.Raw.Order.Log {
				continue
			}
			args := transfer.Event.Args
			if args["from"] == pool && args["value"] == e.Event.Args["amountOut"] {
				matches = append(matches, transfer)
			}
		}
		if len(matches) != 1 {
			continue
		}
		recipient, ok := matches[0].Event.Args["to"].(string)
		if !ok || !common.IsHexAddress(recipient) || common.HexToAddress(recipient) == (common.Address{}) {
			continue
		}
		e.Actors.Recipient = &recipient
		e.Actors.Beneficiary = &recipient
		e.Actors.AttributionConfidence = "receipt_evidence"
		e.Actors.AttributionMethod = "unique_pool_output_transfer"
		e.Actors.EvidenceIdentity = append(e.Actors.EvidenceIdentity, matches[0].Raw.Identity)
	}
	return out, nil
}

func (r *Registry) roleAt(address common.Address, role string, order Order) bool {
	for _, epoch := range r.epochs[address] {
		d := epoch.deployment
		if d.Role == role && !order.less(d.From) && (d.Until == nil || order.less(*d.Until)) && (!d.Optional || d.Enabled) {
			return true
		}
	}
	return false
}
