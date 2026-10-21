package coverage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const MeasurementRate = 10000
const MeasurementSeconds = 300
const MaxMeasurementInput = 32 << 20

type MeasurementRPC interface {
	PinnedRPC
	ChainID(context.Context) (*big.Int, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
}

type ReceiptRequest struct {
	TransactionHash          common.Hash `json:"transactionHash"`
	Scenario                 int         `json:"scenario"`
	ExpectedCanonicalSHA256  string      `json:"expectedCanonicalSha256"`
	ExpectedProjectionSHA256 string      `json:"expectedProjectionSha256"`
}
type GetterRequest struct {
	BlockHash            common.Hash    `json:"blockHash"`
	Address              common.Address `json:"address"`
	Input                []byte         `json:"input"`
	ExpectedOutputSHA256 string         `json:"expectedOutputSha256"`
}
type MeasurementManifest struct {
	SchemaVersion            int                    `json:"schemaVersion"`
	ChainID                  uint64                 `json:"chainId"`
	SourceManifestSHA256     string                 `json:"sourceManifestSha256"`
	InterfaceSHA256          string                 `json:"interfaceSha256"`
	Deployments              []catalog.Deployment   `json:"deployments"`
	EnabledFamilies          []string               `json:"enabledFamilies"`
	PoolRegistry             common.Address         `json:"poolRegistry"`
	Permissions              []PermissionEpoch      `json:"permissions"`
	Implementations          []ImplementationReview `json:"implementations"`
	Receipts                 []ReceiptRequest       `json:"receipts"`
	Getters                  []GetterRequest        `json:"getters"`
	ExpectedProjectionSHA256 string                 `json:"expectedProjectionSha256"`
	ReviewedRevisions        json.RawMessage        `json:"reviewedRevisions,omitempty"`
	ReviewedRevisionsSHA256  string                 `json:"reviewedRevisionsSha256,omitempty"`
	reviewedHash             string
	seal                     [32]byte
}
type CapturedReceipt struct {
	Header                   *types.Header  `json:"header"`
	SignedTransaction        []byte         `json:"signedTransaction"`
	Receipt                  *types.Receipt `json:"receipt"`
	Scenario                 int            `json:"scenario"`
	ExpectedCanonicalSHA256  string         `json:"expectedCanonicalSha256"`
	ExpectedProjectionSHA256 string         `json:"expectedProjectionSha256"`
}
type CapturedGetter struct {
	Request GetterRequest `json:"request"`
	Output  []byte        `json:"output"`
	Block   uint64        `json:"block"`
}
type HistoricalCapture struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ChainID        uint64            `json:"chainId"`
	ManifestSHA256 string            `json:"manifestSha256"`
	Receipts       []CapturedReceipt `json:"receipts"`
	Getters        []CapturedGetter  `json:"getters"`
	seal           [32]byte
}
type SurfaceMeasurement struct {
	Family, Version, Kind, Signature, Identity string
	Observations                               uint64
}
type BurstWindow struct {
	Second                                int
	Offered, Decoded, Matched, Mismatched uint64
	ElapsedNanoseconds                    int64
	CompletedBeforeDeadline               bool
}
type BurstMeasurement struct {
	Kind                                    string
	TargetEventsPerSecond, RequiredSeconds  int
	Attempted, Decoded, Matched, Mismatched uint64
	UniqueHistoricalLogs                    int
	ElapsedNanoseconds                      int64
	MeasuredEventsPerSecond                 float64
	HeapBytesBefore, HeapBytesAfter         uint64
	AllocatedBytes                          uint64
	Windows                                 []BurstWindow
	OutputSHA256                            string
	Passed                                  bool
}
type ReplayMeasurement struct {
	CanonicalFirst, CanonicalSecond               []json.RawMessage
	ProjectionStatesFirst, ProjectionStatesSecond []json.RawMessage
	FirstSHA256, SecondSHA256, ProjectionSHA256   string
	ByteIdentical                                 bool
	ReceiptCount, UniqueRawLogs                   int
	SurfaceMatrix                                 []SurfaceMeasurement
	Scenarios                                     map[int]int
	Blockers                                      []string
}

