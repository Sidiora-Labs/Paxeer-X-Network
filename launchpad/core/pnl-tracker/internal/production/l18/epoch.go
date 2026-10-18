package l18

import (
	"context"
	"encoding/json"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"math/big"
	"strings"
	"time"
)

type NativeEvent struct {
	Identity string     `json:"identity"`
	TxHash   string     `json:"txHash"`
	LogIndex uint64     `json:"logIndex"`
	Block    Checkpoint `json:"block"`
	Values   []any      `json:"-"`
}

func (c *Creator) nativeEvent(ctx context.Context, txHash string, event abi.Event, topics []string) (NativeEvent, error) {
	if !hash.MatchString(txHash) {
		return NativeEvent{}, ErrInvalid
	}
	frontier, checkpointErr := c.checkpoint(ctx)
	if checkpointErr != nil {
		return NativeEvent{}, checkpointErr
	}
	var raw json.RawMessage
	if e := c.rpc.CallContext(ctx, &raw, "eth_getTransactionReceipt", txHash); e != nil {
		return NativeEvent{}, e
	}
	if len(raw) == 0 || string(raw) == "null" {
		return NativeEvent{}, ErrUnavailable
	}
	var r struct {
		Status    hexutil.Uint64 `json:"status"`
		TxHash    string         `json:"transactionHash"`
		BlockHash string         `json:"blockHash"`
		Number    hexutil.Uint64 `json:"blockNumber"`
		Logs      []struct {
			Address string         `json:"address"`
			Topics  []string       `json:"topics"`
			Data    hexutil.Bytes  `json:"data"`
			Index   hexutil.Uint64 `json:"logIndex"`
			Removed bool           `json:"removed"`
		} `json:"logs"`
	}
	if e := json.Unmarshal(raw, &r); e != nil {
		return NativeEvent{}, e
	}
	if r.Status != 1 || r.TxHash != txHash || !hash.MatchString(r.BlockHash) || uint64(r.Number) > frontier.Number {
		return NativeEvent{}, ErrUnavailable
	}
	var h struct {
		Hash string `json:"hash"`
	}
	if e := c.rpc.CallContext(ctx, &h, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(r.Number)), false); e != nil {
		return NativeEvent{}, e
	}
	if h.Hash != r.BlockHash {
		return NativeEvent{}, ErrUnavailable
	}
	var found *NativeEvent
	for _, log := range r.Logs {
		if log.Removed || strings.ToLower(log.Address) != Accumulator || len(log.Topics) != len(topics)+1 || log.Topics[0] != event.ID.Hex() {
			continue
		}
		match := true
		for i, t := range topics {
			if log.Topics[i+1] != t {
				match = false
			}
		}
		if !match {
			continue
		}
		if found != nil {
			return NativeEvent{}, ErrConflict
		}
		v, e := event.Inputs.NonIndexed().Unpack(log.Data)
		if e != nil {
			return NativeEvent{}, e
		}
		ev := NativeEvent{Identity: "125:" + txHash + ":" + new(big.Int).SetUint64(uint64(log.Index)).String(), TxHash: txHash, LogIndex: uint64(log.Index), Block: Checkpoint{uint64(r.Number), r.BlockHash}, Values: v}
		found = &ev
	}
	if found == nil {
		return NativeEvent{}, ErrUnavailable
	}
	return *found, nil
}
func addressTopic(w string) string { return common.BytesToHash(common.HexToAddress(w).Bytes()).Hex() }

type Epoch struct {
	Pool              string     `json:"pool"`
	ID                string     `json:"epoch"`
	SnapshotBlock     uint64     `json:"snapshotBlock"`
	FundedRaw         string     `json:"fundedRaw"`
	EligibleSupplyRaw string     `json:"eligibleSupplyRaw"`
	Block             Checkpoint `json:"block"`
	Provenance        string     `json:"provenance"`
	Version           string     `json:"version"`
}

