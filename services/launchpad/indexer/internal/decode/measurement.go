package decode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const DecoderBurstRate = 10000
const DecoderBurstSeconds = 300
const maxMeasurementInputBytes = 64 << 20

var ErrMeasurementInputs = errors.New("decoder measurement inputs rejected")
var ErrMeasurementAcceptance = errors.New("decoder measurement acceptance failed")

type DecoderMeasurementInput struct {
	SchemaVersion                int    `json:"schemaVersion"`
	SourceRoot                   string `json:"sourceRoot"`
	CandidateSourceSHA256        string `json:"candidateSourceSha256"`
	ContractsRoot                string `json:"contractsRoot"`
	CatalogPath                  string `json:"catalogPath"`
	CatalogSHA256                string `json:"catalogSha256"`
	DeploymentPath               string `json:"deploymentPath"`
	DeploymentSHA256             string `json:"deploymentSha256"`
	ArchivePath                  string `json:"archivePath"`
	ArchiveSHA256                string `json:"archiveSha256"`
	ActiveOrder                  Order  `json:"activeOrder"`
	Workers                      int    `json:"workers"`
	MaxP99Nanoseconds            int64  `json:"maxP99Nanoseconds"`
	MaxHeapGrowthBytes           uint64 `json:"maxHeapGrowthBytes"`
	MaxAllocatedBytesPerDelivery uint64 `json:"maxAllocatedBytesPerDelivery"`
}

type DecoderDeploymentInput struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	ManifestSHA256 string               `json:"manifestSha256"`
	Deployments    []Deployment         `json:"deployments"`
	Markets        []ActorMarketBinding `json:"markets"`
	Capabilities   []CapabilityPolicy   `json:"capabilities"`
	Correlations   []CorrelationBinding `json:"correlations"`
}

type ArchivedDecodeExpectation struct {
	Role          string          `json:"role"`
	ABIRevision   string          `json:"abiRevision"`
	EventName     string          `json:"eventName"`
	Signature     string          `json:"signature"`
	Status        string          `json:"status"`
	FailureReason string          `json:"failureReason"`
	Opaque        bool            `json:"opaque"`
	Args          json.RawMessage `json:"args"`
	Alerts        []string        `json:"alerts"`
}

type ArchivedDecoderLog struct {
	Log      ethtypes.Log              `json:"log"`
	Expected ArchivedDecodeExpectation `json:"expected"`
}

type ArchivedActorReceipt struct {
	TransactionHex string                `json:"transactionHex"`
	Receipt        ethtypes.Receipt      `json:"receipt"`
	ExpectedActors map[uint]ActorContext `json:"expectedActors"`
}

type ArchivedOptionalEffect struct {
	Identity   RawIdentity   `json:"identity"`
	Family     string        `json:"family"`
	Action     string        `json:"action"`
	Companions []RawIdentity `json:"companions"`
	Economic   bool          `json:"economic"`
}

type ArchivedOptionalReceipt struct {
	Receipt           ethtypes.Receipt         `json:"receipt"`
	ExpectedEffects   []ArchivedOptionalEffect `json:"expectedEffects"`
	ExpectedUnmatched []RawIdentity            `json:"expectedUnmatched"`
}

type ArchivedCreationExpectation struct {
	Token    string      `json:"token"`
	Pool     string      `json:"pool"`
	Creator  string      `json:"creator"`
	Optical  string      `json:"optical"`
	NFTID    string      `json:"nftId"`
	Identity RawIdentity `json:"identity"`
}

type ArchivedCreationReceipt struct {
	Receipt  ethtypes.Receipt            `json:"receipt"`
	Expected ArchivedCreationExpectation `json:"expected"`
}

type CreationReplayMeasurement struct {
	TransactionHash string                      `json:"transactionHash"`
	Expected        ArchivedCreationExpectation `json:"expected"`
	Observed        ArchivedCreationExpectation `json:"observed"`
	RestartExact    bool                        `json:"restartExact"`
}

type ArchivedDecoderCorpus struct {
	SchemaVersion    int                       `json:"schemaVersion"`
	ChainID          uint64                    `json:"chainId"`
	CatalogSHA256    string                    `json:"catalogSha256"`
	Logs             []ArchivedDecoderLog      `json:"logs"`
	ActorReceipts    []ArchivedActorReceipt    `json:"actorReceipts"`
	CreationReceipts []ArchivedCreationReceipt `json:"creationReceipts"`
	OptionalReceipts []ArchivedOptionalReceipt `json:"optionalReceipts"`
}

type DecoderCoverageRow struct {
	Emitter     string `json:"emitter"`
	From        Order  `json:"from"`
	Until       *Order `json:"until"`
	Role        string `json:"role"`
	ABIRevision string `json:"abiRevision"`
	Signature   string `json:"signature"`
	EventName   string `json:"eventName"`
	Family      string `json:"family"`
	Authority   string `json:"authority"`
	State       string `json:"state"`
	Current     bool   `json:"current"`
	Archived    uint64 `json:"archived"`
	Gap         string `json:"gap,omitempty"`
}

type DecoderMemorySample struct {
	ElapsedNanoseconds int64  `json:"elapsedNanoseconds"`
	HeapAlloc          uint64 `json:"heapAlloc"`
	HeapInuse          uint64 `json:"heapInuse"`
	TotalAlloc         uint64 `json:"totalAlloc"`
	Mallocs            uint64 `json:"mallocs"`
	GC                 uint32 `json:"gc"`
}