func measurementDigest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func validMeasurementDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && value == strings.ToLower(value)
}
func ReadMeasurementManifest(raw []byte, reviewedSHA256 string) (MeasurementManifest, error) {
	var input MeasurementManifest
	if len(raw) > MaxMeasurementInput || !validMeasurementDigest(reviewedSHA256) || measurementDigest(raw) != reviewedSHA256 {
		return input, fmt.Errorf("exact reviewed bounded measurement input required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, fmt.Errorf("trailing measurement input")
	}
	if input.SchemaVersion != 1 || input.ChainID != 125 || !validMeasurementDigest(input.SourceManifestSHA256) || !validMeasurementDigest(input.InterfaceSHA256) || !validMeasurementDigest(input.ExpectedProjectionSHA256) || len(input.Deployments) == 0 || len(input.Deployments) > 4096 || len(input.Receipts) == 0 || len(input.Receipts) > 4096 || len(input.Getters) > 4096 || len(input.EnabledFamilies) == 0 || len(input.EnabledFamilies) > 64 {
		return input, fmt.Errorf("measurement source identities and bounded real receipt corpus required")
	}
	seen := map[common.Hash]bool{}
	for _, request := range input.Receipts {
		if request.TransactionHash == (common.Hash{}) || request.Scenario < 1 || request.Scenario > 12 || !validMeasurementDigest(request.ExpectedCanonicalSHA256) || !validMeasurementDigest(request.ExpectedProjectionSHA256) || seen[request.TransactionHash] {
			return input, fmt.Errorf("unique reviewed transaction/scenario/expected output required")
		}
		seen[request.TransactionHash] = true
	}
	for _, request := range input.Getters {
		if request.BlockHash == (common.Hash{}) || request.Address == (common.Address{}) || len(request.Input) < 4 || len(request.Input) > catalog.MaxPayload || !validMeasurementDigest(request.ExpectedOutputSHA256) {
			return input, fmt.Errorf("pinned reviewed getter input required")
		}
	}
	input.reviewedHash = reviewedSHA256
	encoded, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	input.seal = sha256.Sum256(encoded)
	return input, nil
}
func manifestIntact(input MeasurementManifest) bool {
	raw, err := json.Marshal(input)
	return err == nil && input.seal != ([32]byte{}) && input.seal == sha256.Sum256(raw) && validMeasurementDigest(input.reviewedHash)
}
func MeasurementRegistry(input MeasurementManifest) (*catalog.Registry, []catalog.Bundle, error) {
	if !manifestIntact(input) {
		return nil, nil, fmt.Errorf("unaltered reviewed measurement manifest required")
	}
	bundles, err := catalog.Bundles()
	if err != nil {
		return nil, nil, err
	}
	var registry *catalog.Registry
	if len(input.ReviewedRevisions) > 0 {
		registry, err = catalog.NewWithReviewedRevisions(input.Deployments, input.ReviewedRevisions, input.ReviewedRevisionsSHA256)
		if err != nil {
			return nil, nil, err
		}
		var revisions catalog.ReviewedRevision
		if err = json.Unmarshal(input.ReviewedRevisions, &revisions); err != nil {
			return nil, nil, err
		}
		bundles = append(bundles, revisions.Bundles...)
	} else {
		registry, err = catalog.New(input.Deployments)
		if err != nil {
			return nil, nil, err
		}
	}
	enabled := map[string]bool{}
	for _, family := range input.EnabledFamilies {
		if family == "" || enabled[family] {
			return nil, nil, fmt.Errorf("explicit unique enabled family required")
		}
		enabled[family] = true
		mapped := false
		for _, d := range input.Deployments {
			if d.Family == family {
				mapped = true
			}
		}
		if !mapped {
			return nil, nil, fmt.Errorf("missing enabled family deployment: %s", family)
		}
	}
	for _, d := range input.Deployments {
		if !enabled[d.Family] {
			return nil, nil, fmt.Errorf("attested deployment omitted from enabled surface: %s", d.Family)
		}
	}
	return registry, bundles, nil
}
func CaptureHistorical(ctx context.Context, rpc MeasurementRPC, input MeasurementManifest, manifestSHA256 string) (*HistoricalCapture, error) {
	if rpc == nil || !manifestIntact(input) || !validMeasurementDigest(manifestSHA256) || manifestSHA256 != input.reviewedHash {
		return nil, fmt.Errorf("historical RPC and reviewed input identity required")
	}
	chain, err := rpc.ChainID(ctx)
	if err != nil {
		return nil, measurementRPCFailure("eth_chainId", err)
	}
	if chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return nil, fmt.Errorf("historical RPC chain differs")
	}
	out := &HistoricalCapture{SchemaVersion: 1, ChainID: 125, ManifestSHA256: manifestSHA256}
	totalBytes := 0
	committed := map[uint64]common.Hash{}
	for _, request := range input.Receipts {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		tx, pending, e := rpc.TransactionByHash(ctx, request.TransactionHash)
		if e != nil {
			return out, measurementRPCFailure("eth_getTransactionByHash", e)
		}
		if tx == nil || pending || tx.Hash() != request.TransactionHash || tx.ChainId().Cmp(big.NewInt(125)) != 0 {
			return out, fmt.Errorf("historical signed transaction unresolved")
		}
		receipt, e := rpc.TransactionReceipt(ctx, request.TransactionHash)
		if e != nil {
			return out, measurementRPCFailure("eth_getTransactionReceipt", e)
		}
		if receipt == nil || receipt.TxHash != tx.Hash() || receipt.BlockHash == (common.Hash{}) || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || len(receipt.Logs) > MaxReceiptLogs {
			return out, fmt.Errorf("historical receipt coordinates invalid")
		}
		header, e := rpc.HeaderByHash(ctx, receipt.BlockHash)
		if e != nil {
			return out, measurementRPCFailure("eth_getBlockByHash", e)
		}
		if header == nil || header.Number == nil || header.Hash() != receipt.BlockHash || header.Number.Cmp(receipt.BlockNumber) != 0 {
			return out, fmt.Errorf("historical header differs")
		}
		canonical, e := rpc.HeaderByNumber(ctx, header.Number)
		if e != nil {
			return out, measurementRPCFailure("eth_getBlockByNumber", e)
		}
		if canonical == nil || canonical.Hash() != header.Hash() {
			return out, fmt.Errorf("historical committed block hash differs")
		}
		height := header.Number.Uint64()
		if previous, exists := committed[height]; exists && previous != header.Hash() {
			return out, fmt.Errorf("conflicting committed historical block hashes")
		}
		committed[height] = header.Hash()
		signed, e := tx.MarshalBinary()
		if e != nil {
			return out, e
		}
		captured := CapturedReceipt{Header: header, SignedTransaction: signed, Receipt: receipt, Scenario: request.Scenario, ExpectedCanonicalSHA256: request.ExpectedCanonicalSHA256, ExpectedProjectionSHA256: request.ExpectedProjectionSHA256}
		raw, e := json.Marshal(captured)
		if e != nil {
			return out, e
		}
		if len(raw) > MaxMeasurementInput-totalBytes {
			return out, fmt.Errorf("historical capture byte bound exceeded")
		}
		totalBytes += len(raw)
		out.Receipts = append(out.Receipts, captured)
	}
	for _, request := range input.Getters {
		header, e := rpc.HeaderByHash(ctx, request.BlockHash)
		if e != nil {
			return out, measurementRPCFailure("eth_getBlockByHash", e)
		}
		if header == nil || header.Number == nil || !header.Number.IsUint64() || header.Hash() != request.BlockHash {
			return out, fmt.Errorf("getter canonical header differs")
		}
		canonical, e := rpc.HeaderByNumber(ctx, header.Number)
		if e != nil {
			return out, measurementRPCFailure("eth_getBlockByNumber", e)
		}
		if canonical == nil || canonical.Hash() != header.Hash() {
			return out, fmt.Errorf("getter committed block hash differs")
		}
		height := header.Number.Uint64()
		if previous, exists := committed[height]; exists && previous != header.Hash() {
			return out, fmt.Errorf("getter conflicts with historical committed hash")
		}
		committed[height] = header.Hash()
		output, e := rpc.CallContractAtHash(ctx, ethereum.CallMsg{To: &request.Address, Data: request.Input}, request.BlockHash)
		if e != nil {
			return out, measurementRPCFailure("eth_call_at_hash", e)
		}
		getter := CapturedGetter{Request: request, Output: output, Block: header.Number.Uint64()}
		raw, e := json.Marshal(getter)
		if e != nil {
			return out, e
		}
		if len(raw) > MaxMeasurementInput-totalBytes {
			return out, fmt.Errorf("getter capture byte bound exceeded")
		}
		totalBytes += len(raw)
		out.Getters = append(out.Getters, getter)
		if len(output) > catalog.MaxPayload || measurementDigest(output) != request.ExpectedOutputSHA256 {
			return out, fmt.Errorf("getter expected output differs")
		}
	}
	sort.Slice(out.Receipts, func(i, j int) bool {
		a, b := out.Receipts[i].Receipt, out.Receipts[j].Receipt
		if a.BlockNumber.Cmp(b.BlockNumber) != 0 {
			return a.BlockNumber.Cmp(b.BlockNumber) < 0
		}
		return a.TransactionIndex < b.TransactionIndex
	})
	out.seal = captureSeal(out)
	return out, nil
}