// RegisterEpoch reads snapshot authority at the native trigger receipt's block.
// The contract defines no epoch deadline; no off-chain deadline is invented.
func (l *Ledger) RegisterEpoch(ctx context.Context, c *Creator, txHash, pool string) (Epoch, error) {
	pool, e := wallet(pool)
	if e != nil {
		return Epoch{}, e
	}
	if c == nil {
		return Epoch{}, ErrUnavailable
	}
	ev, e := c.nativeEvent(ctx, txHash, accumulator.Events["AirdropTriggered"], []string{addressTopic(pool)})
	if e != nil {
		return Epoch{}, e
	}
	if len(ev.Values) != 2 {
		return Epoch{}, ErrInvalid
	}
	epoch := ev.Values[1].(*big.Int)
	p := common.HexToAddress(pool)
	support, e := c.read(ctx, ev.Block, Accumulator, accumulator, "supportsAirdrop", p)
	if e != nil {
		return Epoch{}, e
	}
	if !support.(bool) {
		return Epoch{}, ErrUnavailable
	}
	snap, e := c.read(ctx, ev.Block, Accumulator, accumulator, "airdropSnapshotBlock", p, epoch)
	if e != nil {
		return Epoch{}, e
	}
	supply, e := c.read(ctx, ev.Block, Accumulator, accumulator, "airdropEligibleSupply", p, epoch)
	if e != nil {
		return Epoch{}, e
	}
	if !snap.(*big.Int).IsUint64() || supply.(*big.Int).Sign() == 0 || epoch.Sign() == 0 {
		return Epoch{}, ErrUnavailable
	}
	out := Epoch{pool, epoch.String(), snap.(*big.Int).Uint64(), ev.Values[0].(*big.Int).String(), supply.(*big.Int).String(), ev.Block, ev.Identity, RuleVersion}
	tx, e := l.begin(ctx)
	if e != nil {
		return Epoch{}, e
	}
	defer tx.Rollback(ctx)
	dup, e := inbox(ctx, tx, "epoch:"+ev.Identity, out)
	if e != nil {
		return Epoch{}, e
	}
	if dup {
		return out, tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_epochs(pool,epoch,snapshot_block,funded,eligible_supply,version,checkpoint_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, pool, out.ID, out.SnapshotBlock, out.FundedRaw, out.EligibleSupplyRaw, out.Version, out.Block.Hash)
	if e != nil {
		return Epoch{}, e
	}
	if e = emit(ctx, tx, "epoch:"+ev.Identity, "airdrop.epoch", out); e != nil {
		return Epoch{}, e
	}
	return out, tx.Commit(ctx)
}

type EpochClaim struct {
	Pool        string     `json:"pool"`
	Epoch       string     `json:"epoch"`
	Beneficiary string     `json:"beneficiary"`
	Raw         string     `json:"raw"`
	Asset       string     `json:"asset"`
	Provenance  string     `json:"provenance"`
	Block       Checkpoint `json:"block"`
	Version     string     `json:"version"`
}

func (l *Ledger) RecordEpochClaim(ctx context.Context, c *Creator, txHash, pool, beneficiary, epoch string) (EpochClaim, error) {
	pool, e := wallet(pool)
	if e != nil {
		return EpochClaim{}, e
	}
	beneficiary, e = wallet(beneficiary)
	if e != nil {
		return EpochClaim{}, e
	}
	n, e := amount(epoch)
	if e != nil || n.Sign() == 0 {
		return EpochClaim{}, ErrInvalid
	}
	if c == nil {
		return EpochClaim{}, ErrUnavailable
	}
	ev, e := c.nativeEvent(ctx, txHash, accumulator.Events["AirdropClaimed"], []string{addressTopic(pool), addressTopic(beneficiary)})
	if e != nil {
		return EpochClaim{}, e
	}
	if len(ev.Values) != 2 || ev.Values[1].(*big.Int).Cmp(n) != 0 {
		return EpochClaim{}, ErrInvalid
	}
	out := EpochClaim{pool, epoch, beneficiary, ev.Values[0].(*big.Int).String(), USDL, ev.Identity, ev.Block, RuleVersion}
	transfer, e := c.tokenTransfer(ctx, txHash, Accumulator, beneficiary)
	if e != nil {
		return EpochClaim{}, e
	}
	if transfer.Raw != out.Raw || transfer.Block != out.Block {
		return EpochClaim{}, ErrConflict
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return EpochClaim{}, e
	}
	defer tx.Rollback(ctx)
	dup, e := inbox(ctx, tx, "epochclaim:"+ev.Identity, out)
	if e != nil {
		return EpochClaim{}, e
	}
	if dup {
		return out, tx.Commit(ctx)
	}
	var funded, claimed string
	e = tx.QueryRow(ctx, `SELECT funded::text,claimed::text FROM l18_epochs WHERE pool=$1 AND epoch=$2 FOR UPDATE`, pool, epoch).Scan(&funded, &claimed)
	if e != nil {
		return EpochClaim{}, e
	}
	f, err := amount(funded)
	if err != nil {
		return EpochClaim{}, ErrConflict
	}
	old, err := amount(claimed)
	if err != nil {
		return EpochClaim{}, ErrConflict
	}
	raw, err := amount(out.Raw)
	if err != nil {
		return EpochClaim{}, ErrConflict
	}
	if new(big.Int).Add(old, raw).Cmp(f) > 0 {
		return EpochClaim{}, ErrConflict
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_epoch_claims(pool,epoch,beneficiary,identity,raw) VALUES($1,$2,$3,$4,$5)`, pool, epoch, beneficiary, ev.Identity, out.Raw)
	if e != nil {
		return EpochClaim{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE l18_epochs SET claimed=claimed+$3::numeric WHERE pool=$1 AND epoch=$2`, pool, epoch, out.Raw)
	if e != nil {
		return EpochClaim{}, e
	}
	if e = emit(ctx, tx, "epochclaim:"+ev.Identity, "airdrop.claimed", out); e != nil {
		return EpochClaim{}, e
	}
	return out, tx.Commit(ctx)
}

var checkpointToken = parseABI(`[{"type":"function","name":"getPastBalance","inputs":[{"name":"holder","type":"address"},{"name":"blockNumber","type":"uint256"}],"outputs":[{"name":"balance","type":"uint256"}],"stateMutability":"view"}]`)
var poolToken = parseABI(`[{"type":"function","name":"tokenAddress","inputs":[],"outputs":[{"name":"token","type":"address"}],"stateMutability":"view"}]`)

type Entitlement struct {
	Pool           string     `json:"pool"`
	Epoch          string     `json:"epoch"`
	Beneficiary    string     `json:"beneficiary"`
	SnapshotBlock  uint64     `json:"snapshotBlock"`
	FundedRaw      string     `json:"fundedRaw"`
	ClaimedRaw     string     `json:"claimedRaw"`
	ClaimableRaw   string     `json:"claimableRaw"`
	AlreadyClaimed bool       `json:"alreadyClaimed"`
	Asset          string     `json:"asset"`
	Block          Checkpoint `json:"block"`
	Version        string     `json:"version"`
}

// EpochEntitlement uses the registered native trigger amount because the
// contract's original epoch amount is private. Snapshot balance, supply,
// remaining and claimed flags are read at one canonical committed checkpoint.
func (l *Ledger) EpochEntitlement(ctx context.Context, c *Creator, pool, epoch, beneficiary string) (Entitlement, error) {
	pool, e := wallet(pool)
	if e != nil {
		return Entitlement{}, e
	}
	beneficiary, e = wallet(beneficiary)
	if e != nil {
		return Entitlement{}, e
	}
	if beneficiary == pool || beneficiary == "0x000000000000000000000000000000000000dead" {
		return Entitlement{}, ErrUnauthorized
	}
	n, e := amount(epoch)
	if e != nil || n.Sign() == 0 {
		return Entitlement{}, ErrInvalid
	}
	if c == nil {
		return Entitlement{}, ErrUnavailable
	}
	tx, e := l.begin(ctx)
	if e != nil {
		return Entitlement{}, e
	}
	defer tx.Rollback(ctx)
	var funded, supply string
	var snapshot uint64
	e = tx.QueryRow(ctx, `SELECT funded::text,eligible_supply::text,snapshot_block FROM l18_epochs WHERE pool=$1 AND epoch=$2`, pool, epoch).Scan(&funded, &supply, &snapshot)
	if e != nil {
		return Entitlement{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Entitlement{}, e
	}
	b, e := c.checkpoint(ctx)
	if e != nil {
		return Entitlement{}, e
	}
	if snapshot >= b.Number {
		return Entitlement{}, ErrUnavailable
	}
	p := common.HexToAddress(pool)
	support, e := c.read(ctx, b, Accumulator, accumulator, "supportsAirdrop", p)
	if e != nil {
		return Entitlement{}, e
	}
	if !support.(bool) {
		return Entitlement{}, ErrUnavailable
	}
	remaining, e := c.read(ctx, b, Accumulator, accumulator, "airdropEpochRemaining", p, n)
	if e != nil {
		return Entitlement{}, e
	}
	claimedSupply, e := c.read(ctx, b, Accumulator, accumulator, "airdropClaimedSupply", p, n)
	if e != nil {
		return Entitlement{}, e
	}
	claimed, e := c.read(ctx, b, Accumulator, accumulator, "hasClaimedAirdrop", p, common.HexToAddress(beneficiary), n)
	if e != nil {
		return Entitlement{}, e
	}
	token, e := c.read(ctx, b, pool, poolToken, "tokenAddress")
	if e != nil {
		return Entitlement{}, e
	}
	balance, e := c.read(ctx, b, strings.ToLower(token.(common.Address).Hex()), checkpointToken, "getPastBalance", common.HexToAddress(beneficiary), new(big.Int).SetUint64(snapshot))
	if e != nil {
		return Entitlement{}, e
	}
	raw, e := EpochShare(funded, remaining.(*big.Int).String(), supply, claimedSupply.(*big.Int).String(), balance.(*big.Int).String(), claimed.(bool))
	if e != nil {
		return Entitlement{}, e
	}
	f, _ := amount(funded)
	paid := new(big.Int).Sub(f, remaining.(*big.Int))
	return Entitlement{pool, epoch, beneficiary, snapshot, funded, paid.String(), raw, claimed.(bool), USDL, b, RuleVersion}, nil
}

type PreparedEpochCall struct {
	ChainID     int         `json:"chainId"`
	To          string      `json:"to"`
	Caller      string      `json:"caller"`
	Data        string      `json:"data"`
	Value       string      `json:"value"`
	ExpiresAt   int64       `json:"expiresAt"`
	Entitlement Entitlement `json:"entitlement"`
}

// PrepareEpochClaim checks checkpoint eligibility, never NFT ownership. The
// contract remains the final authorizer; expiry is a server preview TTL only.
func (l *Ledger) PrepareEpochClaim(ctx context.Context, c *Creator, nftID, epoch, caller string) (PreparedEpochCall, error) {
	if c == nil || c.now == nil {
		return PreparedEpochCall{}, ErrUnavailable
	}
	id, e := amount(nftID)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	n, e := amount(epoch)
	if e != nil || n.Sign() == 0 {
		return PreparedEpochCall{}, ErrInvalid
	}
	who, e := wallet(caller)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	if c == nil {
		return PreparedEpochCall{}, ErrUnavailable
	}
	b, e := c.checkpoint(ctx)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	p, e := c.read(ctx, b, NFT, nft, "getPoolAddress", id)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	ent, e := l.EpochEntitlement(ctx, c, strings.ToLower(p.(common.Address).Hex()), epoch, who)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	if ent.AlreadyClaimed || ent.ClaimableRaw == "0" {
		return PreparedEpochCall{}, ErrUnavailable
	}
	data, e := router.Pack("claimAirdropEpoch", id, n)
	if e != nil {
		return PreparedEpochCall{}, e
	}
	return PreparedEpochCall{125, FeesRouter, who, hexutil.Encode(data), "0", c.now().Add(2 * time.Minute).Unix(), ent}, nil
}
