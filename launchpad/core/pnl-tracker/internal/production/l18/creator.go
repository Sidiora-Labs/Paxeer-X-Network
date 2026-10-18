package l18

import (
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Frozen ABI copies retain exact provenance in the lane handoff.
//
//go:embed FeesRouter.json
var routerABI string

//go:embed FeeAccumulator.json
var accumulatorABI string

//go:embed SidioraNFT.json
var nftABI string

func parseABI(s string) abi.ABI {
	a, e := abi.JSON(strings.NewReader(s))
	if e != nil {
		panic(e)
	}
	return a
}

var router = parseABI(routerABI)
var accumulator = parseABI(accumulatorABI)
var nft = parseABI(nftABI)

// RPC is implemented by go-ethereum rpc.Client. Integrator must supply a trusted
// chain125 transport; no request data may select the endpoint or deployment.
type RPC interface {
	CallContext(context.Context, any, string, ...any) error
}
type Checkpoint struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}
type Creator struct {
	rpc       RPC
	now       func() time.Time
	committed func(context.Context) (Checkpoint, error)
	attest    func(context.Context, Checkpoint) error
}

func NewCreator(r RPC, now func() time.Time) *Creator { return &Creator{rpc: r, now: now} }
func (c *Creator) checkpoint(ctx context.Context) (Checkpoint, error) {
	if c == nil || c.rpc == nil {
		return Checkpoint{}, ErrUnavailable
	}
	var chain hexutil.Big
	if e := c.rpc.CallContext(ctx, &chain, "eth_chainId"); e != nil {
		return Checkpoint{}, e
	}
	if (*big.Int)(&chain).Cmp(big.NewInt(125)) != 0 {
		return Checkpoint{}, ErrUnavailable
	}
	if c.committed != nil {
		b, err := c.committed(ctx)
		if err != nil || !hash.MatchString(b.Hash) {
			return Checkpoint{}, ErrUnavailable
		}
		var header struct {
			Hash   string         `json:"hash"`
			Number hexutil.Uint64 `json:"number"`
		}
		if err = c.rpc.CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(b.Number), false); err != nil {
			return Checkpoint{}, err
		}
		if header.Hash != b.Hash || uint64(header.Number) != b.Number {
			return Checkpoint{}, ErrUnavailable
		}
		if c.attest != nil {
			if err = c.attest(ctx, b); err != nil {
				return Checkpoint{}, err
			}
		}
		return b, nil
	}
	var h struct {
		Number hexutil.Uint64 `json:"number"`
		Hash   string         `json:"hash"`
	}
	if e := c.rpc.CallContext(ctx, &h, "eth_getBlockByNumber", "latest", false); e != nil {
		return Checkpoint{}, e
	}
	if !hash.MatchString(h.Hash) {
		return Checkpoint{}, ErrUnavailable
	}
	return Checkpoint{uint64(h.Number), h.Hash}, nil
}
func (c *Creator) read(ctx context.Context, b Checkpoint, target string, a abi.ABI, method string, args ...any) (any, error) {
	data, e := a.Pack(method, args...)
	if e != nil {
		return nil, e
	}
	var result hexutil.Bytes
	e = c.rpc.CallContext(ctx, &result, "eth_call", map[string]any{"to": target, "data": hexutil.Encode(data)}, map[string]any{"blockHash": b.Hash, "requireCanonical": true})
	if e != nil {
		return nil, e
	}
	v, e := a.Unpack(method, result)
	if e != nil || len(v) != 1 {
		return nil, ErrUnavailable
	}
	return v[0], nil
}

type FeePreview struct {
	NFTID             string     `json:"nftId"`
	Owner             string     `json:"owner"`
	Pool              string     `json:"pool"`
	HistoricalCreator string     `json:"historicalCreator"`
	ClaimableRaw      string     `json:"claimableRaw"`
	Asset             string     `json:"asset"`
	Capability        Capability `json:"capability"`
	Block             Checkpoint `json:"block"`
	Version           string     `json:"version"`
}

func (c *Creator) Preview(ctx context.Context, id, displayCreator string) (FeePreview, error) {
	n, e := amount(id)
	if e != nil {
		return FeePreview{}, e
	}
	b, e := c.checkpoint(ctx)
	if e != nil {
		return FeePreview{}, e
	}
	owner, e := c.read(ctx, b, NFT, nft, "ownerOf", n)
	if e != nil {
		return FeePreview{}, e
	}
	pool, e := c.read(ctx, b, NFT, nft, "getPoolAddress", n)
	if e != nil {
		return FeePreview{}, e
	}
	strategy, e := c.read(ctx, b, NFT, nft, "getFeeStrategy", n)
	if e != nil {
		return FeePreview{}, e
	}
	p := pool.(common.Address)
	support, e := c.read(ctx, b, Accumulator, accumulator, "supportsAirdrop", p)
	if e != nil {
		return FeePreview{}, e
	}
	raw, e := c.read(ctx, b, Accumulator, accumulator, "getAccumulatedFees", p)
	if e != nil {
		return FeePreview{}, e
	}
	return FeePreview{NFTID: id, Owner: strings.ToLower(owner.(common.Address).Hex()), Pool: strings.ToLower(p.Hex()), HistoricalCreator: displayCreator, ClaimableRaw: raw.(*big.Int).String(), Asset: USDL, Capability: StrategyCapability(strategy.(uint8), support.(bool)), Block: b, Version: RuleVersion}, nil
}

