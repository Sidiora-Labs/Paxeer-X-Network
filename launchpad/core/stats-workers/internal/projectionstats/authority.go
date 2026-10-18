package projectionstats

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var historicalCorePins = map[string]string{
	"golden-fixtures.json":   "b7538134b931cf24a04d3d18d8545d8865306f703ff5c421f7cff8424e99a993",
	"event-authorities.json": "7812672058fd4aac1b8f0a54d1e5619ff4c6a4fa2ecc6dd412eb0dc8d6553c18",
	"abi-catalog.json":       "8ac70f904a048bca6fbc193c0667fea65b3f5e3d51446c0d5a434ccbbb79103f",
}

type coreLog struct {
	Address, BlockHash, TransactionHash, Data string
	BlockNumber, TransactionIndex, Index      uint64
	Topics                                    []string
	Removed                                   bool
}
type coreReceipt struct {
	Hash, BlockHash    string
	BlockNumber, Index uint64
	Status             uint64
	Logs               []coreLog
}
type admittedCoreTrade struct {
	trade     Trade
	assets    PoolAssets
	timestamp int64
}
type CoreReceiptAuthority struct {
	admitted     map[string]admittedCoreTrade
	transactions map[string][]string
}

func pinnedCoreFile(directory, name string) ([]byte, error) {
	b, e := os.ReadFile(filepath.Join(directory, name))
	if e != nil {
		return nil, e
	}
	if digestBytes(b) != historicalCorePins[name] {
		return nil, fmt.Errorf("%w: frozen core input %s", ErrIntegrity, name)
	}
	return b, nil
}

func coreABI(raw []byte, name string) (abi.ABI, string, error) {
	var c struct {
		ChainID   int `json:"chainId"`
		Contracts []struct {
			Name, ArtifactSha256 string
			ABI                  json.RawMessage
		}
	}
	if json.Unmarshal(raw, &c) != nil || c.ChainID != 125 {
		return abi.ABI{}, "", ErrInvalid
	}
	for _, v := range c.Contracts {
		if v.Name == name {
			a, e := abi.JSON(strings.NewReader(string(v.ABI)))
			return a, v.ArtifactSha256, e
		}
	}
	return abi.ABI{}, "", ErrInvalid
}

func decodeCoreLog(a abi.ABI, name string, l coreLog) (map[string]any, error) {
	event, ok := a.Events[name]
	if !ok || len(l.Topics) == 0 || strings.ToLower(l.Topics[0]) != event.ID.Hex() || l.Removed {
		return nil, ErrInvalid
	}
	raw, e := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
	if e != nil {
		return nil, ErrInvalid
	}
	values := map[string]any{}
	if e = event.Inputs.NonIndexed().UnpackIntoMap(values, raw); e != nil {
		return nil, e
	}
	indexed := abi.Arguments{}
	for _, arg := range event.Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if len(l.Topics) != len(indexed)+1 {
		return nil, ErrInvalid
	}
	topics := make([]common.Hash, len(indexed))
	for i, s := range l.Topics[1:] {
		if !hash.MatchString(strings.ToLower(s)) {
			return nil, ErrInvalid
		}
		topics[i] = common.HexToHash(s)
	}
	if e = abi.ParseTopicsIntoMap(values, indexed, topics); e != nil {
		return nil, e
	}
	return values, nil
}

