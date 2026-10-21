package coverage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const MaxReceiptLogs = 4096

type Identity struct {
	ChainID                    uint64
	BlockHash, TransactionHash common.Hash
	Emitter                    common.Address
	LogIndex                   uint
}

type Position struct {
	Block            uint64
	Transaction, Log uint
}

func (a Position) Before(b Position) bool {
	if a.Block != b.Block {
		return a.Block < b.Block
	}
	if a.Transaction != b.Transaction {
		return a.Transaction < b.Transaction
	}
	return a.Log < b.Log
}
func position(l types.Log) Position { return Position{l.BlockNumber, l.TxIndex, l.Index} }
func identity(chain uint64, l types.Log) Identity {
	return Identity{chain, l.BlockHash, l.TxHash, l.Address, l.Index}
}

type PoolRegistration struct {
	Pool, Token, Creator, Optical, Settlement common.Address
	NFTID                                     *big.Int
	PoolID                                    common.Hash
}

type AuthoritySource interface {
	CallerAt(context.Context, Identity, Position) (common.Address, [4]byte, []PermissionEpoch, error)
	PoolAt(context.Context, common.Hash, common.Hash, common.Address) (PoolRegistration, error)
	ReserveAt(context.Context, common.Hash, common.Address, common.Address) (*big.Int, error)
	WrappedNativeAt(context.Context, common.Hash, common.Address) (common.Address, error)
}

type Record struct {
	Identity  Identity
	Decoded   catalog.Result
	Event     string
	Authority Rule
	Caller    common.Address
	Status    string
}

type Effect struct {
	ID                string
	Identity          Identity
	Position          Position
	Role, Event, Kind string
	RouteID           string
	Fields            map[string]any
	Companions        []Identity
	Actors            ActorSet
}

type ReceiptResult struct {
	TransactionHash, BlockHash common.Hash
	Records                    []Record
	Effects                    []Effect
	Diagnostics                []string
	Successful                 bool
	EnrichmentMissing          bool
	seal                       [32]byte
}

type Correlator struct {
	Registry *catalog.Registry
	Source   AuthoritySource
}