type DecoderBurstReport struct {
	AllocationScope                      string                `json:"allocationScope"`
	HeapGrowthScope                      string                `json:"heapGrowthScope"`
	MemorySamplePeriodNanoseconds        int64                 `json:"memorySamplePeriodNanoseconds"`
	WorkloadSource                       string                `json:"workloadSource"`
	DeliveryIdentityScope                string                `json:"deliveryIdentityScope"`
	TargetRate                           int                   `json:"targetRate"`
	TargetSeconds                        int                   `json:"targetSeconds"`
	Scheduled                            uint64                `json:"scheduled"`
	Enqueued                             uint64                `json:"enqueued"`
	Completed                            uint64                `json:"completed"`
	Decoded                              uint64                `json:"decoded"`
	PreservedOpaque                      uint64                `json:"preservedOpaque"`
	PreservedQuarantined                 uint64                `json:"preservedQuarantined"`
	AdmissionDrops                       uint64                `json:"admissionDrops"`
	MissingDeliveries                    uint64                `json:"missingDeliveries"`
	DuplicateDeliveries                  uint64                `json:"duplicateDeliveries"`
	IdentityFailures                     uint64                `json:"identityFailures"`
	CompletedAfterWindow                 uint64                `json:"completedAfterWindow"`
	DurationNanoseconds                  int64                 `json:"durationNanoseconds"`
	DispatchP99Nanoseconds               int64                 `json:"dispatchP99Nanoseconds"`
	DecodeP99Nanoseconds                 int64                 `json:"decodeP99Nanoseconds"`
	EndToEndP99Nanoseconds               int64                 `json:"endToEndP99Nanoseconds"`
	AllocatedBytes                       uint64                `json:"allocatedBytes"`
	AllocationCount                      uint64                `json:"allocationCount"`
	BytesPerDelivery                     uint64                `json:"bytesPerDelivery"`
	HeapGrowthBytes                      uint64                `json:"heapGrowthBytes"`
	InputIdentitySHA256                  string                `json:"inputIdentitySha256"`
	ObservedIdentitySHA256               string                `json:"observedIdentitySha256"`
	LargestPayloadBytes                  int                   `json:"largestAdmittedPayloadBytes"`
	LargestPayloadDeliveries             uint64                `json:"largestPayloadDeliveries"`
	LargestPayloadDecodeP99Nanoseconds   int64                 `json:"largestPayloadDecodeP99Nanoseconds"`
	LargestPayloadEndToEndP99Nanoseconds int64                 `json:"largestPayloadEndToEndP99Nanoseconds"`
	Memory                               []DecoderMemorySample `json:"memory"`
	MissingSample                        []uint64              `json:"missingSample"`
}

type DecoderCompatibilityReport struct {
	CoreLogs                  uint64               `json:"coreLogs"`
	OracleRemovedDeployments  int                  `json:"oracleRemovedDeployments"`
	CoreParityWithoutOracle   bool                 `json:"coreParityWithoutOracle"`
	Optional                  []FamilyAvailability `json:"optional"`
	ActorReceipts             uint64               `json:"actorReceipts"`
	OptionalReceiptCount      uint64               `json:"optionalReceiptCount"`
	OptionalEconomicEffects   uint64               `json:"optionalEconomicEffects"`
	MatchedMirrorIdentities   uint64               `json:"matchedMirrorIdentities"`
	UnmatchedMirrorIdentities uint64               `json:"unmatchedMirrorIdentities"`
}

type DecoderMeasurementReport struct {
	SchemaVersion          int                         `json:"schemaVersion"`
	MeasurementInputSHA256 string                      `json:"measurementInputSha256"`
	Scope                  string                      `json:"scope"`
	StartedAt              time.Time                   `json:"startedAt"`
	FinishedAt             time.Time                   `json:"finishedAt"`
	CandidateSourceSHA256  string                      `json:"candidateSourceSha256"`
	CatalogSHA256          string                      `json:"catalogSha256"`
	DeploymentSHA256       string                      `json:"deploymentSha256"`
	ArchiveSHA256          string                      `json:"archiveSha256"`
	GoVersion              string                      `json:"goVersion"`
	Input                  DecoderMeasurementInput     `json:"input"`
	LogicalCPUs            int                         `json:"logicalCpus"`
	GoMaxProcs             int                         `json:"goMaxProcs"`
	Workers                int                         `json:"workers"`
	Coverage               []DecoderCoverageRow        `json:"coverage"`
	Gaps                   []string                    `json:"gaps"`
	ArchivedLogCount       uint64                      `json:"archivedLogCount"`
	ArchivedIdentitySHA256 string                      `json:"archivedIdentitySha256"`
	RestartParity          bool                        `json:"restartParity"`
	LargestDataBytes       int                         `json:"largestDataBytes"`
	Compatibility          DecoderCompatibilityReport  `json:"compatibility"`
	Burst                  *DecoderBurstReport         `json:"burst"`
	Passed                 bool                        `json:"passed"`
	Failure                string                      `json:"failure,omitempty"`
	CreationReplay         []CreationReplayMeasurement `json:"creationReplay"`
}

func measurementJSON(path, expected string, target any) ([]byte, error) {
	if !digest(expected) {
		return nil, ErrMeasurementInputs
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxMeasurementInputBytes+1))
	if err != nil || len(body) > maxMeasurementInputBytes {
		return nil, fmt.Errorf("%w: bounded JSON required", ErrMeasurementInputs)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("%w: input digest mismatch %s", ErrMeasurementInputs, filepath.Base(path))
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err = decoder.Decode(target); err != nil {
		return nil, err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing JSON", ErrMeasurementInputs)
	}
	return body, nil
}