func captureSeal(capture *HistoricalCapture) [32]byte {
	raw, err := json.Marshal(capture)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(raw)
}

func measurementSurface(bundles []catalog.Bundle, input MeasurementManifest) ([]SurfaceMeasurement, map[string]abi.ABI, error) {
	enabled := map[string]bool{}
	for _, f := range input.EnabledFamilies {
		enabled[f] = true
	}
	used := map[string]bool{}
	for _, d := range input.Deployments {
		used[d.Family+":"+d.Version] = true
	}
	rows := []SurfaceMeasurement{}
	parsed := map[string]abi.ABI{}
	for _, b := range bundles {
		key := b.Name + ":" + b.ABIHash
		if !enabled[b.Name] || !used[key] {
			continue
		}
		a, err := abi.JSON(bytes.NewReader(b.ABI))
		if err != nil {
			return nil, nil, err
		}
		parsed[key] = a
		for _, e := range a.Events {
			rows = append(rows, SurfaceMeasurement{Family: b.Name, Version: b.ABIHash, Kind: "event", Signature: e.Sig, Identity: e.ID.Hex()})
		}
		for _, m := range a.Methods {
			rows = append(rows, SurfaceMeasurement{Family: b.Name, Version: b.ABIHash, Kind: "function", Signature: m.Sig, Identity: "0x" + hex.EncodeToString(m.ID)})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return a.Family+a.Version+a.Kind+a.Signature < b.Family+b.Version+b.Kind+b.Signature
	})
	return rows, parsed, nil
}
func observeSurface(rows []SurfaceMeasurement, family, version, kind, signature string) {
	for i := range rows {
		if rows[i].Family == family && rows[i].Version == version && rows[i].Kind == kind && rows[i].Signature == signature {
			rows[i].Observations++
		}
	}
}
func observeCall(rows []SurfaceMeasurement, parsed map[string]abi.ABI, registry *catalog.Registry, address common.Address, block uint64, input, output []byte, hasOutput bool) error {
	dep, ok := registry.DeploymentAt(125, address, block)
	if !ok {
		return fmt.Errorf("selector origin unregistered")
	}
	a, ok := parsed[dep.Family+":"+dep.Version]
	if !ok || len(input) < 4 {
		return fmt.Errorf("enabled selector ABI unavailable")
	}
	method, err := a.MethodById(input[:4])
	if err != nil {
		return err
	}
	if !catalog.ArgumentsBounded(method.Inputs, input[4:]) {
		return fmt.Errorf("selector arguments exceed bounded ABI admission")
	}
	values, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return err
	}
	packed, err := method.Inputs.Pack(values...)
	if err != nil || !bytes.Equal(packed, input[4:]) {
		return fmt.Errorf("selector arguments noncanonical")
	}
	if hasOutput {
		if !catalog.ArgumentsBounded(method.Outputs, output) {
			return fmt.Errorf("getter output exceeds bounded ABI admission")
		}
		values, err = method.Outputs.Unpack(output)
		if err != nil {
			return err
		}
		packed, err = method.Outputs.Pack(values...)
		if err != nil || !bytes.Equal(packed, output) {
			return fmt.Errorf("getter return noncanonical")
		}
	}
	observeSurface(rows, dep.Family, dep.Version, "function", method.Sig)
	return nil
}
func ReplayHistorical(ctx context.Context, source AuthoritySource, input MeasurementManifest, capture *HistoricalCapture) (*ReplayMeasurement, error) {
	if !manifestIntact(input) || capture == nil || capture.ChainID != 125 || capture.SchemaVersion != 1 || capture.ManifestSHA256 != input.reviewedHash || capture.seal == ([32]byte{}) || capture.seal != captureSeal(capture) || len(capture.Receipts) == 0 {
		return nil, fmt.Errorf("real reviewed historical capture and manifest required")
	}
	registry, bundles, err := MeasurementRegistry(input)
	if err != nil {
		return nil, err
	}
	correlator := &Correlator{Registry: registry, Source: source}
	rows, parsed, err := measurementSurface(bundles, input)
	if err != nil {
		return nil, err
	}
	out := &ReplayMeasurement{ReceiptCount: len(capture.Receipts), SurfaceMatrix: rows, Scenarios: map[int]int{}}
	unique := map[string]bool{}
	for pass := 0; pass < 2; pass++ {
		lifecycle, e := NewLifecycle(correlator.Registry, input.Implementations)
		if e != nil {
			return out, e
		}
		projection, e := NewProjection(lifecycle)
		if e != nil {
			return out, e
		}
		canonical := []json.RawMessage{}
		projectionStates := []json.RawMessage{}
		for _, record := range capture.Receipts {
			if err = ctx.Err(); err != nil {
				return out, err
			}
			tx := new(types.Transaction)
			if e = tx.UnmarshalBinary(record.SignedTransaction); e != nil {
				return out, e
			}
			result, e := correlator.Correlate(ctx, record.Header, tx, record.Receipt)
			if e != nil {
				return out, e
			}
			raw, e := json.Marshal(result)
			if e != nil {
				return out, e
			}
			canonical = append(canonical, json.RawMessage(raw))
			if measurementDigest(raw) != record.ExpectedCanonicalSHA256 {
				out.Blockers = append(out.Blockers, fmt.Sprintf("scenario_%d_expected_canonical_mismatch:%s", record.Scenario, record.Receipt.TxHash.Hex()))
			}
			if result.Successful {
				if e = projection.ApplyReceipt(result); e != nil {
					out.Blockers = append(out.Blockers, "native_projection:"+e.Error())
				} else {
					firstState, e := json.Marshal(projection)
					if e != nil {
						return out, e
					}
					if e = projection.ApplyReceipt(result); e != nil {
						out.Blockers = append(out.Blockers, "native_projection_duplicate:"+e.Error())
					} else {
						duplicateState, e := json.Marshal(projection)
						if e != nil {
							return out, e
						}
						if !bytes.Equal(firstState, duplicateState) {
							out.Blockers = append(out.Blockers, "native_projection_duplicate_changed_state")
						}
					}
				}
			}
			state, e := json.Marshal(projection)
			if e != nil {
				return out, e
			}
			projectionStates = append(projectionStates, json.RawMessage(state))
			if measurementDigest(state) != record.ExpectedProjectionSHA256 {
				out.Blockers = append(out.Blockers, fmt.Sprintf("scenario_%d_expected_projection_mismatch:%s", record.Scenario, record.Receipt.TxHash.Hex()))
			}
			for _, event := range result.Records {
				if event.Decoded.EmitterRole == "SidioraFactory" && event.Event == "MarketCreated" {
					if _, e = result.ResolveCreation(record.Receipt.TxHash); e != nil {
						out.Blockers = append(out.Blockers, "creation_resolution:"+e.Error())
					}
				}
			}
			if pass == 0 {
				out.Scenarios[record.Scenario]++
				for _, r := range result.Records {
					key := fmt.Sprintf("%d/%s/%s/%s/%d", r.Identity.ChainID, r.Identity.BlockHash.Hex(), r.Identity.TransactionHash.Hex(), r.Identity.Emitter.Hex(), r.Identity.LogIndex)
					if unique[key] {
						continue
					}
					unique[key] = true
					if r.Decoded.Status == "decoded" || r.Decoded.Status == "schema_unrecognized" {
						observeSurface(rows, r.Decoded.EmitterRole, r.Decoded.Version, "event", r.Decoded.Signature)
					}
					if r.Status == "caller_evidence_unavailable" || r.Status == "typed_permission_mismatch" {
						out.Blockers = append(out.Blockers, r.Status+":"+r.Event)
					}
				}
				out.Blockers = append(out.Blockers, result.Diagnostics...)
				if tx.To() != nil && len(tx.Data()) >= 4 {
					if e = observeCall(rows, parsed, correlator.Registry, *tx.To(), record.Receipt.BlockNumber.Uint64(), tx.Data(), nil, false); e != nil {
						out.Blockers = append(out.Blockers, "transaction_selector:"+e.Error())
					}
				}
			}
		}
		state, e := json.Marshal(projection)
		if e != nil {
			return out, e
		}
		digest := measurementDigest(state)
		if digest != input.ExpectedProjectionSHA256 {
			out.Blockers = append(out.Blockers, "expected_projection_mismatch")
		}
		if pass == 0 {
			out.CanonicalFirst = canonical
			out.ProjectionStatesFirst = projectionStates
			out.ProjectionSHA256 = digest
		} else {
			out.CanonicalSecond = canonical
			out.ProjectionStatesSecond = projectionStates
			if out.ProjectionSHA256 != digest {
				out.Blockers = append(out.Blockers, "projection_replay_nondeterministic")
			}
		}
	}
	for _, g := range capture.Getters {
		if err = observeCall(rows, parsed, correlator.Registry, g.Request.Address, g.Block, g.Request.Input, g.Output, true); err != nil {
			out.Blockers = append(out.Blockers, "getter_selector:"+err.Error())
		}
	}
	first, err := json.Marshal(out.CanonicalFirst)
	if err != nil {
		return out, err
	}
	second, err := json.Marshal(out.CanonicalSecond)
	if err != nil {
		return out, err
	}
	out.FirstSHA256 = measurementDigest(first)
	out.SecondSHA256 = measurementDigest(second)
	out.ByteIdentical = bytes.Equal(first, second)
	out.UniqueRawLogs = len(unique)
	if !out.ByteIdentical {
		out.Blockers = append(out.Blockers, "canonical_replay_nondeterministic")
	}
	for _, row := range rows {
		if row.Observations == 0 {
			out.Blockers = append(out.Blockers, "missing_enabled_surface:"+row.Family+":"+row.Kind+":"+row.Signature)
		}
	}
	for scenario := 1; scenario <= 12; scenario++ {
		if out.Scenarios[scenario] == 0 {
			out.Blockers = append(out.Blockers, fmt.Sprintf("missing_reviewed_scenario:%d", scenario))
		}
	}
	out.Blockers = uniqueMeasurementBlockers(out.Blockers)
	return out, nil
}
func uniqueMeasurementBlockers(input []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range input {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func MeasureDecoderBurst(ctx context.Context, registry *catalog.Registry, capture *HistoricalCapture) (*BurstMeasurement, error) {
	if registry == nil || capture == nil || capture.ChainID != 125 || capture.SchemaVersion != 1 || capture.seal == ([32]byte{}) || capture.seal != captureSeal(capture) {
		return nil, fmt.Errorf("reviewed real decoder corpus required")
	}
	logs := []types.Log{}
	expected := [][32]byte{}
	seen := map[string]bool{}
	for _, r := range capture.Receipts {
		if r.Receipt == nil {
			return nil, fmt.Errorf("missing captured receipt")
		}
		for _, l := range r.Receipt.Logs {
			if l == nil {
				return nil, fmt.Errorf("missing captured raw log")
			}
			decoded := registry.Decode(125, *l)
			if decoded.Status != "decoded" && decoded.Status != "schema_unrecognized" {
				continue
			}
			raw, err := json.Marshal(decoded)
			if err != nil {
				return nil, err
			}
			key := l.BlockHash.Hex() + l.TxHash.Hex() + fmt.Sprint(l.Index)
			if seen[key] {
				continue
			}
			seen[key] = true
			logs = append(logs, *l)
			expected = append(expected, sha256.Sum256(raw))
		}
	}
	if len(logs) == 0 {
		return nil, fmt.Errorf("no bounded decoded historical logs for burst")
	}
	out := &BurstMeasurement{Kind: "unchanged_historical_log_decoder_replay", TargetEventsPerSecond: MeasurementRate, RequiredSeconds: MeasurementSeconds, UniqueHistoricalLogs: len(logs)}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out.HeapBytesBefore = before.HeapAlloc
	digest := sha256.New()
	started := time.Now()
	index := 0
	for second := 0; second < MeasurementSeconds; second++ {
		window := BurstWindow{Second: second, Offered: MeasurementRate}
		windowStart := time.Now()
		for n := 0; n < MeasurementRate; n++ {
			if err := ctx.Err(); err != nil {
				out.ElapsedNanoseconds = time.Since(started).Nanoseconds()
				return out, err
			}
			decoded := registry.Decode(125, logs[index])
			raw, err := json.Marshal(decoded)
			if err != nil {
				return out, err
			}
			hash := sha256.Sum256(raw)
			out.Attempted++
			if decoded.Status == "decoded" || decoded.Status == "schema_unrecognized" {
				out.Decoded++
				window.Decoded++
			}
			if hash == expected[index] {
				out.Matched++
				window.Matched++
			} else {
				out.Mismatched++
				window.Mismatched++
			}
			_, _ = digest.Write(hash[:])
			index = (index + 1) % len(logs)
		}
		deadline := started.Add(time.Duration(second+1) * time.Second)
		window.CompletedBeforeDeadline = !time.Now().After(deadline)
		if delay := time.Until(deadline); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				out.ElapsedNanoseconds = time.Since(started).Nanoseconds()
				return out, ctx.Err()
			case <-timer.C:
			}
		}
		window.ElapsedNanoseconds = time.Since(windowStart).Nanoseconds()
		out.Windows = append(out.Windows, window)
	}
	out.ElapsedNanoseconds = time.Since(started).Nanoseconds()
	out.MeasuredEventsPerSecond = float64(out.Matched) / time.Duration(out.ElapsedNanoseconds).Seconds()
	runtime.ReadMemStats(&after)
	out.HeapBytesAfter = after.HeapAlloc
	out.AllocatedBytes = after.TotalAlloc - before.TotalAlloc
	out.OutputSHA256 = hex.EncodeToString(digest.Sum(nil))
	out.Passed = out.Attempted == MeasurementRate*MeasurementSeconds && out.Matched == out.Attempted && out.Decoded == out.Attempted && out.Mismatched == 0 && out.ElapsedNanoseconds >= int64(MeasurementSeconds)*int64(time.Second)
	for _, window := range out.Windows {
		if !window.CompletedBeforeDeadline || window.Matched != MeasurementRate || window.Decoded != MeasurementRate || window.Mismatched != 0 {
			out.Passed = false
		}
	}
	return out, nil
}