// OpenHistoricalCoreAuthority admits only immutable, reviewed isolated EVM
// receipts. It cannot authorize current production pools or accept a whitelist.
func OpenHistoricalCoreAuthority(directory string) (*CoreReceiptAuthority, error) {
	authorityBytes, e := pinnedCoreFile(directory, "event-authorities.json")
	if e != nil {
		return nil, e
	}
	var policy struct {
		ChainID  int `json:"chainId"`
		Families []struct{ Family, Primary string }
	}
	if json.Unmarshal(authorityBytes, &policy) != nil || policy.ChainID != 125 {
		return nil, ErrInvalid
	}
	permitted := false
	for _, v := range policy.Families {
		if v.Family == "core_pool_swap" && strings.HasPrefix(v.Primary, "Authorized EventEmitter.Swap") {
			permitted = true
		}
	}
	if !permitted {
		return nil, ErrInvalid
	}
	catalog, e := pinnedCoreFile(directory, "abi-catalog.json")
	if e != nil {
		return nil, e
	}
	emitterABI, epoch, e := coreABI(catalog, "EventEmitter")
	if e != nil {
		return nil, e
	}
	factoryABI, _, e := coreABI(catalog, "SidioraFactory")
	if e != nil {
		return nil, e
	}
	registryABI, _, e := coreABI(catalog, "PoolRegistry")
	if e != nil {
		return nil, e
	}
	_, tokenArtifact, e := coreABI(catalog, "SidioraERC20")
	if e != nil {
		return nil, e
	}
	golden, e := pinnedCoreFile(directory, "golden-fixtures.json")
	if e != nil {
		return nil, e
	}
	var data struct {
		ChainID        int `json:"chainId"`
		Network        string
		ReplayVerified bool
		Addresses      map[string]string
		Receipts       []coreReceipt
		Artifacts      []struct{ ContractName, Sha256 string }
	}
	if json.Unmarshal(golden, &data) != nil || data.ChainID != 125 || data.Network != "isolated-hardhat" || !data.ReplayVerified {
		return nil, ErrInvalid
	}
	tokenVerified := false
	for _, v := range data.Artifacts {
		if v.ContractName == "SidioraERC20" && v.Sha256 == tokenArtifact {
			tokenVerified = true
		}
	}
	if !tokenVerified {
		return nil, ErrIntegrity
	}
	for k, v := range data.Addresses {
		n, err := NormalizeAddress(v)
		if err != nil {
			return nil, err
		}
		data.Addresses[k] = n
	}
	emitter, factory, registry := data.Addresses["eventEmitter"], data.Addresses["factory"], data.Addresses["registry"]
	if emitter == "" || factory == "" || registry == "" || data.Addresses["usdl"] == "" {
		return nil, ErrInvalid
	}
	sort.Slice(data.Receipts, func(i, j int) bool {
		a, b := data.Receipts[i], data.Receipts[j]
		return a.BlockNumber < b.BlockNumber || a.BlockNumber == b.BlockNumber && a.Index < b.Index
	})
	pools := map[string]PoolAssets{}
	out := &CoreReceiptAuthority{map[string]admittedCoreTrade{}, map[string][]string{}}
	for _, r := range data.Receipts {
		if r.Status != 1 {
			continue
		}
		for _, l := range r.Logs {
			if l.BlockHash != r.BlockHash || l.BlockNumber != r.BlockNumber || l.TransactionHash != r.Hash || l.TransactionIndex != r.Index || l.Removed {
				return nil, ErrIntegrity
			}
		}
		for _, l := range r.Logs {
			if strings.ToLower(l.Address) != factory || len(l.Topics) == 0 || strings.ToLower(l.Topics[0]) != factoryABI.Events["MarketCreated"].ID.Hex() {
				continue
			}
			v, err := decodeCoreLog(factoryABI, "MarketCreated", l)
			if err != nil {
				return nil, err
			}
			pool := strings.ToLower(v["pool"].(common.Address).Hex())
			token := strings.ToLower(v["token"].(common.Address).Hex())
			registered := false
			for _, reg := range r.Logs {
				if strings.ToLower(reg.Address) != registry || len(reg.Topics) == 0 || strings.ToLower(reg.Topics[0]) != registryABI.Events["PoolRegistered"].ID.Hex() {
					continue
				}
				p, err := decodeCoreLog(registryABI, "PoolRegistered", reg)
				if err != nil {
					return nil, err
				}
				if p["pool"].(common.Address) == v["pool"].(common.Address) && p["token"].(common.Address) == v["token"].(common.Address) {
					registered = true
				}
			}
			if !registered {
				return nil, ErrIntegrity
			}
			assets := PoolAssets{data.Addresses["usdl"], token, 6}
			if old, ok := pools[pool]; ok && old != assets {
				return nil, ErrIntegrity
			}
			pools[pool] = assets
		}
		for _, l := range r.Logs {
			if strings.ToLower(l.Address) != emitter || len(l.Topics) == 0 || strings.ToLower(l.Topics[0]) != emitterABI.Events["Swap"].ID.Hex() {
				continue
			}
			v, err := decodeCoreLog(emitterABI, "Swap", l)
			if err != nil {
				return nil, err
			}
			market := v["poolId"].([32]byte)
			if hex.EncodeToString(market[:12]) != strings.Repeat("0", 24) {
				return nil, ErrIntegrity
			}
			pool := "0x" + hex.EncodeToString(market[12:])
			assets, ok := pools[pool]
			if !ok {
				return nil, ErrIntegrity
			}
			height := v["blockNumber"].(*big.Int)
			timestamp := v["timestamp"].(*big.Int)
			if !height.IsUint64() || height.Uint64() != r.BlockNumber || !timestamp.IsInt64() {
				return nil, ErrIntegrity
			}
			paid, output, fee := v["amountIn"].(*big.Int).String(), v["amountOut"].(*big.Int).String(), v["fee"].(*big.Int).String()
			t := Trade{PoolAddress: pool, Sender: strings.ToLower(v["sender"].(common.Address).Hex()), Side: "sell", TokenAmountRaw: paid, USDLAmountRaw: output,
				Position: Position{"125", strconv.FormatUint(r.BlockNumber, 10), strconv.FormatUint(r.Index, 10), strconv.FormatUint(l.Index, 10), strings.ToLower(r.Hash), strings.ToLower(r.BlockHash)},
				FeeRaw:   fee, FeeAssetAddress: assets.Token, FeeAssetDecimals: assets.TokenDecimals, FeeDisposition: "retained_token_reserve", PaidInputRaw: &paid, InputDerivationQuality: "receipt_incomplete", RecipientAttribution: "unavailable", RecipientEvidenceRefs: []string{}, EmittingContract: emitter, CodeEpoch: epoch}
			if v["isBuy"].(bool) {
				t.Side = "buy"
				t.TokenAmountRaw = output
				t.USDLAmountRaw = paid
				t.FeeAssetAddress = assets.USDL
				t.FeeAssetDecimals = 6
				t.FeeDisposition = "distributed_usdl"
			}
			t.EventID = t.Position.Identity()
			if old, exists := out.admitted[t.EventID]; exists {
				if Digest(old.trade) != Digest(t) {
					return nil, ErrIntegrity
				}
				continue
			}
			out.admitted[t.EventID] = admittedCoreTrade{t, assets, timestamp.Int64()}
			out.transactions[t.Position.TransactionHash] = append(out.transactions[t.Position.TransactionHash], t.EventID)
		}
	}
	if len(out.admitted) == 0 {
		return nil, ErrInvalid
	}
	return out, nil
}