func DecoderCandidateSourceDigest(root string) (string, error) {
	files := []string{}
	for _, relative := range []string{"services/launchpad/indexer/internal/decode", "services/launchpad/indexer/internal/chainreader", "services/launchpad/indexer/internal/receipt", "sdk/launchpad/protocol", "sdk/launchpad/shared/accounting"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("source symlink rejected")
			}
			if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".json") {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(relative))
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	files = append(files, "services/launchpad/indexer/go.mod", "services/launchpad/indexer/go.sum", "sdk/launchpad/protocol/go.mod", "sdk/launchpad/protocol/go.sum", "sdk/launchpad/shared/go.mod", "sdk/launchpad/shared/go.sum")
	sort.Strings(files)
	hash := sha256.New()
	for _, relative := range files {
		body, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(body)
		_, _ = fmt.Fprintf(hash, "%s%c%x\n", relative, byte(0), sum)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkedCatalogArtifacts(body []byte, root string) error {
	var catalog struct {
		Contracts []struct {
			Name, SourceName, ArtifactPath, ArtifactSHA256, SourceSHA256, BuildInfoSHA256, CompilerVersion string
			ABI                                                                                            json.RawMessage
			ABIRevision                                                                                    string `json:"abiSha256"`
		}
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		return err
	}
	for _, contract := range catalog.Contracts {
		for _, relative := range []string{contract.SourceName, contract.ArtifactPath} {
			if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), "..") {
				return ErrMeasurementInputs
			}
		}
		artifact, err := os.ReadFile(filepath.Join(root, contract.ArtifactPath))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(artifact)
		if hex.EncodeToString(sum[:]) != contract.ArtifactSHA256 {
			return fmt.Errorf("artifact drift: %s", contract.Name)
		}
		var generated struct {
			ABI                      json.RawMessage
			ContractName, SourceName string
		}
		if err = json.Unmarshal(artifact, &generated); err != nil {
			return err
		}
		var activeABI, artifactABI bytes.Buffer
		if err = json.Compact(&activeABI, contract.ABI); err != nil {
			return err
		}
		if err = json.Compact(&artifactABI, generated.ABI); err != nil {
			return err
		}
		if !bytes.Equal(activeABI.Bytes(), artifactABI.Bytes()) || generated.ContractName != contract.Name || generated.SourceName != contract.SourceName {
			return fmt.Errorf("active ABI disagrees with current artifact: %s", contract.Name)
		}
		source, err := os.ReadFile(filepath.Join(root, contract.SourceName))
		if err != nil {
			return err
		}
		sum = sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != contract.SourceSHA256 {
			return fmt.Errorf("source drift: %s", contract.Name)
		}
		var debug struct {
			BuildInfo string `json:"buildInfo"`
		}
		if data, err := os.ReadFile(filepath.Join(root, strings.TrimSuffix(contract.ArtifactPath, ".json")+".dbg.json")); err != nil {
			return err
		} else if err = json.Unmarshal(data, &debug); err != nil {
			return err
		}
		buildPath := filepath.Join(root, filepath.Dir(contract.ArtifactPath), debug.BuildInfo)
		relativeBuild, err := filepath.Rel(root, buildPath)
		if err != nil || strings.HasPrefix(relativeBuild, "..") || filepath.IsAbs(relativeBuild) {
			return ErrMeasurementInputs
		}
		build, err := os.ReadFile(buildPath)
		if err != nil {
			return err
		}
		sum = sha256.Sum256(build)
		if hex.EncodeToString(sum[:]) != contract.BuildInfoSHA256 {
			return fmt.Errorf("compiler input drift: %s", contract.Name)
		}
		var compiled struct {
			SolcVersion     string `json:"solcVersion"`
			SolcLongVersion string `json:"solcLongVersion"`
			Input           struct {
				Sources map[string]struct {
					Content string `json:"content"`
				} `json:"sources"`
			} `json:"input"`
		}
		if err = json.Unmarshal(build, &compiled); err != nil {
			return err
		}
		compiler := compiled.SolcLongVersion
		if compiler == "" {
			compiler = compiled.SolcVersion
		}
		if compiler != contract.CompilerVersion {
			return fmt.Errorf("compiler version disagreement: %s", contract.Name)
		}
		for name, entry := range compiled.Input.Sources {
			if !strings.HasPrefix(name, "contracts/") {
				continue
			}
			current, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				return err
			}
			if string(current) != entry.Content {
				return fmt.Errorf("compiled source drift: %s", name)
			}
		}
	}
	return nil
}

func coverageKey(address string, revision, signature string, from Order) string {
	return fmt.Sprintf("%s/%s/%s/%d/%d/%d", address, revision, signature, from.Block, from.Transaction, from.Log)
}

