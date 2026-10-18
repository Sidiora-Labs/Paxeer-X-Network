package receipt

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum/common"
	types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var ErrQualification = errors.New("receipt qualification rejected")
var ErrBinding = errors.New("qualified receipt binding mismatch")

const maxBundleBytes = 32 << 20

type Bundle struct {
	Receipt        *types.Receipt
	Transaction    *types.Transaction
	Header         *types.Header
	PrepayReceipts []*types.Receipt
	// BlockHash is the authority-issued identity of the containing block. On
	// Ethereum-hash chains it equals Header.Hash(); on CometBFT-backed chains it
	// is the node-reported consensus hash. Zero means Header.Hash().
	BlockHash common.Hash `json:",omitempty"`
}
type StablePoolMetadata struct {
	Pool, Token, Settlement                                    common.Address
	TokenDecimals, SettlementDecimals                          uint8
	DeploymentID, ABIRevision, EmitterCodeEpoch, PoolCodeEpoch string
	BlockHash                                                  common.Hash
}
type Qualification interface {
	ValidateReceipt(context.Context, Bundle) error
	StableMetadata(context.Context, common.Address, common.Hash) (StablePoolMetadata, error)
	ExecutionPolicy(context.Context, Bundle, uint32) (accounting.ExecutionPolicy, error)
}
type provenanceQualification interface {
	SourceProvenance(context.Context, Bundle, common.Address) (accounting.Provenance, error)
}
type issuance struct {
	nonce   [32]byte
	digest  [32]byte
	receipt *QualifiedReceipt
}
type QualifiedReceipt struct {
	issuer     *issuance
	bundle     Bundle
	sender     string
	fills      []accounting.FillV1
	events     []*decode.Event
	envelopes  []*decode.DecodedEnvelope
	provenance map[string]accounting.Provenance
	selected   map[uint32]bool
}