func (c *Correlator) Correlate(ctx context.Context, header *types.Header, tx *types.Transaction, receipt *types.Receipt) (*ReceiptResult, error) {
	if c == nil || c.Registry == nil || header == nil || header.Number == nil || !header.Number.IsUint64() || tx == nil || tx.ChainId().Cmp(big.NewInt(125)) != 0 || receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.TxHash != tx.Hash() || receipt.BlockHash != header.Hash() || receipt.BlockNumber.Cmp(header.Number) != 0 || len(receipt.Logs) > MaxReceiptLogs {
		return nil, fmt.Errorf("canonical signed chain125 receipt required")
	}
	from, err := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), tx)
	if err != nil {
		return nil, fmt.Errorf("signed sender: %w", err)
	}
	out := &ReceiptResult{TransactionHash: receipt.TxHash, BlockHash: receipt.BlockHash, Successful: receipt.Status == types.ReceiptStatusSuccessful}
	if !out.Successful {
		if len(receipt.Logs) != 0 {
			return nil, fmt.Errorf("failed receipt contains logs")
		}
		return out, nil
	}
	seen := map[uint]types.Log{}
	logs := make([]types.Log, 0, len(receipt.Logs))
	totalBytes := 0
	for _, lp := range receipt.Logs {
		if lp == nil {
			return nil, fmt.Errorf("nil receipt log")
		}
		l := *lp
		if len(l.Data) > 16*1024*1024-totalBytes {
			return nil, fmt.Errorf("receipt raw byte bound exceeded; source archive required")
		}
		totalBytes += len(l.Data)
		if l.Removed || l.Address == (common.Address{}) || l.BlockHash != receipt.BlockHash || l.TxHash != receipt.TxHash || l.BlockNumber != receipt.BlockNumber.Uint64() || l.TxIndex != receipt.TransactionIndex {
			return nil, fmt.Errorf("receipt log coordinates differ")
		}
		if old, exists := seen[l.Index]; exists {
			if old.Address != l.Address || !bytes.Equal(old.Data, l.Data) || len(old.Topics) != len(l.Topics) {
				return nil, fmt.Errorf("conflicting duplicate log identity")
			}
			for i := range old.Topics {
				if old.Topics[i] != l.Topics[i] {
					return nil, fmt.Errorf("conflicting duplicate topics")
				}
			}
			continue
		}
		seen[l.Index] = l
		logs = append(logs, l)
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Index < logs[j].Index })
	for _, l := range logs {
		d := c.Registry.Decode(125, l)
		event := strings.SplitN(d.Signature, "(", 2)[0]
		record := Record{Identity: identity(125, l), Decoded: d, Event: event, Authority: Authority(d.EmitterRole, event), Status: d.Status}
		if d.Status != "decoded" {
			out.Records = append(out.Records, record)
			continue
		}
		if record.Authority.RequiresCallerPermission {
			record.Status = "caller_evidence_unavailable"
			if c.Source != nil {
				caller, selector, epochs, e := c.Source.CallerAt(ctx, record.Identity, position(l))
				expected, ok := TypedSelector(event)
				if e == nil && ok && expected == selector && CallerAllowedAt(epochs, 125, l.Address, caller, selector, position(l)) {
					record.Caller = caller
					record.Status = "authorized"
				} else if e == nil {
					record.Status = "typed_permission_mismatch"
				}
			}
		} else {
			record.Status = "authorized"
		}
		out.Records = append(out.Records, record)
		if record.Status != "authorized" || record.Authority.Effect == "none" || record.Authority.Kind == "declared_not_emitted" {
			if record.Authority.Effect == "core_fill" && record.Status != "authorized" {
				out.Diagnostics = append(out.Diagnostics, "core_swap_coverage_gap")
			}
			continue
		}
		effect := Effect{Identity: record.Identity, Position: position(l), Role: d.EmitterRole, Event: event, Kind: record.Authority.Effect, Fields: d.Fields, RouteID: receipt.TxHash.Hex()}
		effect.Actors = actors(from, tx.To(), l.Address, d.Fields)
		if effect.Kind == "core_fill" || effect.Kind == "market_attachment" {
			if c.Source == nil {
				out.Diagnostics = append(out.Diagnostics, "coverage_gap:"+effect.Kind)
				continue
			}
			pool, _ := d.Fields["pool"].(common.Address)
			poolID := hashField(d.Fields["poolId"])
			registration, e := c.Source.PoolAt(ctx, l.BlockHash, poolID, pool)
			if e != nil || registration.Pool == (common.Address{}) || registration.Token == (common.Address{}) || registration.Settlement == (common.Address{}) {
				out.Diagnostics = append(out.Diagnostics, "pool_registration_unavailable")
				continue
			}
			if effect.Kind == "market_attachment" && !creationMatches(d.Fields, registration) {
				out.Diagnostics = append(out.Diagnostics, "registry_attachment_mismatch")
				continue
			}
			if effect.Kind == "core_fill" && (registration.PoolID != poolID || record.Caller != registration.Pool) {
				out.Diagnostics = append(out.Diagnostics, "unauthorized_pool_swap")
				continue
			}
			effect.Fields = copyFields(d.Fields)
			effect.Fields["pool"] = registration.Pool
			effect.Fields["token"] = registration.Token
			effect.Fields["settlement"] = registration.Settlement
		}
		if d.EmitterRole == "PECOR" && event == "TrackerCallFailed" {
			out.EnrichmentMissing = true
		}
		effect.ID = effectID(effect.Identity, effect.Kind)
		out.Effects = append(out.Effects, effect)
	}
	c.correlateCompanions(out)
	out.correlateRoutes()
	out.attributeTransfers()
	out.reconcileReserves(ctx, c.Source)
	sort.Strings(out.Diagnostics)
	out.seal = receiptSeal(out)
	return out, nil
}