func releaseCoverage(r *Registry, at Order) []DecoderCoverageRow {
	out := []DecoderCoverageRow{}
	for address, epochs := range r.epochs {
		for _, epoch := range epochs {
			d := epoch.deployment
			for _, event := range epoch.events {
				state := "required"
				if d.Optional {
					state = "optional_deployed"
					if !d.Enabled {
						state = "unsupported"
					}
				}
				if declaredOnly(d.Role, event.Name) || d.Role == "EventEmitter" && optionalEvent(event.Name) && !optionalEnabled(d, event.Name) {
					state = "unsupported"
				}
				out = append(out, DecoderCoverageRow{Emitter: strings.ToLower(address.Hex()), From: d.From, Until: d.Until, Role: d.Role, ABIRevision: d.ABIRevision, Signature: event.Sig, EventName: event.Name, Family: eventFamily(d.Role, event.Name), Authority: authority(d.Role, event.Name), State: state, Current: !at.less(d.From) && (d.Until == nil || at.less(*d.Until))})
			}
		}
	}
	deployed := map[string]bool{}
	for _, row := range out {
		deployed[row.Role+"/"+row.ABIRevision] = true
	}
	for _, row := range r.Coverage() {
		if !deployed[row.Role+"/"+row.ABIRevision] {
			out = append(out, DecoderCoverageRow{Role: row.Role, ABIRevision: row.ABIRevision, Signature: row.Signature, EventName: row.EventName, Family: row.ProcessorRoute, Authority: row.Authority, State: "unsupported", Gap: "deployment_not_attested"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return coverageKey(out[i].Emitter, out[i].ABIRevision, out[i].Signature, out[i].From) < coverageKey(out[j].Emitter, out[j].ABIRevision, out[j].Signature, out[j].From)
	})
	return out
}

func expectedArgs(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, ErrMeasurementInputs
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func matchesArchived(e *DecodedEnvelope, record ArchivedDecoderLog) error {
	expected := record.Expected
	if e == nil || e.Raw.Identity != rawMetadata(record.Log).Identity || e.ABIRevision != expected.ABIRevision || e.EmitterRole != expected.Role || e.EventName != expected.EventName || e.EventSignature != expected.Signature || e.Status != expected.Status || e.FailureReason != expected.FailureReason || e.Opaque != expected.Opaque || !reflect.DeepEqual(e.Alerts, expected.Alerts) {
		return fmt.Errorf("independent archived expectation mismatch: %s/%d", record.Log.TxHash, record.Log.Index)
	}
	if rawSizeAllowed(record.Log) {
		if !reflect.DeepEqual(e.Raw, rawLog(record.Log)) {
			return fmt.Errorf("archived raw history lost")
		}
	} else {
		summary := summarizeRaw(record.Log)
		if !reflect.DeepEqual(e.RawSummary, summary) || e.Raw.Data != "" || len(e.Raw.Topics) != 0 {
			return fmt.Errorf("bounded quarantine raw summary mismatch")
		}
	}
	if e.Event != nil {
		actual, err := json.Marshal(e.Event.Args)
		if err != nil {
			return err
		}
		expected, err := expectedArgs(expected.Args)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("independent indexed/nonindexed fields mismatch")
		}
	}
	return nil
}

func RunDecoderMeasurement(ctx context.Context, input DecoderMeasurementInput) (report *DecoderMeasurementReport, err error) {
	report = &DecoderMeasurementReport{SchemaVersion: 1, Scope: "native_decoder_archive_and_burst_measurement_shared_acceptance_pending", StartedAt: time.Now().UTC(), CandidateSourceSHA256: input.CandidateSourceSHA256, CatalogSHA256: input.CatalogSHA256, DeploymentSHA256: input.DeploymentSHA256, ArchiveSHA256: input.ArchiveSHA256, GoVersion: runtime.Version(), Input: input, LogicalCPUs: runtime.NumCPU(), GoMaxProcs: runtime.GOMAXPROCS(0), Workers: input.Workers, Gaps: []string{}}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err != nil {
			report.Failure = err.Error()
		}
	}()
	if input.SchemaVersion != 1 || !digest(input.CandidateSourceSHA256) || input.Workers < 1 || input.Workers > 32 || input.MaxP99Nanoseconds <= 0 || input.MaxHeapGrowthBytes == 0 || input.MaxAllocatedBytesPerDelivery == 0 || input.SourceRoot == "" || input.ContractsRoot == "" {
		return report, ErrMeasurementInputs
	}
	approvedRoot, rootErr := filepath.Abs(input.SourceRoot)
	contractsRoot, contractsErr := filepath.Abs(input.ContractsRoot)
	if rootErr != nil || contractsErr != nil || contractsRoot != filepath.Join(approvedRoot, "contracts", "launchpad") {
		return report, fmt.Errorf("%w: actual source contracts/launchpad required", ErrMeasurementInputs)
	}
	source, err := DecoderCandidateSourceDigest(input.SourceRoot)
	if err != nil || source != input.CandidateSourceSHA256 {
		return report, fmt.Errorf("%w: current source digest", ErrMeasurementInputs)
	}
	catalog, err := os.ReadFile(input.CatalogPath)
	if err != nil {
		return report, err
	}
	if len(catalog) > maxMeasurementInputBytes {
		return report, ErrMeasurementInputs
	}
	sum := sha256.Sum256(catalog)
	if hex.EncodeToString(sum[:]) != input.CatalogSHA256 {
		return report, ErrMeasurementInputs
	}
	if err = checkedCatalogArtifacts(catalog, input.ContractsRoot); err != nil {
		return report, err
	}
	var deployment DecoderDeploymentInput
	if _, err = measurementJSON(input.DeploymentPath, input.DeploymentSHA256, &deployment); err != nil {
		return report, err
	}
	var catalogIdentity Catalog
	if err = json.Unmarshal(catalog, &catalogIdentity); err != nil {
		return report, err
	}
	if deployment.SchemaVersion != 1 || deployment.ManifestSHA256 != catalogIdentity.ManifestSHA256 || !digest(deployment.ManifestSHA256) || len(deployment.Deployments) == 0 || len(deployment.Deployments) > 256 || len(deployment.Capabilities) != 6 {
		return report, ErrMeasurementInputs
	}
	var corpus ArchivedDecoderCorpus
	if _, err = measurementJSON(input.ArchivePath, input.ArchiveSHA256, &corpus); err != nil {
		return report, err
	}
	if corpus.SchemaVersion != 1 || corpus.ChainID != 125 || corpus.CatalogSHA256 != input.CatalogSHA256 || len(corpus.Logs) == 0 || len(corpus.Logs) > MaxRawBatchLogs || len(corpus.ActorReceipts) == 0 || len(corpus.ActorReceipts) > 64 {
		return report, ErrMeasurementInputs
	}
	r, err := NewRegistry(catalog, deployment.Deployments)
	if err != nil {
		return report, err
	}
	restart, err := NewRegistry(catalog, deployment.Deployments)
	if err != nil {
		return report, err
	}
	report.Coverage = releaseCoverage(r, input.ActiveOrder)
	rows := map[string]int{}
	for i, row := range report.Coverage {
		rows[coverageKey(row.Emitter, row.ABIRevision, row.Signature, row.From)] = i
	}
	seen := map[RawIdentity]bool{}
	identities := sha256.New()
	envelopes := make([]*DecodedEnvelope, len(corpus.Logs))
	aggregate := 0
	uintMax, intMin, nonDefault, fieldMax, sameBlock := false, false, false, false, false
	boolWords := map[uint64]bool{}
	overEnvelope, overField, malformedArity, unknownAction := false, false, false, false
	umax := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
	imin := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255)).String()
	var previous *DecodedEnvelope
	for i, record := range corpus.Logs {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if len(record.Log.Topics) > 16 || len(record.Log.Data) > MaxEnvelopeBytes+MaxFieldBytes || !validIdentity(record.Log) || record.Log.Removed {
			return report, ErrMeasurementInputs
		}
		aggregate += len(record.Log.Data) + 32*len(record.Log.Topics)
		if aggregate > MaxRawBatchBytes {
			return report, ErrMeasurementInputs
		}
		envelope, decodeErr := r.Decode(record.Log)
		if decodeErr != nil && envelope == nil {
			return report, decodeErr
		}
		if err = matchesArchived(envelope, record); err != nil {
			return report, err
		}
		identity := envelope.Raw.Identity
		if seen[identity] {
			return report, fmt.Errorf("%w: duplicated archived raw identity", ErrMeasurementInputs)
		}
		seen[identity] = true
		body, _ := json.Marshal(identity)
		_, _ = identities.Write(body)
		replay, _ := restart.Decode(record.Log)
		if !reflect.DeepEqual(envelope, replay) {
			return report, fmt.Errorf("restart envelope drift")
		}
		envelopes[i] = envelope
		if envelope.Status == "quarantined" {
			if envelope.FailureReason == "bounds" {
				if len(record.Log.Data)+32*len(record.Log.Topics) > MaxEnvelopeBytes {
					overEnvelope = true
				} else if oversizedArchivedField(r, record.Log) {
					overField = true
				}
			}
			if envelope.FailureReason == "arity" {
				malformedArity = true
			}
		}
		for _, alert := range envelope.Alerts {
			if strings.HasPrefix(alert, "unknown_action:") {
				unknownAction = true
			}
		}
		if len(record.Log.Data) > report.LargestDataBytes {
			report.LargestDataBytes = len(record.Log.Data)
		}
		if envelope.Event != nil {
			for _, epoch := range r.epochs[record.Log.Address] {
				d := epoch.deployment
				if !envelope.Raw.Order.less(d.From) && (d.Until == nil || envelope.Raw.Order.less(*d.Until)) {
					if index, ok := rows[coverageKey(envelope.Raw.Emitter, envelope.ABIRevision, envelope.Event.Sig, d.From)]; ok {
						report.Coverage[index].Archived++
					}
					definition := epoch.events[record.Log.Topics[0]]
					u, i, n, f := archivedScalarCases(definition, envelope.Event.Args, umax, imin)
					uintMax = uintMax || u
					intMin = intMin || i
					nonDefault = nonDefault || n
					fieldMax = fieldMax || f
					collectBooleanWords(definition, record.Log, d.PaxeerNonzeroBools[envelope.Event.Sig], boolWords)
				}
			}
		}
		if previous != nil && previous.EventName == "Upgraded" && previous.Raw.Emitter == envelope.Raw.Emitter && previous.Raw.Order.Block == envelope.Raw.Order.Block && previous.Raw.Order.less(envelope.Raw.Order) && previous.ABIRevision != envelope.ABIRevision && envelope.Event != nil && envelope.Status == "decoded" {
			sameBlock = true
		}
		if envelope.EventName == "Upgraded" {
			previous = envelope
		}
	}
	report.ArchivedLogCount = uint64(len(corpus.Logs))
	report.ArchivedIdentitySHA256 = hex.EncodeToString(identities.Sum(nil))
	report.RestartParity = true
	for i, row := range report.Coverage {
		if row.State == "unsupported" {
			continue
		}
		if row.Family == "" {
			report.Coverage[i].Gap = "decoder_route_missing"
		} else if row.Archived == 0 {
			report.Coverage[i].Gap = "archived_epoch_event_missing"
		}
		if report.Coverage[i].Gap != "" {
			report.Gaps = append(report.Gaps, coverageKey(row.Emitter, row.ABIRevision, row.Signature, row.From)+":"+report.Coverage[i].Gap)
		}
	}
	for name, present := range map[string]bool{"uint256_max": uintMax, "int256_min": intMin, "non_default_uint256": nonDefault, "maximum_field_payload": fieldMax, "same_block_abi_upgrade": sameBlock, "approved_boolean_words_0_1_2": boolWords[0] && boolWords[1] && boolWords[2], "over_envelope_quarantined": overEnvelope, "over_field_quarantined": overField, "malformed_arity_quarantined": malformedArity, "unknown_action_raw_preserved": unknownAction} {
		if !present {
			report.Gaps = append(report.Gaps, "archive_case_missing:"+name)
		}
	}
	optional, err := r.OptionalAvailability(input.ActiveOrder, deployment.Capabilities)
	if err != nil {
		return report, err
	}
	report.Compatibility.Optional = optional
	for _, availability := range optional {
		if availability.PublicEnabled && !availability.Available {
			report.Gaps = append(report.Gaps, "enabled_capability_unavailable:"+availability.Family)
		}
	}
	noOracle := make([]Deployment, 0, len(deployment.Deployments))
	for _, d := range deployment.Deployments {
		if optionalFamily(d.Role, "") == "oracle" {
			report.Compatibility.OracleRemovedDeployments++
			continue
		}
		copy := d
		copy.EnabledOptionalEvents = nil
		for _, name := range d.EnabledOptionalEvents {
			if optionalFamily("EventEmitter", name) != "oracle" {
				copy.EnabledOptionalEvents = append(copy.EnabledOptionalEvents, name)
			}
		}
		noOracle = append(noOracle, copy)
	}
	without, err := NewRegistry(catalog, noOracle)
	if err != nil {
		return report, err
	}
	for i, record := range corpus.Logs {
		original := envelopes[i]
		if original.Event == nil || optionalFamily(original.EmitterRole, original.EventName) != "" {
			continue
		}
		isolated, _ := without.Decode(record.Log)
		if !reflect.DeepEqual(original, isolated) {
			return report, fmt.Errorf("core decode depends on optional oracle")
		}
		report.Compatibility.CoreLogs++
	}
	if report.Compatibility.CoreLogs == 0 {
		return report, ErrMeasurementInputs
	}
	report.Compatibility.CoreParityWithoutOracle = true
	for _, archived := range corpus.ActorReceipts {
		binaryTx, err := hex.DecodeString(strings.TrimPrefix(archived.TransactionHex, "0x"))
		if err != nil {
			return report, err
		}
		var tx ethtypes.Transaction
		if err = tx.UnmarshalBinary(binaryTx); err != nil {
			return report, err
		}
		actors, err := r.DecodeTransactionReceipt(&tx, &archived.Receipt, deployment.Markets)
		if err != nil {
			return report, err
		}
		found := 0
		for _, event := range actors {
			if expected, ok := archived.ExpectedActors[event.Raw.Identity.LogIndex]; ok {
				if event.Actors == nil || !reflect.DeepEqual(*event.Actors, expected) {
					return report, fmt.Errorf("independent actor evidence mismatch")
				}
				found++
			}
		}
		if found == 0 || found != len(archived.ExpectedActors) {
			return report, ErrMeasurementInputs
		}
		report.Compatibility.ActorReceipts++
	}
	optionalEnabledCount := 0
	for _, d := range deployment.Deployments {
		if d.Optional && d.Enabled && optionalFamily(d.Role, "") != "oracle" {
			optionalEnabledCount++
		}
	}
	if len(corpus.OptionalReceipts) > 64 {
		return report, ErrMeasurementInputs
	}
	if optionalEnabledCount > 0 && len(corpus.OptionalReceipts) == 0 {
		report.Gaps = append(report.Gaps, "archive_case_missing:optional_native_mirror_receipts")
	}
	for _, archived := range corpus.OptionalReceipts {
		correlated, err := r.CorrelateOptionalReceipt(&archived.Receipt, deployment.Correlations)
		if err != nil {
			return report, err
		}
		actual := make([]ArchivedOptionalEffect, 0, len(correlated.Effects))
		for _, effect := range correlated.Effects {
			actual = append(actual, ArchivedOptionalEffect{Identity: effect.Identity, Family: effect.Family, Action: effect.Action, Companions: effect.Companions, Economic: effect.Economic})
			if effect.Economic {
				report.Compatibility.OptionalEconomicEffects++
			}
			report.Compatibility.MatchedMirrorIdentities += uint64(len(effect.Companions))
		}
		if !reflect.DeepEqual(actual, archived.ExpectedEffects) || !reflect.DeepEqual(correlated.Unmatched, archived.ExpectedUnmatched) {
			return report, fmt.Errorf("independent optional native/mirror identity mismatch")
		}
		report.Compatibility.UnmatchedMirrorIdentities += uint64(len(correlated.Unmatched))
		report.Compatibility.OptionalReceiptCount++
	}
	report.Burst, err = measureDecoderBurst(ctx, r, corpus.Logs, envelopes, input)
	if err != nil {
		return report, err
	}
	sort.Strings(report.Gaps)
	if len(report.Gaps) > 0 {
		return report, fmt.Errorf("%w: %d explicit capability/archive gaps", ErrMeasurementAcceptance, len(report.Gaps))
	}
	for _, binding := range [][2]string{{input.CatalogPath, input.CatalogSHA256}, {input.DeploymentPath, input.DeploymentSHA256}, {input.ArchivePath, input.ArchiveSHA256}} {
		if err = unchangedMeasurementFile(binding[0], binding[1]); err != nil {
			return report, err
		}
	}
	if err = checkedCatalogArtifacts(catalog, input.ContractsRoot); err != nil {
		return report, err
	}
	after, err := DecoderCandidateSourceDigest(input.SourceRoot)
	if err != nil || after != source {
		return report, fmt.Errorf("%w: candidate changed during measurement", ErrMeasurementInputs)
	}
	report.Passed = true
	return report, nil
}