func cloneCoreTrade(t Trade) Trade {
	b, _ := json.Marshal(t)
	var copy Trade
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (a *CoreReceiptAuthority) Trades(transactionHash string) []Trade {
	if a == nil {
		return nil
	}
	out := []Trade{}
	for _, id := range a.transactions[strings.ToLower(transactionHash)] {
		out = append(out, cloneCoreTrade(a.admitted[id].trade))
	}
	return out
}

func (a *CoreReceiptAuthority) Account(t Trade) (Accounting, error) {
	if a == nil {
		return Accounting{}, ErrInvalid
	}
	admitted, ok := a.admitted[t.EventID]
	if !ok || Digest(admitted.trade) != Digest(t) {
		return Accounting{}, ErrIntegrity
	}
	return accountAmounts(t, admitted.assets, nil, true)
}

type QualifiedAccountingPolicy struct {
	FeeBPS                  *uint16 `json:"feeBps"`
	FeeEvidenceDigest       *string `json:"feeEvidenceDigest"`
	AuthorityEvidenceDigest string  `json:"authorityEvidenceDigest"`
	tradeDigest             string
	policyDigest            string
	blockTimestamp          int64
	assets                  PoolAssets
}

type QualifiedApplyInput struct {
	Trade                                               Trade
	Assets                                              PoolAssets
	Policy                                              QualifiedAccountingPolicy
	BlockTimestamp, WatermarkTimestamp, FutureTolerance int64
	ConsumerVersion, ProjectionVersion                  string
}

type AccountingSourceAuthority struct {
	view         string
	publications string
}

func NewAccountingSourceAuthority(schema string) (*AccountingSourceAuthority, error) {
	if !schemaName.MatchString(schema) {
		return nil, ErrInvalid
	}
	return &AccountingSourceAuthority{view: pgx.Identifier{schema, "indexer_accounting_fills_v1"}.Sanitize(), publications: pgx.Identifier{schema, "indexer_accounting_publications_v1"}.Sanitize()}, nil
}

func (a *AccountingSourceAuthority) OriginalTx(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1) (accounting.FillV1, error) {
	if a == nil || a.view == "" || tx == nil || accounting.ValidatePublicationV1(p) != nil {
		return accounting.FillV1{}, ErrInvalid
	}
	var raw []byte
	var recorded string
	if err := tx.QueryRow(ctx, "SELECT payload,source_fact_digest FROM "+a.view+" WHERE source_fill_id=$1", p.SourceFillID).Scan(&raw, &recorded); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accounting.FillV1{}, ErrIntegrity
		}
		return accounting.FillV1{}, err
	}
	original, err := accounting.DecodeFillV1(raw)
	if err != nil {
		return accounting.FillV1{}, ErrIntegrity
	}
	digest, err := accounting.FillDigest(original)
	if err != nil || original.SourceFillID != p.SourceFillID || string(digest) != recorded || digest != p.SourceFactDigest {
		return accounting.FillV1{}, ErrIntegrity
	}
	if p.Fill != nil {
		proposed, err := accounting.FillDigest(*p.Fill)
		if err != nil || proposed != digest {
			return accounting.FillV1{}, ErrIntegrity
		}
	} else {
		if p.Positions[0].SourceOrder != original.Position || p.Enrichment == nil || p.Enrichment.Attribution == nil || p.Enrichment.Attribution.Payer == nil || p.Positions[0].PartitionKey != "125:"+*p.Enrichment.Attribution.Payer+":"+original.Token.Address {
			return accounting.FillV1{}, ErrIntegrity
		}
		var source []byte
		if err = tx.QueryRow(ctx, "SELECT payload FROM "+a.publications+" WHERE publication_id=$1", p.PublicationID).Scan(&source); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return accounting.FillV1{}, ErrIntegrity
			}
			return accounting.FillV1{}, err
		}
		issued, decodeErr := accounting.DecodePublicationV1(source)
		if decodeErr != nil {
			return accounting.FillV1{}, ErrIntegrity
		}
		actual, encodeErr := accounting.CanonicalPublicationV1(p)
		expected, issuedErr := accounting.CanonicalPublicationV1(issued)
		if encodeErr != nil || issuedErr != nil || !bytes.Equal(actual, expected) {
			return accounting.FillV1{}, ErrIntegrity
		}
	}
	return original, nil
}