func digest(v any) ([32]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > maxBundleBytes {
		return [32]byte{}, ErrQualification
	}
	return sha256.Sum256(raw), nil
}
func clone[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > maxBundleBytes {
		return out, ErrQualification
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
func (q *QualifiedReceipt) seal() ([32]byte, error) {
	if q == nil || q.issuer == nil || q.issuer.receipt != q || q.issuer.nonce == ([32]byte{}) {
		return [32]byte{}, ErrQualification
	}
	return digest(struct {
		Nonce      [32]byte
		Bundle     Bundle
		Sender     string
		Fills      []accounting.FillV1
		Events     []*decode.Event
		Envelopes  []*decode.DecodedEnvelope
		Provenance map[string]accounting.Provenance
		Selected   map[uint32]bool
	}{q.issuer.nonce, q.bundle, q.sender, q.fills, q.events, q.envelopes, q.provenance, q.selected})
}
func (q *QualifiedReceipt) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := q.seal()
	if err != nil || h != q.issuer.digest {
		return ErrQualification
	}
	return nil
}
func boundedBundle(b Bundle) error {
	if b.Receipt == nil || b.Transaction == nil || b.Header == nil || len(b.PrepayReceipts) > 4096 || len(b.Header.Extra) > decode.MaxFieldBytes || len(b.Transaction.Data()) > maxBundleBytes {
		return ErrQualification
	}
	used := len(b.Header.Extra) + len(b.Transaction.Data())
	count := 0
	receipts := append([]*types.Receipt{b.Receipt}, b.PrepayReceipts...)
	for _, r := range receipts {
		if r == nil || len(r.Logs) > 4096 {
			return ErrQualification
		}
		for _, l := range r.Logs {
			if l == nil || len(l.Topics) > 4 || len(l.Data) > decode.MaxFieldBytes {
				return ErrQualification
			}
			count++
			used += 256 + len(l.Data) + 32*len(l.Topics)
			if count > 65536 || used > maxBundleBytes {
				return ErrQualification
			}
		}
	}
	return nil
}
func coordinates(b Bundle) (string, error) {
	r, t, h := b.Receipt, b.Transaction, b.Header
	if r == nil || t == nil || h == nil || h.Number == nil || !h.Number.IsInt64() || h.Number.Sign() < 0 || h.Time == 0 || h.Time > math.MaxInt64/1000 || r.BlockNumber == nil || !r.BlockNumber.IsInt64() || r.Status != types.ReceiptStatusSuccessful || r.Type != t.Type() || r.TransactionIndex > math.MaxUint32 || len(r.Logs) > 4096 || len(b.PrepayReceipts) > 4096 {
		return "", ErrQualification
	}
	if t.ChainId().Cmp(big.NewInt(125)) != 0 || r.TxHash != t.Hash() || r.BlockHash != bundleBlockHash(b) || r.BlockNumber.Cmp(h.Number) != 0 {
		return "", ErrQualification
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), t)
	if err != nil || sender == (common.Address{}) {
		return "", ErrQualification
	}
	var prior uint
	for i, l := range r.Logs {
		if l == nil || l.Address == (common.Address{}) || l.Removed || l.BlockHash != r.BlockHash || l.TxHash != r.TxHash || l.BlockNumber != h.Number.Uint64() || l.TxIndex != r.TransactionIndex || l.Index > math.MaxUint32 || len(l.Topics) > 4 || len(l.Data) > decode.MaxFieldBytes || (i > 0 && l.Index <= prior) {
			return "", ErrQualification
		}
		prior = l.Index
	}
	seen := map[common.Hash]bool{r.TxHash: true}
	for _, pre := range b.PrepayReceipts {
		if pre == nil || pre.Status != types.ReceiptStatusSuccessful || pre.TxHash == (common.Hash{}) || seen[pre.TxHash] || pre.BlockNumber == nil || !pre.BlockNumber.IsInt64() || pre.BlockNumber.Sign() < 0 || pre.BlockNumber.Cmp(h.Number) > 0 || pre.TransactionIndex > math.MaxUint32 || len(pre.Logs) > 4096 {
			return "", ErrQualification
		}
		seen[pre.TxHash] = true
		if pre.BlockNumber.Cmp(h.Number) == 0 && (pre.BlockHash != bundleBlockHash(b) || pre.TransactionIndex >= r.TransactionIndex) {
			return "", ErrQualification
		}
		for _, l := range pre.Logs {
			if l == nil || l.Removed || l.BlockHash != pre.BlockHash || l.BlockNumber != pre.BlockNumber.Uint64() || l.TxHash != pre.TxHash || l.TxIndex != pre.TransactionIndex || l.Index > math.MaxUint32 || len(l.Topics) > 4 || len(l.Data) > decode.MaxFieldBytes {
				return "", ErrQualification
			}
		}
	}
	return strings.ToLower(sender.Hex()), nil
}
func Qualify(ctx context.Context, r *decode.Registry, a Qualification, b Bundle) (*QualifiedReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || a == nil {
		return nil, ErrQualification
	}
	if err := boundedBundle(b); err != nil {
		return nil, err
	}
	owned, err := clone(b)
	if err != nil {
		return nil, err
	}
	sender, err := coordinates(owned)
	if err != nil {
		return nil, err
	}
	authoritative, err := clone(owned)
	if err != nil {
		return nil, err
	}
	before, err := digest(authoritative)
	if err != nil {
		return nil, err
	}
	if err = a.ValidateReceipt(ctx, authoritative); err != nil {
		return nil, fmt.Errorf("%w: authority: %v", ErrQualification, err)
	}
	after, err := digest(authoritative)
	if err != nil || before != after {
		return nil, ErrQualification
	}
	q := &QualifiedReceipt{bundle: owned, sender: sender, fills: []accounting.FillV1{}, events: []*decode.Event{}, envelopes: []*decode.DecodedEnvelope{}, provenance: map[string]accounting.Provenance{}, selected: map[uint32]bool{}}
	decoded := make([]*decode.DecodedEnvelope, len(owned.Receipt.Logs))
	for i, l := range owned.Receipt.Logs {
		env, err := r.Decode(*l)
		if err != nil {
			return nil, fmt.Errorf("%w: decode log %d: %v", ErrQualification, l.Index, err)
		}
		decoded[i] = env
		if env.Status == "decoded" && env.Event != nil {
			q.envelopes = append(q.envelopes, env)
			q.selected[uint32(l.Index)] = true
			if env.Family != nil && (env.Family.Authority == "primary" || env.Family.Authority == "native") {
				q.events = append(q.events, env.Event)
			}
			if authority, ok := a.(provenanceQualification); ok {
				key := strings.ToLower(l.Address.Hex())
				if _, exists := q.provenance[key]; !exists {
					proof, err := authority.SourceProvenance(ctx, owned, l.Address)
					if err != nil {
						return nil, err
					}
					proof, err = clone(proof)
					if err != nil || proof.DeploymentID == "" || proof.ABIRevision != env.ABIRevision || proof.EmitterCodeEpoch == "" {
						return nil, ErrQualification
					}
					q.provenance[key] = proof
				}
			}
		}
	}
	receiptDigest, _ := digest(owned.Receipt)
	headerDigest, _ := digest(owned.Header)
	for i, env := range decoded {
		if env.Family == nil || !env.Family.EconomicFill {
			continue
		}
		if env.EmitterRole != "EventEmitter" || env.EventName != "Swap" || env.Family.Authority != "primary" {
			return nil, ErrQualification
		}
		l := owned.Receipt.Logs[i]
		f, err := makeFill(ctx, a, owned, env, l, sender, receiptDigest, headerDigest)
		if err != nil {
			return nil, err
		}
		f.Attribution = attribute(owned, decoded, f, sender, receiptDigest)
		if err = accounting.ValidateFillV1(f); err != nil {
			return nil, fmt.Errorf("%w: fill: %v", ErrQualification, err)
		}
		q.fills = append(q.fills, f)
	}
	markets := make([]decode.ActorMarketBinding, 0, len(q.fills))
	byPool := map[common.Address]decode.ActorMarketBinding{}
	for _, fill := range q.fills {
		market := decode.ActorMarketBinding{Pool: common.HexToAddress(fill.PoolAddress), Token: common.HexToAddress(fill.Token.Address), Settlement: common.HexToAddress(fill.Settlement.Address)}
		if prior, exists := byPool[market.Pool]; exists {
			if prior != market {
				return nil, ErrQualification
			}
			continue
		}
		byPool[market.Pool] = market
		markets = append(markets, market)
	}
	actorEnvelopes, err := r.DecodeTransactionReceipt(owned.Transaction, owned.Receipt, markets)
	if err != nil || len(actorEnvelopes) != len(decoded) {
		return nil, fmt.Errorf("%w: actor receipt evidence: %v", ErrQualification, err)
	}
	for i, envelope := range actorEnvelopes {
		if envelope == nil || envelope.Raw.Identity != decoded[i].Raw.Identity || envelope.ABIRevision != decoded[i].ABIRevision || envelope.Status != decoded[i].Status {
			return nil, ErrQualification
		}
		decoded[i].Actors = envelope.Actors
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q.issuer = &issuance{receipt: q}
	if _, err = rand.Read(q.issuer.nonce[:]); err != nil {
		return nil, err
	}
	q.issuer.digest, err = q.seal()
	if err != nil {
		return nil, err
	}
	return q, nil
}
func makeFill(ctx context.Context, a Qualification, b Bundle, env *decode.DecodedEnvelope, l *types.Log, sender string, receiptHash, headerHash [32]byte) (accounting.FillV1, error) {
	var f accounting.FillV1
	if env.Event.Args["timestamp"] != strconv.FormatUint(b.Header.Time, 10) || env.Event.Args["blockNumber"] != b.Header.Number.String() {
		return f, ErrQualification
	}
	poolID, ok := env.Event.Args["poolId"].(string)
	if !ok || len(poolID) != 66 || !strings.HasPrefix(poolID, "0x") {
		return f, ErrQualification
	}
	poolBytes, err := hex.DecodeString(poolID[2:])
	if err != nil || !bytes.Equal(poolBytes[:12], make([]byte, 12)) {
		return f, ErrQualification
	}
	pool := common.BytesToAddress(poolBytes[12:])
	if pool == (common.Address{}) || pool == l.Address {
		return f, ErrQualification
	}
	meta, err := a.StableMetadata(ctx, pool, bundleBlockHash(b))
	if err != nil {
		return f, err
	}
	if meta.Pool != pool || meta.BlockHash != bundleBlockHash(b) || meta.Token == (common.Address{}) || meta.Settlement == (common.Address{}) || meta.Token == meta.Settlement || meta.SettlementDecimals != 6 || meta.ABIRevision != env.ABIRevision {
		return f, ErrQualification
	}
	isBuy, ok := env.Event.Args["isBuy"].(bool)
	if !ok {
		return f, ErrQualification
	}
	input, ok := env.Event.Args["amountIn"].(string)
	if !ok {
		return f, ErrQualification
	}
	output, ok := env.Event.Args["amountOut"].(string)
	if !ok {
		return f, ErrQualification
	}
	fee, ok := env.Event.Args["fee"].(string)
	if !ok {
		return f, ErrQualification
	}
	authorityBundle, err := clone(b)
	if err != nil {
		return f, err
	}
	execution, err := a.ExecutionPolicy(ctx, authorityBundle, uint32(l.Index))
	if err != nil {
		return f, err
	}
	execution, err = clone(execution)
	if err != nil || accounting.ValidateExecutionPolicy(execution) != nil {
		return f, ErrQualification
	}
	f = accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: b.Header.Number.String(), TransactionIndex: strconv.FormatUint(uint64(l.TxIndex), 10), LogIndex: strconv.FormatUint(uint64(l.Index), 10), TransactionHash: strings.ToLower(l.TxHash.Hex()), BlockHash: strings.ToLower(l.BlockHash.Hex())}, ActualEmitter: strings.ToLower(l.Address.Hex()), PoolAddress: strings.ToLower(pool.Hex()), Token: accounting.Asset{Address: strings.ToLower(meta.Token.Hex()), Decimals: meta.TokenDecimals}, Settlement: accounting.Asset{Address: strings.ToLower(meta.Settlement.Hex()), Decimals: meta.SettlementDecimals}, PaidInputRaw: input, FeeRaw: fee, InputDerivationQuality: "receipt_incomplete", Attribution: accounting.Attribution{TransactionSender: sender, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Execution: execution, Provenance: accounting.Provenance{DeploymentID: meta.DeploymentID, ABIRevision: meta.ABIRevision, EmitterCodeEpoch: meta.EmitterCodeEpoch, PoolCodeEpoch: meta.PoolCodeEpoch, ReceiptSHA256: hex.EncodeToString(receiptHash[:]), HeaderSHA256: hex.EncodeToString(headerHash[:])}, BlockTimestampSeconds: strconv.FormatUint(b.Header.Time, 10)}
	if isBuy {
		f.Side = "buy"
		f.TokenAmountRaw = output
		f.USDLAmountRaw = input
		f.FeeDisposition = "distributed_usdl"
	} else {
		f.Side = "sell"
		f.TokenAmountRaw = input
		f.USDLAmountRaw = output
		f.FeeDisposition = "retained_token_reserve"
	}
	if execution.Quality != "unavailable" && execution.OpticalImplementation != nil && *execution.OpticalImplementation == strings.ToLower((common.Address{}).Hex()) {
		paid, pe := accounting.Uint(input)
		feeValue, fe := accounting.Uint(fee)
		if pe != nil || fe != nil || paid.Cmp(feeValue) < 0 {
			return accounting.FillV1{}, ErrQualification
		}
		priced := input
		net := new(big.Int).Sub(paid, feeValue).String()
		f.PricedInputRaw = &priced
		f.AMMInputAfterFeeRaw = &net
		f.InputDerivationQuality = "exact"
	}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, meta.DeploymentID)
	return f, nil
}
func attribute(b Bundle, decoded []*decode.DecodedEnvelope, f accounting.FillV1, sender string, receiptHash [32]byte) accounting.Attribution {
	unavailable := f.Attribution
	data := b.Transaction.Data()
	to := b.Transaction.To()
	if to == nil || len(data) != 132 || b.Transaction.Value().Sign() != 0 {
		return unavailable
	}
	name, actor, inKey, outKey := "Buy", "buyer", "usdlIn", "tokensOut"
	sig := "buy(address,uint256,uint256,uint256)"
	inputAsset, outputAsset := common.HexToAddress(f.Settlement.Address), common.HexToAddress(f.Token.Address)
	input, output := f.USDLAmountRaw, f.TokenAmountRaw
	if f.Side == "sell" {
		name, actor, inKey, outKey = "Sell", "seller", "tokensIn", "usdlOut"
		sig = "sell(address,uint256,uint256,uint256)"
		inputAsset, outputAsset = outputAsset, inputAsset
		input, output = f.TokenAmountRaw, f.USDLAmountRaw
	}
	if !bytes.Equal(data[:4], crypto.Keccak256([]byte(sig))[:4]) || !bytes.Equal(data[4:16], make([]byte, 12)) || common.BytesToAddress(data[16:36]) != common.HexToAddress(f.PoolAddress) || new(big.Int).SetBytes(data[36:68]).String() != input {
		return unavailable
	}
	outputValue, err := accounting.Uint(output)
	if err != nil || new(big.Int).SetBytes(data[68:100]).Cmp(outputValue) > 0 || new(big.Int).SetBytes(data[100:132]).Cmp(new(big.Int).SetUint64(b.Header.Time)) < 0 {
		return unavailable
	}
	routes := 0
	primaryFills := 0
	matchingTrader := false
	for i, e := range decoded {
		if e.Family != nil && e.Family.EconomicFill {
			primaryFills++
			if strconv.FormatUint(uint64(b.Receipt.Logs[i].Index), 10) == f.Position.LogIndex && e.Event != nil && e.Event.Args["sender"] == sender {
				matchingTrader = true
			}
		}
		if e.Event != nil && e.EmitterRole == "Router" && e.EventName == name && b.Receipt.Logs[i].Address == *to && e.Event.Args["pool"] == f.PoolAddress && e.Event.Args[actor] == sender && e.Event.Args[inKey] == input && e.Event.Args[outKey] == output {
			routes++
		}
	}
	if routes != 1 || primaryFills != 1 || !matchingTrader {
		return unavailable
	}
	fillIndex, _ := strconv.ParseUint(f.Position.LogIndex, 10, 32)
	match := func(asset, from, to common.Address, amount string) (uint, bool) {
		var index uint
		count := 0
		for _, l := range b.Receipt.Logs {
			if l.Address == asset && len(l.Topics) == 3 && l.Topics[0] == crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")) && len(l.Data) == 32 && bytes.Equal(l.Topics[1].Bytes()[:12], make([]byte, 12)) && bytes.Equal(l.Topics[2].Bytes()[:12], make([]byte, 12)) && common.BytesToAddress(l.Topics[1].Bytes()[12:]) == from && common.BytesToAddress(l.Topics[2].Bytes()[12:]) == to && new(big.Int).SetBytes(l.Data).String() == amount && uint64(l.Index) < fillIndex {
				count++
				index = l.Index
			}
		}
		return index, count == 1
	}
	payer := common.HexToAddress(sender)
	pool := common.HexToAddress(f.PoolAddress)
	inIndex, inOK := match(inputAsset, payer, pool, input)
	outIndex, outOK := match(outputAsset, pool, payer, output)
	if !inOK || !outOK {
		return unavailable
	}
	inLog, outLog := strconv.FormatUint(uint64(inIndex), 10), strconv.FormatUint(uint64(outIndex), 10)
	txDigest := sha256.Sum256(data)
	rd := hex.EncodeToString(receiptHash[:])
	return accounting.Attribution{TransactionSender: sender, RouterCaller: &sender, Trader: &sender, Payer: &sender, Recipient: &sender, Quality: "qualified_calldata", Evidence: []accounting.EvidenceRef{{Kind: "router-calldata", ArtifactSHA256: hex.EncodeToString(txDigest[:]), ArtifactID: f.Position.TransactionHash}, {Kind: "input-transfer", ArtifactSHA256: rd, ArtifactID: f.Position.TransactionHash, LogIndex: &inLog}, {Kind: "output-transfer", ArtifactSHA256: rd, ArtifactID: f.Position.TransactionHash, LogIndex: &outLog}}}
}
func (q *QualifiedReceipt) Fills(ctx context.Context) ([]accounting.FillV1, error) {
	if err := q.validate(ctx); err != nil {
		return nil, err
	}
	return clone(q.fills)
}
func (q *QualifiedReceipt) Events(ctx context.Context) ([]*decode.Event, error) {
	if err := q.validate(ctx); err != nil {
		return nil, err
	}
	return clone(q.events)
}
func (q *QualifiedReceipt) QualifiedEvents(ctx context.Context) ([]*decode.DecodedEnvelope, error) {
	if err := q.validate(ctx); err != nil {
		return nil, err
	}
	return clone(q.envelopes)
}
func (q *QualifiedReceipt) SourceLogs(ctx context.Context) ([]types.Log, error) {
	if err := q.validate(ctx); err != nil {
		return nil, err
	}
	logs := make([]types.Log, len(q.bundle.Receipt.Logs))
	for i, l := range q.bundle.Receipt.Logs {
		logs[i] = *l
	}
	return clone(logs)
}
func (q *QualifiedReceipt) SourceProvenance(ctx context.Context, address common.Address) (accounting.Provenance, error) {
	if err := q.validate(ctx); err != nil {
		return accounting.Provenance{}, err
	}
	proof, ok := q.provenance[strings.ToLower(address.Hex())]
	if !ok {
		return accounting.Provenance{}, ErrQualification
	}
	return clone(proof)
}
func (q *QualifiedReceipt) SourceContext(ctx context.Context) (txHash, sender string, timestamp int64, err error) {
	if err = q.validate(ctx); err != nil {
		return
	}
	return strings.ToLower(q.bundle.Transaction.Hash().Hex()), q.sender, int64(q.bundle.Header.Time), nil
}
func matches(raw store.RawRecord, l *types.Log) bool {
	if raw.TransactionHash != strings.ToLower(l.TxHash.Hex()) || raw.BlockHash != strings.ToLower(l.BlockHash.Hex()) || raw.Emitter != strings.ToLower(l.Address.Hex()) || raw.Height < 0 || uint64(raw.Height) != l.BlockNumber || uint(raw.TransactionIndex) != l.TxIndex || uint(raw.LogIndex) != l.Index || !bytes.Equal(raw.Data, l.Data) || len(raw.Topics) != len(l.Topics) {
		return false
	}
	for i, t := range l.Topics {
		if raw.Topics[i] != strings.ToLower(t.Hex()) {
			return false
		}
	}
	return true
}
func (q *QualifiedReceipt) ValidateBinding(ctx context.Context, b store.CanonicalBatch) error {
	if err := q.validate(ctx); err != nil {
		return err
	}
	if b.ChainID != 125 {
		return ErrBinding
	}
	found := false
	seen := map[uint32]bool{}
	for _, raw := range b.Records {
		if raw.TransactionHash != strings.ToLower(q.bundle.Transaction.Hash().Hex()) {
			continue
		}
		var matching *types.Log
		for _, l := range q.bundle.Receipt.Logs {
			if l.Index == uint(raw.LogIndex) {
				matching = l
				break
			}
		}
		if matching == nil || !matches(raw, matching) || seen[raw.LogIndex] {
			return ErrBinding
		}
		seen[raw.LogIndex] = true
		found = true
	}
	if !found {
		return ErrBinding
	}
	for index := range q.selected {
		if !seen[index] {
			return ErrBinding
		}
	}
	blockFound := false
	for _, block := range b.Blocks {
		if block.Height == q.bundle.Header.Number.Int64() {
			if blockFound || block.Hash != strings.ToLower(bundleBlockHash(q.bundle).Hex()) || !block.FetchComplete || !block.RequiredDecodeComplete {
				return ErrBinding
			}
			blockFound = true
		}
	}
	if !blockFound {
		return ErrBinding
	}
	return nil
}
func ValidateBindings(ctx context.Context, b store.CanonicalBatch, receipts []*QualifiedReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.ChainID != 125 || len(receipts) > 4096 || len(b.Records) > 65536 {
		return ErrBinding
	}
	byTx := map[string]*QualifiedReceipt{}
	for _, q := range receipts {
		if err := q.ValidateBinding(ctx, b); err != nil {
			return err
		}
		key := strings.ToLower(q.bundle.Transaction.Hash().Hex())
		if byTx[key] != nil {
			return ErrBinding
		}
		byTx[key] = q
	}
	seen := map[string]bool{}
	for _, raw := range b.Records {
		key := raw.TransactionHash + ":" + strconv.FormatUint(uint64(raw.LogIndex), 10)
		if byTx[raw.TransactionHash] == nil || seen[key] {
			return ErrBinding
		}
		seen[key] = true
	}
	counts := map[int64]int{}
	for _, raw := range b.Records {
		counts[raw.Height]++
	}
	blocks := map[int64]bool{}
	for _, block := range b.Blocks {
		if block.Height < 0 || blocks[block.Height] || !block.FetchComplete || !block.RequiredDecodeComplete || block.RawCount != counts[block.Height] {
			return ErrBinding
		}
		blocks[block.Height] = true
	}
	for height := range counts {
		if !blocks[height] {
			return ErrBinding
		}
	}
	return nil
}