type decoderDelivery struct {
	id        uint64
	record    int
	scheduled time.Time
}

func measurementPercentile(samples []int64) int64 {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[(99*len(samples)+99)/100-1]
}

func memorySample(start time.Time) DecoderMemorySample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return DecoderMemorySample{ElapsedNanoseconds: time.Since(start).Nanoseconds(), HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse, TotalAlloc: m.TotalAlloc, Mallocs: m.Mallocs, GC: m.NumGC}
}

func measureDecoderBurst(ctx context.Context, r *Registry, records []ArchivedDecoderLog, expected []*DecodedEnvelope, input DecoderMeasurementInput) (*DecoderBurstReport, error) {
	count := DecoderBurstRate * DecoderBurstSeconds
	report := &DecoderBurstReport{AllocationScope: "whole_process_delta_during_decoder_burst_including_instrumentation", HeapGrowthScope: "maximum_one_second_sampled_heap_alloc_minus_post_gc_initial_heap_alloc", MemorySamplePeriodNanoseconds: int64(time.Second), WorkloadSource: "approved_archived_log_repetition_with_unchanged_raw_identities", DeliveryIdentityScope: "run_local_monotonic_delivery_id_independent_of_onchain_raw_identity", TargetRate: DecoderBurstRate, TargetSeconds: DecoderBurstSeconds, MissingSample: []uint64{}, Memory: make([]DecoderMemorySample, 0, DecoderBurstSeconds+2)}
	decodeLatency := make([]int64, count)
	endLatency := make([]int64, count)
	dispatchLatency := make([]int64, count)
	completed := make([]atomic.Uint64, (count+63)/64)
	identities := make([][]byte, len(records))
	for i, record := range records {
		identities[i], _ = json.Marshal(rawMetadata(record.Log).Identity)
	}
	jobs := make(chan decoderDelivery, 2048)
	var workers sync.WaitGroup
	var total, typed, opaque, quarantined, duplicates, identityFailures, afterWindow atomic.Uint64
	var windowEnd time.Time
	burstCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for worker := 0; worker < input.Workers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-burstCtx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					started := time.Now()
					event, err := r.Decode(records[job.record].Log)
					decodedAt := time.Now()
					if event == nil || event.Raw.Identity != expected[job.record].Raw.Identity || event.Status != expected[job.record].Status || event.ABIRevision != expected[job.record].ABIRevision || event.EventName != expected[job.record].EventName || (err != nil && event.Status != "quarantined") {
						identityFailures.Add(1)
						continue
					}
					bit := uint64(1) << (job.id % 64)
					if completed[job.id/64].Or(bit)&bit != 0 {
						duplicates.Add(1)
						continue
					}
					decodeLatency[job.id] = decodedAt.Sub(started).Nanoseconds()
					endLatency[job.id] = decodedAt.Sub(job.scheduled).Nanoseconds()
					if event.Status == "decoded" {
						typed.Add(1)
					} else if event.Status == "quarantined" {
						quarantined.Add(1)
					} else {
						opaque.Add(1)
					}
					if decodedAt.After(windowEnd) {
						afterWindow.Add(1)
					}
					total.Add(1)
				}
			}
		}()
	}
	runtime.GC()
	start := time.Now()
	windowEnd = start.Add(DecoderBurstSeconds * time.Second)
	baseline := memorySample(start)
	report.Memory = append(report.Memory, baseline)
	memoryStop := make(chan struct{})
	memoryDone := make(chan struct{})
	go func() {
		defer close(memoryDone)
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-memoryStop:
				return
			case <-timer.C:
				report.Memory = append(report.Memory, memorySample(start))
			}
		}
	}()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	waitUntil := func(target time.Time) error {
		remaining := time.Until(target)
		if remaining <= 0 {
			return ctx.Err()
		}
		timer.Reset(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	var schedulingError error
dispatchLoop:
	for batch := 0; batch < DecoderBurstSeconds*100; batch++ {
		due := start.Add(time.Duration(batch) * 10 * time.Millisecond)
		if schedulingError = waitUntil(due); schedulingError != nil {
			break
		}
		for offset := 0; offset < DecoderBurstRate/100; offset++ {
			if time.Now().After(windowEnd) {
				schedulingError = fmt.Errorf("five-minute workload dispatch deadline exceeded")
				break dispatchLoop
			}
			id := uint64(batch*(DecoderBurstRate/100) + offset)
			report.Scheduled++
			dispatchLatency[id] = time.Since(due).Nanoseconds()
			job := decoderDelivery{id: id, record: int(id) % len(records), scheduled: due}
			select {
			case jobs <- job:
				report.Enqueued++
			default:
				report.AdmissionDrops++
			}
		}
	}
	if schedulingError == nil {
		schedulingError = waitUntil(start.Add(DecoderBurstSeconds * time.Second))
	}
	close(jobs)
	finished := make(chan struct{})
	go func() { workers.Wait(); close(finished) }()
	drain := time.NewTimer(5 * time.Second)
	select {
	case <-finished:
	case <-ctx.Done():
		schedulingError = ctx.Err()
		cancel()
		<-finished
	case <-drain.C:
		schedulingError = fmt.Errorf("decoder drain deadline exceeded")
		cancel()
		<-finished
	}
	if !drain.Stop() {
		select {
		case <-drain.C:
		default:
		}
	}
	close(memoryStop)
	<-memoryDone
	final := memorySample(start)
	report.Memory = append(report.Memory, final)
	report.DurationNanoseconds = time.Since(start).Nanoseconds()
	report.Completed = total.Load()
	report.Decoded = typed.Load()
	report.PreservedOpaque = opaque.Load()
	report.PreservedQuarantined = quarantined.Load()
	report.DuplicateDeliveries = duplicates.Load()
	report.IdentityFailures = identityFailures.Load()
	report.CompletedAfterWindow = afterWindow.Load()
	report.AllocatedBytes = final.TotalAlloc - baseline.TotalAlloc
	report.AllocationCount = final.Mallocs - baseline.Mallocs
	if report.Completed > 0 {
		report.BytesPerDelivery = report.AllocatedBytes / report.Completed
		if report.AllocatedBytes%report.Completed != 0 {
			report.BytesPerDelivery++
		}
	}
	peak := baseline.HeapAlloc
	for _, sample := range report.Memory {
		if sample.HeapAlloc > peak {
			peak = sample.HeapAlloc
		}
	}
	report.HeapGrowthBytes = peak - baseline.HeapAlloc
	inputHash, observedHash := sha256.New(), sha256.New()
	var deliveryID [8]byte
	used := 0
	largest := 0
	for i, record := range records {
		if expected[i].Status == "decoded" && len(record.Log.Data) > largest {
			largest = len(record.Log.Data)
		}
	}
	report.LargestPayloadBytes = largest
	largestDecode, largestEnd := []int64{}, []int64{}
	for id := 0; id < count; id++ {
		binary.BigEndian.PutUint64(deliveryID[:], uint64(id))
		_, _ = inputHash.Write(deliveryID[:])
		_, _ = inputHash.Write(identities[id%len(records)])
		if completed[id/64].Load()&(uint64(1)<<(uint(id)%64)) == 0 {
			report.MissingDeliveries++
			if len(report.MissingSample) < 64 {
				report.MissingSample = append(report.MissingSample, uint64(id))
			}
			continue
		}
		_, _ = observedHash.Write(deliveryID[:])
		_, _ = observedHash.Write(identities[id%len(records)])
		if expected[id%len(records)].Status == "decoded" && len(records[id%len(records)].Log.Data) == largest {
			largestDecode = append(largestDecode, decodeLatency[id])
			largestEnd = append(largestEnd, endLatency[id])
			report.LargestPayloadDeliveries++
		}
		decodeLatency[used] = decodeLatency[id]
		endLatency[used] = endLatency[id]
		used++
	}
	report.InputIdentitySHA256 = hex.EncodeToString(inputHash.Sum(nil))
	report.ObservedIdentitySHA256 = hex.EncodeToString(observedHash.Sum(nil))
	report.LargestPayloadDecodeP99Nanoseconds = measurementPercentile(largestDecode)
	report.LargestPayloadEndToEndP99Nanoseconds = measurementPercentile(largestEnd)
	report.DecodeP99Nanoseconds = measurementPercentile(decodeLatency[:used])
	report.EndToEndP99Nanoseconds = measurementPercentile(endLatency[:used])
	report.DispatchP99Nanoseconds = measurementPercentile(dispatchLatency[:int(report.Scheduled)])
	if schedulingError != nil {
		return report, schedulingError
	}
	if report.Scheduled != uint64(count) || report.Enqueued != uint64(count) || report.Completed != uint64(count) || report.AdmissionDrops != 0 || report.MissingDeliveries != 0 || report.DuplicateDeliveries != 0 || report.IdentityFailures != 0 || report.CompletedAfterWindow != 0 || report.InputIdentitySHA256 != report.ObservedIdentitySHA256 || report.DurationNanoseconds < int64(DecoderBurstSeconds*time.Second) {
		return report, fmt.Errorf("%w: burst identity/delivery conservation", ErrMeasurementAcceptance)
	}
	if report.EndToEndP99Nanoseconds > input.MaxP99Nanoseconds || report.HeapGrowthBytes > input.MaxHeapGrowthBytes || report.BytesPerDelivery > input.MaxAllocatedBytesPerDelivery {
		return report, fmt.Errorf("%w: declared latency/heap/allocation budgets", ErrMeasurementAcceptance)
	}
	return report, nil
}

func WriteDecoderMeasurement(path string, report *DecoderMeasurementReport) error {
	if report == nil || path == "" {
		return ErrMeasurementInputs
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(body, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func LoadDecoderMeasurementInput(path, sha string) (DecoderMeasurementInput, error) {
	var input DecoderMeasurementInput
	_, err := measurementJSON(path, sha, &input)
	return input, err
}
func LoadDecoderMeasurementCorpus(input DecoderMeasurementInput) (ArchivedDecoderCorpus, error) {
	var corpus ArchivedDecoderCorpus
	_, err := measurementJSON(input.ArchivePath, input.ArchiveSHA256, &corpus)
	return corpus, err
}
func LoadDecoderMeasurementDeployments(input DecoderMeasurementInput) (DecoderDeploymentInput, error) {
	var deployments DecoderDeploymentInput
	_, err := measurementJSON(input.DeploymentPath, input.DeploymentSHA256, &deployments)
	return deployments, err
}

func abiHeadWords(t abi.Type) (int, bool) {
	switch t.T {
	case abi.BytesTy, abi.StringTy, abi.SliceTy:
		return 1, true
	case abi.ArrayTy:
		width, dynamic := abiHeadWords(*t.Elem)
		if dynamic {
			return 1, true
		}
		return t.Size * width, false
	case abi.TupleTy:
		words := 0
		for _, element := range t.TupleElems {
			width, dynamic := abiHeadWords(*element)
			if dynamic {
				return 1, true
			}
			words += width
		}
		return words, false
	default:
		return 1, false
	}
}
func collectBooleanWords(event abi.Event, log ethtypes.Log, approved []string, out map[uint64]bool) {
	permitted := map[string]bool{}
	for _, field := range approved {
		permitted[field] = true
	}
	topic, offset := 1, 0
	for _, input := range event.Inputs {
		var word []byte
		if input.Indexed {
			if topic < len(log.Topics) {
				word = log.Topics[topic][:]
			}
			topic++
		} else {
			if offset+32 <= len(log.Data) {
				word = log.Data[offset : offset+32]
			}
			width, _ := abiHeadWords(input.Type)
			offset += 32 * width
		}
		if input.Type.T == abi.BoolTy && permitted[input.Name] && len(word) == 32 {
			number := new(big.Int).SetBytes(word)
			if number.IsUint64() && number.Uint64() <= 2 {
				out[number.Uint64()] = true
			}
		}
	}
}

func archivedScalarCases(event abi.Event, args map[string]any, umax, imin string) (uintMax, intMin, nonDefault, fieldMax bool) {
	var visit func(abi.Type, any)
	visit = func(typ abi.Type, value any) {
		switch typ.T {
		case abi.UintTy, abi.IntTy:
			text, ok := value.(string)
			if !ok {
				return
			}
			if typ.T == abi.UintTy && typ.Size == 256 {
				uintMax = uintMax || text == umax
				if number, ok := new(big.Int).SetString(text, 10); ok && number.BitLen() > 53 {
					nonDefault = true
				}
			}
			if typ.T == abi.IntTy && typ.Size == 256 {
				intMin = intMin || text == imin
			}
		case abi.StringTy:
			text, ok := value.(string)
			fieldMax = fieldMax || ok && len(text) == MaxFieldBytes
		case abi.BytesTy:
			text, ok := value.(string)
			fieldMax = fieldMax || ok && strings.HasPrefix(text, "0x") && len(text) == 2+MaxFieldBytes*2
		case abi.SliceTy, abi.ArrayTy:
			if values, ok := value.([]any); ok {
				for _, entry := range values {
					visit(*typ.Elem, entry)
				}
			}
		case abi.TupleTy:
			if values, ok := value.(map[string]any); ok {
				for i, element := range typ.TupleElems {
					visit(*element, values[typ.TupleRawNames[i]])
				}
			}
		}
	}
	for _, input := range event.Inputs {
		visit(input.Type, args[input.Name])
	}
	return
}

func oversizedArchivedField(r *Registry, log ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	order := Order{Block: log.BlockNumber, Transaction: log.TxIndex, Log: log.Index}
	for _, epoch := range r.epochs[log.Address] {
		d := epoch.deployment
		if order.less(d.From) || (d.Until != nil && !order.less(*d.Until)) {
			continue
		}
		event, ok := epoch.events[log.Topics[0]]
		if !ok {
			return false
		}
		inputs := append(abi.Arguments(nil), event.Inputs.NonIndexed()...)
		approved := map[string]bool{}
		for _, field := range d.PaxeerNonzeroBools[event.Sig] {
			approved[field] = true
		}
		for i, input := range inputs {
			if input.Type.T == abi.BoolTy && approved[input.Name] {
				inputs[i].Type = abiUint256
			}
		}
		values, err := inputs.Unpack(log.Data)
		if err != nil {
			return false
		}
		for _, value := range values {
			if overSizedValue(reflect.ValueOf(value)) {
				return true
			}
		}
	}
	return false
}
func overSizedValue(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		return overSizedValue(value.Elem())
	}
	switch value.Kind() {
	case reflect.String:
		return value.Len() > MaxFieldBytes
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 && value.Len() > MaxFieldBytes {
			return true
		}
		fallthrough
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if overSizedValue(value.Index(i)) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if overSizedValue(value.Field(i)) {
				return true
			}
		}
	}
	return false
}

func unchangedMeasurementFile(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, maxMeasurementInputBytes+1))
	if err != nil {
		return err
	}
	if count > maxMeasurementInputBytes || hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("%w: input changed during measurement", ErrMeasurementInputs)
	}
	return nil
}