type PreparedCall struct {
	ChainID   int        `json:"chainId"`
	To        string     `json:"to"`
	Caller    string     `json:"caller"`
	Data      string     `json:"data"`
	Value     string     `json:"value"`
	ExpiresAt int64      `json:"expiresAt"`
	Preview   FeePreview `json:"preview"`
}

func (c *Creator) PrepareClaim(ctx context.Context, id, caller, displayCreator string) (PreparedCall, error) {
	if c == nil || c.now == nil {
		return PreparedCall{}, ErrUnavailable
	}
	who, e := wallet(caller)
	if e != nil {
		return PreparedCall{}, e
	}
	p, e := c.Preview(ctx, id, displayCreator)
	if e != nil {
		return PreparedCall{}, e
	}
	if who != p.Owner {
		return PreparedCall{}, ErrUnauthorized
	}
	if !p.Capability.Available || p.Capability.Strategy != "CLAIM" || p.ClaimableRaw == "0" {
		return PreparedCall{}, ErrUnavailable
	}
	n, _ := amount(id)
	data, e := router.Pack("claimFees", n)
	if e != nil {
		return PreparedCall{}, e
	}
	return PreparedCall{125, FeesRouter, who, hexutil.Encode(data), "0", c.now().Add(2 * time.Minute).Unix(), p}, nil
}

// Native claim evidence is decoded from the accumulator, not its router/mirror.
// Raw JSON is accepted only from the trusted receipt RPC and rechecked against
// an exact canonical block; a submitted hash or elapsed timer has no effect.
type ClaimEvidence struct {
	Identity    string     `json:"identity"`
	Pool        string     `json:"pool"`
	Beneficiary string     `json:"beneficiary"`
	Raw         string     `json:"raw"`
	Asset       string     `json:"asset"`
	Block       Checkpoint `json:"block"`
	TxHash      string     `json:"txHash"`
	LogIndex    uint64     `json:"logIndex"`
	Version     string     `json:"version"`
}

func (c *Creator) ClaimReceipt(ctx context.Context, txHash, pool, beneficiary string) (ClaimEvidence, error) {
	if !hash.MatchString(txHash) {
		return ClaimEvidence{}, ErrInvalid
	}
	pool, e := wallet(pool)
	if e != nil {
		return ClaimEvidence{}, e
	}
	beneficiary, e = wallet(beneficiary)
	if e != nil {
		return ClaimEvidence{}, e
	}
	// checkpoint also enforces chain125, without confirmation delays.
	frontier, e := c.checkpoint(ctx)
	if e != nil {
		return ClaimEvidence{}, e
	}
	var raw json.RawMessage
	if e = c.rpc.CallContext(ctx, &raw, "eth_getTransactionReceipt", txHash); e != nil {
		return ClaimEvidence{}, e
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ClaimEvidence{}, ErrUnavailable
	}
	var r struct {
		Status      hexutil.Uint64 `json:"status"`
		Hash        string         `json:"transactionHash"`
		BlockHash   string         `json:"blockHash"`
		BlockNumber hexutil.Uint64 `json:"blockNumber"`
		Logs        []struct {
			Address string         `json:"address"`
			Topics  []string       `json:"topics"`
			Data    string         `json:"data"`
			Index   hexutil.Uint64 `json:"logIndex"`
			Removed bool           `json:"removed"`
		} `json:"logs"`
	}
	if e = json.Unmarshal(raw, &r); e != nil {
		return ClaimEvidence{}, e
	}
	if r.Hash != txHash || !hash.MatchString(r.BlockHash) || uint64(r.BlockNumber) > frontier.Number {
		return ClaimEvidence{}, ErrInvalid
	}
	if r.Status != 1 {
		return ClaimEvidence{}, ErrUnavailable
	}
	var canonical struct {
		Hash string `json:"hash"`
	}
	if e = c.rpc.CallContext(ctx, &canonical, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(r.BlockNumber)), false); e != nil {
		return ClaimEvidence{}, e
	}
	if canonical.Hash != r.BlockHash {
		return ClaimEvidence{}, ErrUnavailable
	}
	event := accumulator.Events["FeesClaimed"]
	var found *ClaimEvidence
	for _, l := range r.Logs {
		if strings.ToLower(l.Address) != Accumulator || l.Removed || len(l.Topics) != 3 || l.Topics[0] != event.ID.Hex() {
			continue
		}
		if l.Topics[1] != common.BytesToHash(common.HexToAddress(pool).Bytes()).Hex() || l.Topics[2] != common.BytesToHash(common.HexToAddress(beneficiary).Bytes()).Hex() {
			continue
		}
		data, e := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
		if e != nil {
			return ClaimEvidence{}, ErrInvalid
		}
		v, e := event.Inputs.NonIndexed().Unpack(data)
		if e != nil || len(v) != 1 {
			return ClaimEvidence{}, ErrInvalid
		}
		if found != nil {
			return ClaimEvidence{}, ErrConflict
		}
		out := ClaimEvidence{fmt.Sprintf("125:%s:%d", txHash, l.Index), pool, beneficiary, v[0].(*big.Int).String(), USDL, Checkpoint{uint64(r.BlockNumber), r.BlockHash}, txHash, uint64(l.Index), RuleVersion}
		found = &out
	}
	if found == nil {
		return ClaimEvidence{}, ErrUnavailable
	}
	transfer, e := c.tokenTransfer(ctx, txHash, Accumulator, beneficiary)
	if e != nil {
		return ClaimEvidence{}, e
	}
	if transfer.Raw != found.Raw || transfer.Block != found.Block {
		return ClaimEvidence{}, ErrConflict
	}
	return *found, nil
}
