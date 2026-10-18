package control

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ManifestAuthority must attest the current deployment digest and custody authority.
// The production pending-beacon manifest does not meet release acceptance. A local
// fixture authority is usable only in isolated tests and is never release proof.
type AwardAuthority interface {
	VerifyAward(context.Context, Bounty, string, string) error
}
type ManifestAuthority interface {
	Current(context.Context, string, common.Address) error
}
type ChainVerifier struct {
	client    *ethclient.Client
	custody   common.Address
	manifest  string
	authority ManifestAuthority
	awards    AwardAuthority
}

func NewChainVerifier(client *ethclient.Client, custody, manifest string, authority ManifestAuthority, awards AwardAuthority) (*ChainVerifier, error) {
	if client == nil || !destination(custody) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(manifest) || authority == nil {
		return nil, Invalid
	}
	return &ChainVerifier{client: client, custody: common.HexToAddress(custody), manifest: manifest, authority: authority, awards: awards}, nil
}
func (v *ChainVerifier) pin(ctx context.Context) error {
	if e := v.authority.Current(ctx, v.manifest, v.custody); e != nil {
		return e
	}
	n, e := v.client.ChainID(ctx)
	if e != nil {
		return e
	}
	if n.Cmp(big.NewInt(125)) != 0 {
		return Denied
	}
	return nil
}

// reportedBlock returns the header at number (nil means latest) together with
// the block hash the node reports for it. On Paxeer (chain 125, CometBFT-backed)
// that reported hash is the block identity; go-ethereum's recomputed
// types.Header.Hash() names no block there.
func (v *ChainVerifier) reportedBlock(ctx context.Context, number *big.Int) (*types.Header, common.Hash, error) {
	tag := "latest"
	if number != nil {
		tag = hexutil.EncodeBig(number)
	}
	var raw json.RawMessage
	if e := v.client.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); e != nil {
		return nil, common.Hash{}, e
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, common.Hash{}, Unavailable
	}
	var h types.Header
	if e := json.Unmarshal(raw, &h); e != nil {
		return nil, common.Hash{}, e
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if e := json.Unmarshal(raw, &reported); e != nil {
		return nil, common.Hash{}, e
	}
	if reported.Hash == nil || *reported.Hash == (common.Hash{}) || h.Number == nil || h.Number.Sign() < 0 || (number != nil && h.Number.Cmp(number) != 0) {
		return nil, common.Hash{}, Denied
	}
	return &h, *reported.Hash, nil
}

// atReported proves the node reports hash for height h.Number.
func (v *ChainVerifier) atReported(ctx context.Context, h *types.Header, hash common.Hash) error {
	_, current, e := v.reportedBlock(ctx, h.Number)
	if e != nil {
		return e
	}
	if current != hash {
		return Denied
	}
	return nil
}