//go:embed *.go *.json cmd/measure/*.go
var compiledMeasurementSources embed.FS

func CompiledMeasurementSourceSHA256() (map[string]string, error) {
	result, err := catalog.CompiledSourceSHA256()
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(compiledMeasurementSources, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := compiledMeasurementSources.ReadFile(path)
		if err != nil {
			return err
		}
		result["protocol/coverage/"+path] = measurementDigest(raw)
		return nil
	})
	return result, err
}

type measurementBoundedBody struct {
	io.Reader
	io.Closer
}
type measurementTransport struct{ inner http.RoundTripper }

func (t measurementTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.inner.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > MaxMeasurementInput {
		response.Body.Close()
		return nil, fmt.Errorf("historical RPC response exceeds bounded capture")
	}
	response.Body = measurementBoundedBody{Reader: io.LimitReader(response.Body, MaxMeasurementInput+1), Closer: response.Body}
	return response, nil
}
func DialMeasurementRPC(ctx context.Context, endpoint string) (*ethclient.Client, error) {
	selected, err := url.Parse(endpoint)
	if err != nil || (selected.Scheme != "http" && selected.Scheme != "https") || selected.Host == "" {
		return nil, fmt.Errorf("explicit HTTP historical RPC required")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.MaxConnsPerHost = 8
	transport.MaxIdleConnsPerHost = 8
	client := &http.Client{Timeout: 15 * time.Second, Transport: measurementTransport{inner: transport}}
	connected, err := rpc.DialOptions(ctx, endpoint, rpc.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("historical RPC connection unavailable")
	}
	return ethclient.NewClient(connected), nil
}

func measurementRPCFailure(method string, err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", method, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", method, context.DeadlineExceeded)
	}
	var remote rpc.Error
	if errors.As(err, &remote) {
		return fmt.Errorf("%s: RPC error code %d", method, remote.ErrorCode())
	}
	return fmt.Errorf("%s: historical RPC unavailable", method)
}