func (a *AccountingSourceAuthority) ResolveTx(ctx context.Context, tx pgx.Tx, p accounting.PublicationV1, consumerVersion string) (QualifiedApplyInput, error) {
	if p.Facet != "market" || p.Fill == nil {
		return QualifiedApplyInput{}, ErrInvalid
	}
	f, err := a.OriginalTx(ctx, tx, p)
	if err != nil {
		return QualifiedApplyInput{}, err
	}
	in, err := qualifiedInput(f, consumerVersion, p.Positions[0].ProjectionVersion)
	if err != nil {
		return QualifiedApplyInput{}, err
	}
	return in, nil
}

func qualifiedInput(f accounting.FillV1, consumerVersion, projectionVersion string) (QualifiedApplyInput, error) {
	if accounting.ValidateFillV1(f) != nil {
		return QualifiedApplyInput{}, ErrInvalid
	}
	timestamp, err := strconv.ParseInt(f.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return QualifiedApplyInput{}, ErrInvalid
	}
	paid := f.PaidInputRaw
	feeAsset, feeDecimals := f.Token.Address, int(f.Token.Decimals)
	if f.Side == "buy" {
		feeAsset, feeDecimals = f.Settlement.Address, int(f.Settlement.Decimals)
	}
	refs := []string{}
	attribution := "unavailable"
	var recipient *string
	if f.Attribution.Recipient != nil {
		r := *f.Attribution.Recipient
		recipient = &r
		attribution = f.Attribution.Quality
		for _, ref := range f.Attribution.Evidence {
			raw, err := json.Marshal(ref)
			if err != nil {
				return QualifiedApplyInput{}, err
			}
			refs = append(refs, digestBytes(raw))
		}
	}
	trade := Trade{EventID: Position(f.Position).Identity(), PoolAddress: f.PoolAddress, Sender: f.Attribution.TransactionSender,
		Recipient: recipient, Side: f.Side, TokenAmountRaw: f.TokenAmountRaw, USDLAmountRaw: f.USDLAmountRaw, Position: Position(f.Position),
		FeeRaw: f.FeeRaw, FeeAssetAddress: feeAsset, FeeAssetDecimals: feeDecimals, FeeDisposition: f.FeeDisposition,
		PaidInputRaw: &paid, PricedInputRaw: f.PricedInputRaw, AMMInputAfterFeeRaw: f.AMMInputAfterFeeRaw,
		InputDerivationQuality: f.InputDerivationQuality, RecipientAttribution: attribution, RecipientEvidenceRefs: refs,
		EmittingContract: f.ActualEmitter, CodeEpoch: f.Provenance.EmitterCodeEpoch}
	assets := PoolAssets{f.Settlement.Address, f.Token.Address, int(f.Token.Decimals)}
	sourceDigest, err := accounting.FillDigest(f)
	if err != nil {
		return QualifiedApplyInput{}, err
	}
	policy := QualifiedAccountingPolicy{AuthorityEvidenceDigest: string(sourceDigest), tradeDigest: Digest(trade), blockTimestamp: timestamp, assets: assets}
	if f.Execution.FeeBPS != nil {
		if f.Execution.Quality == "unavailable" || f.Execution.ReplayEvidenceDigest == nil {
			return QualifiedApplyInput{}, ErrIntegrity
		}
		bps := *f.Execution.FeeBPS
		policy.FeeBPS = &bps
		evidence := string(*f.Execution.ReplayEvidenceDigest)
		policy.FeeEvidenceDigest = &evidence
	}
	policyBytes, _ := json.Marshal(policy)
	policy.policyDigest = digestBytes(policyBytes)
	return QualifiedApplyInput{trade, assets, policy, timestamp, timestamp, 0, consumerVersion, projectionVersion}, nil
}

func AccountQualified(t Trade, assets PoolAssets, p QualifiedAccountingPolicy) (Accounting, error) {
	policyBytes, _ := json.Marshal(p)
	if p.policyDigest == "" || digestBytes(policyBytes) != p.policyDigest {
		return Accounting{}, ErrIntegrity
	}
	if !qualifiedDigest.MatchString(p.AuthorityEvidenceDigest) || p.tradeDigest == "" || p.tradeDigest != Digest(t) || p.assets != assets ||
		(p.FeeBPS == nil) != (p.FeeEvidenceDigest == nil) || p.FeeEvidenceDigest != nil && !qualifiedDigest.MatchString(*p.FeeEvidenceDigest) {
		return Accounting{}, ErrIntegrity
	}
	return accountAmounts(t, assets, p.FeeBPS, true)
}

var qualifiedDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