func (v *ChainVerifier) Inventory(ctx context.Context, a Asset) (Observation, error) {
	o := Observation{}
	if !a.Valid() {
		return o, Invalid
	}
	if e := v.pin(ctx); e != nil {
		return o, e
	}
	// Reads run by the pinned height with the node-reported hash checked
	// before and after, so the balance belongs to exactly the referenced block.
	h, reference, e := v.reportedBlock(ctx, nil)
	if e != nil {
		return o, e
	}
	if e = v.atReported(ctx, h, reference); e != nil {
		return o, e
	}
	var balance *big.Int
	if a.Address == NativePAX {
		balance, e = v.client.BalanceAt(ctx, v.custody, h.Number)
	} else {
		code, x := v.client.CodeAt(ctx, common.HexToAddress(a.Address), h.Number)
		if x != nil {
			return o, x
		}
		if len(code) == 0 {
			return o, Denied
		}
		data := append(common.FromHex("0x70a08231"), common.LeftPadBytes(v.custody.Bytes(), 32)...)
		raw, x := v.client.CallContract(ctx, ethereum.CallMsg{To: ptrAddress(a.Address), Data: data}, h.Number)
		if x != nil {
			return o, x
		}
		if len(raw) != 32 {
			return o, Denied
		}
		balance = new(big.Int).SetBytes(raw)
		decimals, x := v.client.CallContract(ctx, ethereum.CallMsg{To: ptrAddress(a.Address), Data: common.FromHex("0x313ce567")}, h.Number)
		if x != nil {
			return o, x
		}
		if len(decimals) != 32 || new(big.Int).SetBytes(decimals).Cmp(big.NewInt(int64(a.Decimals))) != 0 {
			return o, Denied
		}
	}
	if e != nil {
		return o, e
	}
	if e = v.atReported(ctx, h, reference); e != nil {
		return o, e
	}
	return Observation{Asset: a, Balance: balance.String(), Reference: reference.Hex(), Manifest: v.manifest, Source: strings.ToLower(v.custody.Hex()), At: time.Now().UTC()}, nil
}
func ptrAddress(s string) *common.Address { a := common.HexToAddress(s); return &a }
func (v *ChainVerifier) Settlement(ctx context.Context, o Obligation, reference string) (SettlementEvidence, error) {
	p := SettlementEvidence{}
	if !o.Asset.Valid() || !destination(o.Destination) || len(reference) != 66 || !strings.HasPrefix(reference, "0x") {
		return p, Invalid
	}
	raw := common.FromHex(reference)
	if len(raw) != 32 {
		return p, Invalid
	}
	if e := v.pin(ctx); e != nil {
		return p, e
	}
	hash := common.HexToHash(reference)
	r, e := v.client.TransactionReceipt(ctx, hash)
	if errors.Is(e, ethereum.NotFound) {
		return p, Unavailable
	}
	if e != nil {
		return p, e
	}
	if r.TxHash != hash || r.Status != types.ReceiptStatusSuccessful || r.BlockNumber == nil {
		return p, Denied
	}
	// The receipt names the node-reported block hash; the node must still
	// report it for the receipt's height (canonical inclusion).
	_, reported, e := v.reportedBlock(ctx, r.BlockNumber)
	if e != nil {
		return p, e
	}
	if reported != r.BlockHash {
		return p, Denied
	}
	n, e := amount(o.Amount)
	if e != nil {
		return p, e
	}
	if o.Asset.Address == NativePAX {
		tx, pending, e := v.client.TransactionByHash(ctx, hash)
		if e != nil {
			return p, e
		}
		if pending || tx.Hash() != hash || tx.To() == nil || *tx.To() != common.HexToAddress(o.Destination) || tx.Value().Cmp(n) != 0 || tx.ChainId().Cmp(big.NewInt(125)) != 0 {
			return p, Denied
		}
		from, e := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), tx)
		if e != nil || from != v.custody {
			return p, Denied
		}
	} else {
		matched := false
		for _, log := range r.Logs {
			if log.Removed || log.TxHash != hash || log.BlockHash != r.BlockHash || log.Address != common.HexToAddress(o.Asset.Address) || len(log.Topics) != 3 || log.Topics[0] != common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef") {
				continue
			}
			if log.Topics[1] != common.BytesToHash(common.LeftPadBytes(v.custody.Bytes(), 32)) || log.Topics[2] != common.BytesToHash(common.LeftPadBytes(common.HexToAddress(o.Destination).Bytes(), 32)) || len(log.Data) != 32 {
				continue
			}
			if new(big.Int).SetBytes(log.Data).Cmp(n) == 0 {
				matched = true
			}
		}
		if !matched {
			return p, Denied
		}
	}
	return SettlementEvidence{Reference: reference, Manifest: v.manifest, Asset: o.Asset, Amount: o.Amount, Destination: o.Destination, Source: strings.ToLower(v.custody.Hex()), Successful: true}, nil
}

func (v *ChainVerifier) Award(ctx context.Context, b Bounty, evidence, dest string) error {
	if v.awards == nil {
		return Unavailable
	}
	if b.FundingSource != strings.ToLower(v.custody.Hex()) {
		return Denied
	}
	if e := v.pin(ctx); e != nil {
		return e
	}
	return v.awards.VerifyAward(ctx, b, evidence, dest)
}