func effectID(id Identity, kind string) string {
	raw, _ := json.Marshal(struct {
		Identity Identity
		Kind     string
	}{id, kind})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func hashField(v any) common.Hash {
	switch h := v.(type) {
	case [32]byte:
		return common.Hash(h)
	case common.Hash:
		return h
	}
	return common.Hash{}
}
func copyFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if n, ok := v.(*big.Int); ok {
			out[k] = new(big.Int).Set(n)
		} else {
			out[k] = v
		}
	}
	return out
}
func creationMatches(fields map[string]any, p PoolRegistration) bool {
	return fields["pool"] == p.Pool && fields["token"] == p.Token && fields["creator"] == p.Creator && fields["optical"] == p.Optical && equalNumber(fields["nftId"], p.NFTID)
}
func equalNumber(a, b any) bool {
	x, ok := a.(*big.Int)
	y, other := b.(*big.Int)
	return ok && other && x != nil && y != nil && x.Cmp(y) == 0
}
func (c *Correlator) correlateCompanions(out *ReceiptResult) {
	for _, record := range out.Records {
		if record.Status != "authorized" || record.Authority.Kind != "corroboration" {
			continue
		}
		matches := []int{}
		for i, effect := range out.Effects {
			if record.Caller != (common.Address{}) && record.Caller != effect.Identity.Emitter {
				continue
			}
			if companionMatches(effect, record) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 1 {
			out.Effects[matches[0]].Companions = append(out.Effects[matches[0]].Companions, record.Identity)
		} else {
			out.Diagnostics = append(out.Diagnostics, "mirror_native_correlation_conflict:"+record.Event)
		}
	}
}
func companionMatches(n Effect, m Record) bool {
	a, b := n.Fields, m.Decoded.Fields
	switch n.Role + ":" + n.Event + ":" + m.Decoded.EmitterRole + ":" + m.Event {
	case "SidioraERC20:Transfer:EventEmitter:TokenTransfer":
		return b["token"] == n.Identity.Emitter && a["from"] == b["from"] && a["to"] == b["to"] && equalNumber(a["value"], b["value"])
	case "SidioraNFT:Transfer:EventEmitter:NftTransfer":
		return b["nft"] == n.Identity.Emitter && a["from"] == b["from"] && a["to"] == b["to"] && equalNumber(a["tokenId"], b["tokenId"])
	case "SidioraFactory:MarketCreated:Router:MarketCreated":
		return a["token"] == b["token"] && a["pool"] == b["pool"] && a["creator"] == b["creator"] && equalNumber(a["nftId"], b["nftId"])
	case "SidioraFactory:MarketCreated:EventEmitter:MarketCreated":
		return a["token"] == b["token"] && a["pool"] == b["pool"] && a["creator"] == b["creator"] && a["optical"] == b["optical"]
	case "PECOR:SimpleSwap:EventEmitter:PecorSwap", "PECOR:SimpleSwap:TransactionTracker:TradeExecuted":
		return a["user"] == b["user"] && a["tokenIn"] == b["tokenIn"] && a["tokenOut"] == b["tokenOut"] && equalNumber(a["amountIn"], b["amountIn"]) && equalNumber(a["amountOut"], b["amountOut"])
	case "FeeAccumulator:OpticalSurplusRecorded:EventEmitter:FeeFlow":
		return b["kind"] == uint8(7) && a["pool"] == b["pool"] && equalNumber(a["amount"], b["amount"])
	case "FeeAccumulator:OpticalSurplusClaimed:EventEmitter:FeeFlow":
		return b["kind"] == uint8(8) && a["pool"] == b["pool"] && a["recipient"] == b["party"] && equalNumber(a["amount"], b["amount"])
	case "FeeAccumulator:FeeRecorded:EventEmitter:FeeFlow":
		return b["kind"] == uint8(0) && a["pool"] == b["pool"] && equalNumber(a["feeAmount"], b["amount"]) && equalNumber(a["protocolCut"], b["protocolCut"]) && equalNumber(a["poolCut"], b["poolCut"])
	case "FeeAccumulator:FeesBurned:EventEmitter:FeeFlow":
		return b["kind"] == uint8(2) && a["pool"] == b["pool"] && equalNumber(a["amount"], b["amount"])
	case "FeeAccumulator:LpRewardsSent:EventEmitter:FeeFlow":
		return b["kind"] == uint8(5) && a["pool"] == b["pool"] && b["party"] == a["pool"] && equalNumber(a["amount"], b["amount"])
	case "FeeAccumulator:AirdropClaimed:EventEmitter:FeeFlow":
		return b["kind"] == uint8(4) && a["pool"] == b["pool"] && a["holder"] == b["party"] && equalNumber(a["amount"], b["amount"]) && equalNumber(a["epoch"], b["epoch"])
	case "FeeAccumulator:AirdropTriggered:EventEmitter:FeeFlow":
		return b["kind"] == uint8(3) && a["pool"] == b["pool"] && equalNumber(a["totalAmount"], b["amount"]) && equalNumber(a["epoch"], b["epoch"])
	case "FeeAccumulator:FeesClaimed:EventEmitter:FeeFlow":
		return b["kind"] == uint8(1) && a["pool"] == b["pool"] && a["recipient"] == b["party"] && equalNumber(a["amount"], b["amount"])
	case "PECORVault:Deposit:EventEmitter:VaultFlow":
		return b["flowType"] == uint8(0) && a["token"] == b["token"] && a["depositor"] == b["party"] && equalNumber(a["amount"], b["amount"]) && equalNumber(a["newReserve"], b["newReserve"])
	}
	return false
}
func (out *ReceiptResult) reconcileReserves(ctx context.Context, source AuthoritySource) {
	last := map[string]int{}
	for i, e := range out.Effects {
		if e.Role != "PECORVault" || (e.Kind != "funding_movement" && e.Kind != "reserve_state") {
			continue
		}
		token, _ := e.Fields["token"].(common.Address)
		if token == (common.Address{}) && source != nil {
			wrapped, err := source.WrappedNativeAt(ctx, out.BlockHash, e.Identity.Emitter)
			if err == nil {
				token = wrapped
			}
		}
		if token == (common.Address{}) {
			out.Diagnostics = append(out.Diagnostics, "unknown_vault_asset")
			continue
		}
		last[e.Identity.Emitter.Hex()+token.Hex()] = i
	}
	keys := make([]string, 0, len(last))
	for k := range last {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		i := last[key]
		e := out.Effects[i]
		token, _ := e.Fields["token"].(common.Address)
		if source == nil {
			out.Diagnostics = append(out.Diagnostics, "vault_reserve_unavailable")
			continue
		}
		if token == (common.Address{}) {
			token, _ = source.WrappedNativeAt(ctx, out.BlockHash, e.Identity.Emitter)
		}
		reserve, err := source.ReserveAt(ctx, out.BlockHash, e.Identity.Emitter, token)
		observed := e.Fields["newReserve"]
		if observed == nil {
			observed = e.Fields["newReserves"]
		}
		if err != nil || reserve == nil {
			out.Diagnostics = append(out.Diagnostics, "vault_reserve_unavailable")
		} else if !equalNumber(observed, reserve) {
			out.Diagnostics = append(out.Diagnostics, "vault_reserve_mismatch")
		}
	}
}

func receiptSeal(result *ReceiptResult) [32]byte {
	raw, err := json.Marshal(result)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(raw)
}

type PinnedRPC interface {
	CallContractAtHash(context.Context, ethereum.CallMsg, common.Hash) ([]byte, error)
	TransactionByHash(context.Context, common.Hash) (*types.Transaction, bool, error)
	TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
	HeaderByHash(context.Context, common.Hash) (*types.Header, error)
}

type RPCSource struct {
	RPC          PinnedRPC
	Registry     *catalog.Registry
	PoolRegistry common.Address
	Permissions  []PermissionEpoch
}

func (s *RPCSource) call(ctx context.Context, block common.Hash, address common.Address, family, method string, args ...any) ([]any, error) {
	if s == nil || s.RPC == nil || s.Registry == nil || block == (common.Hash{}) || address == (common.Address{}) {
		return nil, fmt.Errorf("pinned RPC identity required")
	}
	bundles, err := catalog.Bundles()
	if err != nil {
		return nil, err
	}
	var selected *catalog.Bundle
	for i := range bundles {
		if bundles[i].Name == family {
			selected = &bundles[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("reviewed getter family unavailable")
	}
	header, err := s.RPC.HeaderByHash(ctx, block)
	if err != nil {
		return nil, err
	}
	if header == nil || header.Number == nil || !header.Number.IsUint64() || header.Hash() != block {
		return nil, fmt.Errorf("getter header differs")
	}
	dep, ok := s.Registry.DeploymentAt(125, address, header.Number.Uint64())
	if !ok || dep.Family != family || dep.Version != selected.ABIHash {
		return nil, fmt.Errorf("getter deployment revision unattested")
	}
	a, err := abi.JSON(bytes.NewReader(selected.ABI))
	if err != nil {
		return nil, err
	}
	input, err := a.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	raw, err := s.RPC.CallContractAtHash(ctx, ethereum.CallMsg{To: &address, Data: input}, block)
	if err != nil {
		return nil, err
	}
	if len(raw) > catalog.MaxPayload {
		return nil, fmt.Errorf("getter output exceeds bound")
	}
	values, err := a.Unpack(method, raw)
	if err != nil {
		return nil, err
	}
	canonical, err := a.Methods[method].Outputs.Pack(values...)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, fmt.Errorf("noncanonical getter output")
	}
	return values, nil
}
func (s *RPCSource) CallerAt(ctx context.Context, id Identity, at Position) (common.Address, [4]byte, []PermissionEpoch, error) {
	fail := func(err error) (common.Address, [4]byte, []PermissionEpoch, error) {
		return common.Address{}, [4]byte{}, nil, err
	}
	if s == nil || s.RPC == nil || s.Registry == nil || id.ChainID != 125 {
		return fail(fmt.Errorf("authenticated caller source unavailable"))
	}
	tx, pending, err := s.RPC.TransactionByHash(ctx, id.TransactionHash)
	if err != nil {
		return fail(err)
	}
	if pending || tx == nil || tx.Hash() != id.TransactionHash || tx.To() == nil || *tx.To() != id.Emitter || len(tx.Data()) < 4 || tx.ChainId().Cmp(big.NewInt(125)) != 0 {
		return fail(fmt.Errorf("internal emitter call requires authenticated execution trace"))
	}
	receipt, err := s.RPC.TransactionReceipt(ctx, id.TransactionHash)
	if err != nil {
		return fail(err)
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.BlockHash != id.BlockHash || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() != at.Block || receipt.TransactionIndex != at.Transaction {
		return fail(fmt.Errorf("caller receipt differs"))
	}
	found := false
	for _, l := range receipt.Logs {
		if l != nil && l.Index == id.LogIndex && l.Address == id.Emitter && l.TxHash == id.TransactionHash && l.BlockHash == id.BlockHash && !l.Removed {
			found = true
		}
	}
	if !found {
		return fail(fmt.Errorf("caller raw log absent"))
	}
	caller, err := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), tx)
	if err != nil {
		return fail(err)
	}
	var selector [4]byte
	copy(selector[:], tx.Data()[:4])
	dep, ok := s.Registry.DeploymentAt(125, id.Emitter, at.Block)
	if !ok || dep.Family != "EventEmitter" {
		return fail(fmt.Errorf("unregistered emitter getter"))
	}
	if !CallerAllowedAt(s.Permissions, 125, id.Emitter, caller, selector, at) {
		return fail(fmt.Errorf("selector permission canonical position history unavailable"))
	}
	return caller, selector, append([]PermissionEpoch(nil), s.Permissions...), nil
}
func (s *RPCSource) PoolAt(ctx context.Context, block common.Hash, id common.Hash, pool common.Address) (PoolRegistration, error) {
	fail := func(err error) (PoolRegistration, error) { return PoolRegistration{}, err }
	if pool == (common.Address{}) {
		for _, b := range id[:12] {
			if b != 0 {
				return fail(fmt.Errorf("non-address pool id"))
			}
		}
		pool = common.BytesToAddress(id[12:])
	}
	if pool == (common.Address{}) || (id != (common.Hash{}) && id != common.BytesToHash(pool.Bytes())) {
		return fail(fmt.Errorf("pool identity differs"))
	}
	registered, err := s.call(ctx, block, s.PoolRegistry, "PoolRegistry", "isRegisteredPool", pool)
	if err != nil {
		return fail(err)
	}
	if len(registered) != 1 || registered[0] != true {
		return fail(fmt.Errorf("pool not registered"))
	}
	metadata, err := s.call(ctx, block, s.PoolRegistry, "PoolRegistry", "getPoolMetadata", pool)
	if err != nil {
		return fail(err)
	}
	if len(metadata) != 1 {
		return fail(fmt.Errorf("pool metadata absent"))
	}
	converted := abi.ConvertType(metadata[0], new(struct {
		Creator      common.Address
		Token        common.Address
		Optical      common.Address
		NftId        *big.Int
		CreatedAt    *big.Int
		CreatedBlock *big.Int
	}))
	m := converted.(*struct {
		Creator      common.Address
		Token        common.Address
		Optical      common.Address
		NftId        *big.Int
		CreatedAt    *big.Int
		CreatedBlock *big.Int
	})
	settlement, err := s.call(ctx, block, pool, "SidioraPool", "usdlAddress")
	if err != nil {
		return fail(err)
	}
	if len(settlement) != 1 {
		return fail(fmt.Errorf("pool settlement absent"))
	}
	asset, ok := settlement[0].(common.Address)
	if !ok || asset == (common.Address{}) {
		return fail(fmt.Errorf("pool settlement invalid"))
	}
	token, err := s.call(ctx, block, pool, "SidioraPool", "tokenAddress")
	if err != nil {
		return fail(err)
	}
	if len(token) != 1 || token[0] != m.Token {
		return fail(fmt.Errorf("pool token getter differs"))
	}
	return PoolRegistration{Pool: pool, Token: m.Token, Creator: m.Creator, Optical: m.Optical, Settlement: asset, NFTID: m.NftId, PoolID: common.BytesToHash(pool.Bytes())}, nil
}
func (s *RPCSource) ReserveAt(ctx context.Context, block common.Hash, vault, asset common.Address) (*big.Int, error) {
	out, err := s.call(ctx, block, vault, "PECORVault", "getReserves", asset)
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("vault reserve absent")
	}
	return unsigned(out[0])
}
func (s *RPCSource) WrappedNativeAt(ctx context.Context, block common.Hash, vault common.Address) (common.Address, error) {
	out, err := s.call(ctx, block, vault, "PECORVault", "weth")
	if err != nil {
		return common.Address{}, err
	}
	if len(out) != 1 {
		return common.Address{}, fmt.Errorf("wrapped asset absent")
	}
	asset, ok := out[0].(common.Address)
	if !ok || asset == (common.Address{}) {
		return common.Address{}, fmt.Errorf("wrapped asset invalid")
	}
	return asset, nil
}

func (out *ReceiptResult) correlateRoutes() {
	for _, route := range out.Effects {
		if route.Role != "Router" || (route.Event != "Buy" && route.Event != "Sell") {
			continue
		}
		matches := []int{}
		for i, fill := range out.Effects {
			if fill.Kind != "core_fill" || fill.Fields["pool"] != route.Fields["pool"] {
				continue
			}
			isBuy, ok := fill.Fields["isBuy"].(bool)
			if !ok || isBuy != (route.Event == "Buy") {
				continue
			}
			actor, in, outField := "buyer", "usdlIn", "tokensOut"
			if !isBuy {
				actor, in, outField = "seller", "tokensIn", "usdlOut"
			}
			if fill.Fields["sender"] == route.Fields[actor] && equalNumber(fill.Fields["amountIn"], route.Fields[in]) && equalNumber(fill.Fields["amountOut"], route.Fields[outField]) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 1 {
			out.Effects[matches[0]].Companions = append(out.Effects[matches[0]].Companions, route.Identity)
		} else {
			out.Diagnostics = append(out.Diagnostics, "route_leg_mismatch")
		}
	}
}

type MarketCreation struct {
	Creator, Token, Pool, Optical common.Address
	NFTID                         *big.Int
	TransactionHash               common.Hash
	LogIndex                      uint
	FactoryVersion                string
}

func (out *ReceiptResult) ResolveCreation(submitted common.Hash) (MarketCreation, error) {
	if out == nil || !out.Successful || out.TransactionHash != submitted || submitted == (common.Hash{}) || out.seal != receiptSeal(out) {
		return MarketCreation{}, fmt.Errorf("exact correlated creation transaction required")
	}
	matches := []Effect{}
	for _, e := range out.Effects {
		if e.Role == "SidioraFactory" && e.Kind == "market_attachment" {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		return MarketCreation{}, fmt.Errorf("authoritative registered creation unresolved")
	}
	e := matches[0]
	nft, err := unsigned(e.Fields["nftId"])
	if err != nil {
		return MarketCreation{}, err
	}
	result := MarketCreation{TransactionHash: submitted, LogIndex: e.Identity.LogIndex, NFTID: nft}
	var ok bool
	result.Creator, ok = e.Fields["creator"].(common.Address)
	if !ok {
		return MarketCreation{}, fmt.Errorf("creator missing")
	}
	result.Token, ok = e.Fields["token"].(common.Address)
	if !ok {
		return MarketCreation{}, fmt.Errorf("token missing")
	}
	result.Pool, ok = e.Fields["pool"].(common.Address)
	if !ok {
		return MarketCreation{}, fmt.Errorf("pool missing")
	}
	result.Optical, ok = e.Fields["optical"].(common.Address)
	if !ok {
		return MarketCreation{}, fmt.Errorf("optical missing")
	}
	for _, record := range out.Records {
		if record.Identity == e.Identity {
			result.FactoryVersion = record.Decoded.Version
		}
	}
	return result, nil
}
