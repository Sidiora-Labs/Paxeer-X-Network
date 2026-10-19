package client

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cosmos/btcutil/bech32"
	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seictl/sidecar/wire"
)

const seiBech32HRP = "sei"

// btcutil's bech32 decode validates checksum + alphabet; cheaper than
// pulling sei-cosmos's sdk.AccAddressFromBech32 just for client-side
// validation. Sidecar still does the SDK-shape check server-side.
func validateSeiAccountAddress(addr string) error {
	hrp, _, err := bech32.Decode(addr, 1023) // bech32 spec max length
	if err != nil {
		return fmt.Errorf("address %q: %w", addr, err)
	}
	if hrp != seiBech32HRP {
		return fmt.Errorf("address %q: hrp %q, expected %q", addr, hrp, seiBech32HRP)
	}
	return nil
}

// TaskBuilder is implemented by every typed task struct. It converts a
// strongly-typed task description into the generic TaskRequest wire format
// and validates required fields before submission.
type TaskBuilder interface {
	TaskType() string
	Validate() error
	ToTaskRequest() TaskRequest
}

// Task type constants re-exported from wire for external consumers.
const (
	TaskTypeSnapshotRestore    = string(wire.TaskSnapshotRestore)
	TaskTypeConfigPatch        = string(wire.TaskConfigPatch)
	TaskTypeConfigApply        = string(wire.TaskConfigApply)
	TaskTypeConfigValidate     = string(wire.TaskConfigValidate)
	TaskTypeConfigReload       = string(wire.TaskConfigReload)
	TaskTypeMarkReady          = string(wire.TaskMarkReady)
	TaskTypeRestartSeid        = string(wire.TaskRestartSeid)
	TaskTypeConfigureGenesis   = string(wire.TaskConfigureGenesis)
	TaskTypeConfigureStateSync = string(wire.TaskConfigureStateSync)
	TaskTypeSnapshotUpload     = string(wire.TaskSnapshotUpload)
	TaskTypeSnapshotUploadOnce = string(wire.TaskSnapshotUploadOnce)
	TaskTypeResultExport       = string(wire.TaskResultExport)
	TaskTypeAwaitCondition     = string(wire.TaskAwaitCondition)

	TaskTypeGenerateIdentity       = string(wire.TaskGenerateIdentity)
	TaskTypeGenerateGentx          = string(wire.TaskGenerateGentx)
	TaskTypeUploadGenesisArtifacts = string(wire.TaskUploadGenesisArtifacts)
	TaskTypeAssembleGenesis        = string(wire.TaskAssembleAndUploadGenesis)
	TaskTypeSetGenesisPeers        = string(wire.TaskSetGenesisPeers)

	TaskTypeGovVote            = string(wire.TaskGovVote)
	TaskTypeGovSoftwareUpgrade = string(wire.TaskGovSoftwareUpgrade)
	TaskTypeGovParamChange     = string(wire.TaskGovParamChange)

	TaskTypeMarkNotReady = string(wire.TaskMarkNotReady)
	TaskTypeStopSeid     = string(wire.TaskStopSeid)
	TaskTypeResetData    = string(wire.TaskResetData)
)

// Snapshot-upload outcome contract, re-exported from wire so CLI consumers
// classify a result against the same definition the handler emits.
type (
	UploadOutcome = wire.UploadOutcome
	NoopReason    = wire.NoopReason
)

const (
	OutcomeUploaded = wire.OutcomeUploaded
	OutcomeNoop     = wire.OutcomeNoop
	OutcomeError    = wire.OutcomeError

	NoopFewerThanTwoSnapshots = wire.NoopFewerThanTwoSnapshots
	NoopAlreadyUploaded       = wire.NoopAlreadyUploaded
)

// Known condition and action values for AwaitConditionTask.
const (
	ConditionHeight     = "height"
	ConditionCatchingUp = "catchingUp"
	ActionSIGTERM       = "SIGTERM_SEID"
)

// SnapshotRestoreTask downloads and extracts a snapshot archive from S3.
// S3 coordinates are derived by the sidecar from its environment.
// TargetHeight selects the highest available snapshot <= that height.
// When zero, the latest snapshot (from latest.txt) is used.
type SnapshotRestoreTask struct {
	TargetHeight int64
}

func (t SnapshotRestoreTask) TaskType() string { return TaskTypeSnapshotRestore }

func (t SnapshotRestoreTask) Validate() error { return nil }

func (t SnapshotRestoreTask) ToTaskRequest() TaskRequest {
	var p *map[string]interface{}
	if t.TargetHeight > 0 {
		m := map[string]interface{}{"targetHeight": t.TargetHeight}
		p = &m
	}
	req := TaskRequest{Type: t.TaskType(), Params: p}
	return req
}

// SnapshotUploadTask archives and streams a local snapshot to S3.
// S3 coordinates are derived by the sidecar from its environment.
type SnapshotUploadTask struct {
}

func (t SnapshotUploadTask) TaskType() string { return TaskTypeSnapshotUpload }

func (t SnapshotUploadTask) Validate() error { return nil }

func (t SnapshotUploadTask) ToTaskRequest() TaskRequest {
	req := TaskRequest{Type: t.TaskType()}
	return req
}

// SnapshotUploadOnceTask runs a single snapshot upload and reaches a real
// terminal (completed with an outcome, or failed) rather than looping. A poller
// reads the structured result to distinguish uploaded / noop / error. S3
// coordinates and the per-task deadline are derived by the sidecar from its
// environment.
//
// Each invocation MUST be submitted with a fresh, unique task ID. The engine
// coalesces a resubmit onto an existing Completed row without re-running, so a
// reused ID reads back the prior run's stale result and never uploads.
type SnapshotUploadOnceTask struct{}

func (t SnapshotUploadOnceTask) TaskType() string { return TaskTypeSnapshotUploadOnce }

func (t SnapshotUploadOnceTask) Validate() error { return nil }

func (t SnapshotUploadOnceTask) ToTaskRequest() TaskRequest {
	return TaskRequest{Type: t.TaskType()}
}

// ConfigureGenesisTask instructs the sidecar to resolve and write genesis.json.
// The sidecar resolves genesis from its chain ID: embedded config is checked
// first, then S3 fallback at {bucket}/{chainID}/genesis.json using env vars.
//
// ExpectedGenesisHash is the bare SHA-256 hex digest (no "sha256:" prefix) the
// downloaded genesis.json must match. When set it gates the S3 download and the
// sidecar fails closed on mismatch. When empty the download is unverified —
// callers that omit it (and the field is omitted from the wire request) keep
// the sidecar's pre-verification behavior.
type ConfigureGenesisTask struct {
	ExpectedGenesisHash string
}

func (t ConfigureGenesisTask) TaskType() string { return TaskTypeConfigureGenesis }

func (t ConfigureGenesisTask) Validate() error { return nil }

func (t ConfigureGenesisTask) ToTaskRequest() TaskRequest {
	if t.ExpectedGenesisHash == "" {
		return TaskRequest{Type: t.TaskType()}
	}
	p := map[string]interface{}{"expectedGenesisHash": t.ExpectedGenesisHash}
	return TaskRequest{Type: t.TaskType(), Params: &p}
}

// ConfigPatchTask applies generic TOML merge-patches to seid config files.
// Files maps a filename (e.g. "config.toml", "app.toml") to a nested patch
// that will be recursively merged into the existing file.
type ConfigPatchTask struct {
	Files map[string]map[string]any
}

func (t ConfigPatchTask) TaskType() string { return TaskTypeConfigPatch }

func (t ConfigPatchTask) Validate() error {
	if len(t.Files) == 0 {
		return fmt.Errorf("config-patch: at least one file is required")
	}
	return nil
}

func (t ConfigPatchTask) ToTaskRequest() TaskRequest {
	files := make(map[string]interface{}, len(t.Files))
	for k, v := range t.Files {
		files[k] = v
	}
	p := map[string]interface{}{"files": files}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// ConfigureStateSyncTask discovers a trust point and configures state sync.
// When UseLocalSnapshot is true, the task uses the locally-restored snapshot
// height as the trust height and sets use-local-snapshot = true in config.toml.
type ConfigureStateSyncTask struct {
	UseLocalSnapshot bool
	TrustPeriod      string
	BackfillBlocks   int64
	RpcServers       []string
}

func (t ConfigureStateSyncTask) TaskType() string { return TaskTypeConfigureStateSync }
func (t ConfigureStateSyncTask) Validate() error  { return nil }

func (t ConfigureStateSyncTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{}
	if t.UseLocalSnapshot {
		p["useLocalSnapshot"] = true
	}
	if t.TrustPeriod != "" {
		p["trustPeriod"] = t.TrustPeriod
	}
	if t.BackfillBlocks > 0 {
		p["backfillBlocks"] = t.BackfillBlocks
	}
	if len(t.RpcServers) > 0 {
		p["rpcServers"] = t.RpcServers
	}
	var req TaskRequest
	if len(p) == 0 {
		req = TaskRequest{Type: t.TaskType()}
	} else {
		req = TaskRequest{Type: t.TaskType(), Params: &p}
	}
	return req
}

// MarkReadyTask signals that bootstrap is complete.
type MarkReadyTask struct{}

func (t MarkReadyTask) TaskType() string { return TaskTypeMarkReady }
func (t MarkReadyTask) Validate() error  { return nil }

func (t MarkReadyTask) ToTaskRequest() TaskRequest {
	req := TaskRequest{Type: t.TaskType()}
	return req
}

// RestartSeidTask restarts the co-located seid process in place so it
// re-reads config.toml without bouncing the sidecar. The task completes
// once seid's local RPC is serving again.
type RestartSeidTask struct{}

func (t RestartSeidTask) TaskType() string { return TaskTypeRestartSeid }
func (t RestartSeidTask) Validate() error  { return nil }

func (t RestartSeidTask) ToTaskRequest() TaskRequest {
	req := TaskRequest{Type: t.TaskType()}
	return req
}

// MarkNotReadyTask re-arms the seid start gate for a node hold: it purges
// recorded mark-ready results and flips the sidecar readiness flag false, so
// the next container restart parks seid at the gate (healthz 503).
//
// Consumers MUST poll this task to a terminal state (never fire-and-forget):
// the purge it performs is a precondition for the destructive steps that follow
// in the hold recipe, so releasing the next step before it completes is unsafe.
// This contrasts with mark-ready, which is fire-and-forget by design.
type MarkNotReadyTask struct{}

func (t MarkNotReadyTask) TaskType() string { return TaskTypeMarkNotReady }
func (t MarkNotReadyTask) Validate() error  { return nil }

func (t MarkNotReadyTask) ToTaskRequest() TaskRequest {
	return TaskRequest{Type: t.TaskType()}
}

// StopSeidTask SIGTERMs the co-located seid process and confirms it exited,
// without waiting for it to come back up. Paired with a prior mark-not-ready,
// the restarted container blocks at the gate instead of booting.
type StopSeidTask struct{}

func (t StopSeidTask) TaskType() string { return TaskTypeStopSeid }
func (t StopSeidTask) Validate() error  { return nil }

func (t StopSeidTask) ToTaskRequest() TaskRequest {
	return TaskRequest{Type: t.TaskType()}
}

// ResetDataTask clears the chain data directory (data/ only), rewrites an empty
// priv_validator_state, and removes the state-sync completion marker so the
// node re-bootstraps through state sync. Refuses to run while seid's RPC serves.
type ResetDataTask struct{}

func (t ResetDataTask) TaskType() string { return TaskTypeResetData }
func (t ResetDataTask) Validate() error  { return nil }

func (t ResetDataTask) ToTaskRequest() TaskRequest {
	return TaskRequest{Type: t.TaskType()}
}

// SetGenesisPeersTask requests the sidecar to publish this node's peer
// entry to the shared genesis peers list (S3 coordinates derived from
// the sidecar environment).
type SetGenesisPeersTask struct{}

func (t SetGenesisPeersTask) TaskType() string { return TaskTypeSetGenesisPeers }
func (t SetGenesisPeersTask) Validate() error  { return nil }

func (t SetGenesisPeersTask) ToTaskRequest() TaskRequest {
	req := TaskRequest{Type: t.TaskType()}
	return req
}

// ConfigApplyTask generates or patches node config using sei-config's
// intent resolution pipeline. The caller builds a ConfigIntent describing
// the desired state; the sidecar resolves it via sei-config.
type ConfigApplyTask struct {
	Intent seiconfig.ConfigIntent
}

func (t ConfigApplyTask) TaskType() string { return TaskTypeConfigApply }

func (t ConfigApplyTask) Validate() error {
	result := seiconfig.ValidateIntent(t.Intent)
	if !result.Valid {
		return fmt.Errorf("config-apply: invalid intent: %v", result.Diagnostics)
	}
	return nil
}

func (t ConfigApplyTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"mode":          string(t.Intent.Mode),
		"incremental":   t.Intent.Incremental,
		"targetVersion": t.Intent.TargetVersion,
	}
	if len(t.Intent.Overrides) > 0 {
		overrides := make(map[string]interface{}, len(t.Intent.Overrides))
		for k, v := range t.Intent.Overrides {
			overrides[k] = v
		}
		p["overrides"] = overrides
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// ConfigValidateTask reads on-disk config and returns validation diagnostics.
type ConfigValidateTask struct{}

func (t ConfigValidateTask) TaskType() string { return TaskTypeConfigValidate }
func (t ConfigValidateTask) Validate() error  { return nil }

func (t ConfigValidateTask) ToTaskRequest() TaskRequest {
	req := TaskRequest{Type: t.TaskType()}
	return req
}

// ConfigReloadTask patches hot-reloadable fields on disk and signals seid
// to re-read its configuration.
type ConfigReloadTask struct {
	Fields map[string]string
}

func (t ConfigReloadTask) TaskType() string { return TaskTypeConfigReload }

func (t ConfigReloadTask) Validate() error {
	if len(t.Fields) == 0 {
		return fmt.Errorf("config-reload: at least one field is required")
	}
	return nil
}

func (t ConfigReloadTask) ToTaskRequest() TaskRequest {
	fields := make(map[string]interface{}, len(t.Fields))
	for k, v := range t.Fields {
		fields[k] = v
	}
	p := map[string]interface{}{"fields": fields}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// ResultExportTask queries the local seid RPC for block results and uploads
// them in paginated NDJSON files to S3. Setting CanonicalRPC enables comparison
// mode — the sidecar compares local block results against the canonical chain —
// and the remaining fields tune that comparison. By default the task completes
// on the first divergence; ContinueOnDivergence surveys past divergences and
// runs until stopped.
type ResultExportTask struct {
	Bucket       string
	Prefix       string
	Region       string
	CanonicalRPC string

	// Comparison-mode tuning — all require CanonicalRPC. MigrationMode keys the
	// verdict on execution results for an AppHash-breaking migration shadow;
	// ContinueOnDivergence surveys past divergences instead of halting on the
	// first; ShadowEVMRPC + CanonicalEVMRPC enable Layer 2 (logical state) diff,
	// with TraceRPC sourcing each block's touched keys.
	MigrationMode        bool
	ContinueOnDivergence bool
	ShadowEVMRPC         string
	CanonicalEVMRPC      string
	TraceRPC             string
}

func (t ResultExportTask) TaskType() string { return TaskTypeResultExport }

func (t ResultExportTask) Validate() error {
	if t.Bucket == "" {
		return fmt.Errorf("result-export: missing required field Bucket")
	}
	if t.Region == "" {
		return fmt.Errorf("result-export: missing required field Region")
	}
	// The comparison-tuning fields are silently inert without CanonicalRPC (the
	// plain export path never reads them), so reject that misconfiguration here
	// rather than let it pass as a no-op.
	if t.CanonicalRPC == "" &&
		(t.MigrationMode || t.ContinueOnDivergence ||
			t.ShadowEVMRPC != "" || t.CanonicalEVMRPC != "" || t.TraceRPC != "") {
		return fmt.Errorf("result-export: comparison-mode fields require CanonicalRPC")
	}
	return nil
}

func (t ResultExportTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"bucket": t.Bucket,
		"region": t.Region,
	}
	if t.Prefix != "" {
		p["prefix"] = t.Prefix
	}
	if t.CanonicalRPC != "" {
		p["canonicalRpc"] = t.CanonicalRPC
	}
	if t.MigrationMode {
		p["migrationMode"] = true
	}
	if t.ContinueOnDivergence {
		p["continueOnDivergence"] = true
	}
	if t.ShadowEVMRPC != "" {
		p["shadowEvmRpc"] = t.ShadowEVMRPC
	}
	if t.CanonicalEVMRPC != "" {
		p["canonicalEvmRpc"] = t.CanonicalEVMRPC
	}
	if t.TraceRPC != "" {
		p["traceRpc"] = t.TraceRPC
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// GenerateIdentityTask creates validator identity (keys, node ID).
type GenerateIdentityTask struct {
	ChainID string
	Moniker string
}

func (t GenerateIdentityTask) TaskType() string { return TaskTypeGenerateIdentity }

func (t GenerateIdentityTask) Validate() error {
	if t.ChainID == "" {
		return fmt.Errorf("generate-identity: missing required field ChainID")
	}
	if t.Moniker == "" {
		return fmt.Errorf("generate-identity: missing required field Moniker")
	}
	return nil
}

func (t GenerateIdentityTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"chainId": t.ChainID,
		"moniker": t.Moniker,
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// GenerateGentxTask creates a gentx for the validator. The handler discovers
// the node's own account address from the keys generated during identity
// creation and funds it with AccountBalance before generating the gentx.
type GenerateGentxTask struct {
	ChainID        string
	StakingAmount  string
	AccountBalance string
}

func (t GenerateGentxTask) TaskType() string { return TaskTypeGenerateGentx }

func (t GenerateGentxTask) Validate() error {
	if t.ChainID == "" {
		return fmt.Errorf("generate-gentx: missing required field ChainID")
	}
	if t.StakingAmount == "" {
		return fmt.Errorf("generate-gentx: missing required field StakingAmount")
	}
	if t.AccountBalance == "" {
		return fmt.Errorf("generate-gentx: missing required field AccountBalance")
	}
	return nil
}

func (t GenerateGentxTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"chainId":        t.ChainID,
		"stakingAmount":  t.StakingAmount,
		"accountBalance": t.AccountBalance,
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// UploadGenesisArtifactsTask uploads identity.json and gentx.json to S3.
// S3 coordinates are derived by the sidecar from its environment.
type UploadGenesisArtifactsTask struct {
	NodeName string
}

func (t UploadGenesisArtifactsTask) TaskType() string { return TaskTypeUploadGenesisArtifacts }

func (t UploadGenesisArtifactsTask) Validate() error {
	if t.NodeName == "" {
		return fmt.Errorf("upload-genesis-artifacts: missing required field NodeName")
	}
	return nil
}

func (t UploadGenesisArtifactsTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"nodeName": t.NodeName,
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// GenesisNodeParam is the wire format for nodes[] in assemble-and-upload-genesis.
type GenesisNodeParam struct {
	Name string `json:"name"`
}

// GenesisAccountEntry and GenesisAccountVesting are the wire contract, aliased
// here so callers keep writing client.GenesisAccountEntry. The sidecar's
// handler side (sidecar/tasks) aliases the same definitions, so the request
// this package builds and the payload the server unmarshals cannot drift.
type (
	GenesisAccountEntry   = wire.GenesisAccountEntry
	GenesisAccountVesting = wire.GenesisAccountVesting
)

func genesisAccountsToWire(accounts []GenesisAccountEntry) []interface{} {
	if len(accounts) == 0 {
		return nil
	}
	out := make([]interface{}, len(accounts))
	for i, a := range accounts {
		entry := map[string]interface{}{"address": a.Address, "balance": a.Balance}
		if a.Vesting != nil {
			entry["vesting"] = map[string]interface{}{
				"amount":  a.Vesting.Amount,
				"endTime": a.Vesting.EndTime,
				"delayed": a.Vesting.Delayed,
			}
		}
		out[i] = entry
	}
	return out
}

// Balance shape is validated server-side via sdk.ParseCoinsNormalized.
func validateGenesisAccounts(prefix string, accounts []GenesisAccountEntry) error {
	for i, a := range accounts {
		if a.Address == "" {
			return fmt.Errorf("%s: accounts[%d] missing required field Address", prefix, i)
		}
		if a.Balance == "" {
			return fmt.Errorf("%s: accounts[%d] missing required field Balance", prefix, i)
		}
		if err := validateSeiAccountAddress(a.Address); err != nil {
			return fmt.Errorf("%s: accounts[%d]: %w", prefix, i, err)
		}
		if a.Vesting != nil {
			if a.Vesting.Amount == "" {
				return fmt.Errorf("%s: accounts[%d].vesting missing required field Amount", prefix, i)
			}
			if a.Vesting.EndTime <= 0 {
				return fmt.Errorf("%s: accounts[%d].vesting: EndTime must be a positive unix timestamp", prefix, i)
			}
		}
	}
	return nil
}

// AssembleAndUploadGenesisTask collects per-node artifacts and produces final genesis.json.
// S3 coordinates are derived by the sidecar from its environment.
//
// Overrides is a flat map of dotted-path keys into genesis.app_state to raw
// JSON values, applied to the assembled genesis after collect-gentxs runs.
// The controller validates keys (immutability post-bootstrap) via CEL; the
// sidecar applies them verbatim and fails loudly on bad paths.
//
// RESULT: this task produces the assembled genesis.json's bare SHA-256 hex
// digest (the value the controller writes to status.genesisHash and plumbs
// into followers' ConfigureGenesisTask.ExpectedGenesisHash). The digest is
// returned in-band on the task result as {"genesisHash":"<bare-hex>"}, which
// the controller reads over the trusted GET /v0/tasks/{id} channel. It is
// never written to S3, where the prefix is attacker-writable.
type AssembleAndUploadGenesisTask struct {
	AccountBalance string
	Namespace      string
	Nodes          []GenesisNodeParam
	Accounts       []GenesisAccountEntry
	Overrides      map[string]json.RawMessage
}

func (t AssembleAndUploadGenesisTask) TaskType() string { return TaskTypeAssembleGenesis }

func (t AssembleAndUploadGenesisTask) Validate() error {
	if t.AccountBalance == "" {
		return fmt.Errorf("assemble-and-upload-genesis: missing required field AccountBalance")
	}
	if t.Namespace == "" {
		return fmt.Errorf("assemble-and-upload-genesis: missing required field Namespace")
	}
	if len(t.Nodes) == 0 {
		return fmt.Errorf("assemble-and-upload-genesis: at least one node is required")
	}
	return validateGenesisAccounts("assemble-and-upload-genesis", t.Accounts)
}

func (t AssembleAndUploadGenesisTask) ToTaskRequest() TaskRequest {
	nodes := make([]interface{}, len(t.Nodes))
	for i, n := range t.Nodes {
		nodes[i] = map[string]interface{}{"name": n.Name}
	}
	p := map[string]interface{}{
		"accountBalance": t.AccountBalance,
		"namespace":      t.Namespace,
		"nodes":          nodes,
	}
	if accounts := genesisAccountsToWire(t.Accounts); accounts != nil {
		p["accounts"] = accounts
	}
	if len(t.Overrides) > 0 {
		overrides := make(map[string]interface{}, len(t.Overrides))
		for k, v := range t.Overrides {
			overrides[k] = v
		}
		p["overrides"] = overrides
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// AwaitConditionTask blocks until a condition is met, then optionally
// executes a post-condition action. Currently supports the "height"
// condition and the "SIGTERM_SEID" action.
type AwaitConditionTask struct {
	Condition    string
	TargetHeight int64
	Action       string
}

func (t AwaitConditionTask) TaskType() string { return TaskTypeAwaitCondition }

func (t AwaitConditionTask) Validate() error {
	switch t.Condition {
	case ConditionHeight:
		if t.TargetHeight <= 0 {
			return fmt.Errorf("await-condition: height condition requires TargetHeight > 0")
		}
	case ConditionCatchingUp:
		// No parameters: caught-up is derived from the local node's /status
		// (catching_up=false, height>1).
	default:
		return fmt.Errorf("await-condition: unknown condition %q", t.Condition)
	}
	if t.Action != "" && t.Action != ActionSIGTERM {
		return fmt.Errorf("await-condition: unknown action %q", t.Action)
	}
	return nil
}

func (t AwaitConditionTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"condition": t.Condition,
	}
	// targetHeight is meaningful only for the height condition; omit it
	// otherwise so a catchingUp request doesn't carry a spurious zero.
	if t.TargetHeight > 0 {
		p["targetHeight"] = t.TargetHeight
	}
	if t.Action != "" {
		p["action"] = t.Action
	}
	req := TaskRequest{Type: t.TaskType(), Params: &p}
	return req
}

// GovVoteTask submits a gov v1beta1 vote.
type GovVoteTask struct {
	ChainID    string
	KeyName    string
	ProposalID uint64
	Option     string // yes | no | abstain | no_with_veto
	Memo       string
	Fees       string
	Gas        uint64
}

func (t GovVoteTask) TaskType() string { return TaskTypeGovVote }

func (t GovVoteTask) Validate() error {
	if t.ChainID == "" {
		return errors.New("gov-vote: chainId required")
	}
	if t.KeyName == "" {
		return errors.New("gov-vote: keyName required")
	}
	if t.ProposalID == 0 {
		return errors.New("gov-vote: proposalId required (must be > 0)")
	}
	if _, err := wire.ParseVoteOption(t.Option); err != nil {
		return fmt.Errorf("gov-vote: %w", err)
	}
	if t.Fees == "" {
		return errors.New("gov-vote: fees required")
	}
	if t.Gas == 0 {
		return errors.New("gov-vote: gas required (must be > 0)")
	}
	return nil
}

func (t GovVoteTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"chainId":    t.ChainID,
		"keyName":    t.KeyName,
		"proposalId": t.ProposalID,
		"option":     t.Option,
		"fees":       t.Fees,
		"gas":        t.Gas,
	}
	if t.Memo != "" {
		p["memo"] = t.Memo
	}
	return TaskRequest{Type: t.TaskType(), Params: &p}
}

// GovSoftwareUpgradeTask submits a gov v1beta1 software-upgrade
// proposal. The chain auto-assigns proposalID; it is not returned via
// the task result (operators correlate via the memo's taskID= tag).
//
// REHYDRATION WARNING: MsgSubmitProposal is NOT chain-idempotent. See
// the handler doc in sidecar/tasks/gov_software_upgrade.go and #174.
type GovSoftwareUpgradeTask struct {
	ChainID string
	KeyName string

	Title       string
	Description string

	UpgradeName   string
	UpgradeHeight int64
	UpgradeInfo   string

	InitialDeposit string

	Memo string
	Fees string
	Gas  uint64
}

func (t GovSoftwareUpgradeTask) TaskType() string { return TaskTypeGovSoftwareUpgrade }

func (t GovSoftwareUpgradeTask) Validate() error {
	if t.ChainID == "" {
		return errors.New("gov-software-upgrade: chainId required")
	}
	if t.KeyName == "" {
		return errors.New("gov-software-upgrade: keyName required")
	}
	if t.Title == "" {
		return errors.New("gov-software-upgrade: title required")
	}
	if t.Description == "" {
		return errors.New("gov-software-upgrade: description required")
	}
	if t.UpgradeName == "" {
		return errors.New("gov-software-upgrade: upgradeName required")
	}
	if t.UpgradeHeight <= 0 {
		return errors.New("gov-software-upgrade: upgradeHeight required (must be > 0)")
	}
	if t.InitialDeposit == "" {
		return errors.New("gov-software-upgrade: initialDeposit required")
	}
	if t.Fees == "" {
		return errors.New("gov-software-upgrade: fees required")
	}
	if t.Gas == 0 {
		return errors.New("gov-software-upgrade: gas required (must be > 0)")
	}
	return nil
}

func (t GovSoftwareUpgradeTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"chainId":        t.ChainID,
		"keyName":        t.KeyName,
		"title":          t.Title,
		"description":    t.Description,
		"upgradeName":    t.UpgradeName,
		"upgradeHeight":  t.UpgradeHeight,
		"initialDeposit": t.InitialDeposit,
		"fees":           t.Fees,
		"gas":            t.Gas,
	}
	if t.UpgradeInfo != "" {
		p["upgradeInfo"] = t.UpgradeInfo
	}
	if t.Memo != "" {
		p["memo"] = t.Memo
	}
	return TaskRequest{Type: t.TaskType(), Params: &p}
}

// ParamChangeInput is one (subspace, key, value) entry of a
// ParameterChangeProposal. Value is raw JSON of whatever shape the
// param's registered type expects — a scalar (100), a string
// ("86400000000000"), a bool, or an object. It is carried as
// json.RawMessage and stringified exactly ONCE in the handler
// (gov_param_change.go); a pre-escaped string would double-encode and
// fail at apply time. Integer-valued params must be JSON strings (e.g.
// "100"), not bare numbers — the sidecar decode is float64-based and
// loses precision above 2^53 (Sei large-integer params are string-encoded
// by convention).
type ParamChangeInput struct {
	Subspace string          `json:"subspace"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
}

// GovParamChangeTask submits a gov v1beta1 ParameterChangeProposal. The
// chain auto-assigns proposalID; it is not returned via the task result
// (operators correlate via the memo's taskID= tag).
//
// REHYDRATION WARNING: MsgSubmitProposal is NOT chain-idempotent, and —
// unlike a software upgrade, which the upgrade module applies once at a
// named height — a param-change has no "applies once" safety net: a
// rehydration double-submit produces two real proposals and two
// deposits. See the handler doc in sidecar/tasks/gov_param_change.go
// and #174.
type GovParamChangeTask struct {
	ChainID string
	KeyName string

	Title       string
	Description string

	Changes []ParamChangeInput

	InitialDeposit string

	Memo string
	Fees string
	Gas  uint64
}

func (t GovParamChangeTask) TaskType() string { return TaskTypeGovParamChange }

func (t GovParamChangeTask) Validate() error {
	if t.ChainID == "" {
		return errors.New("gov-param-change: chainId required")
	}
	if t.KeyName == "" {
		return errors.New("gov-param-change: keyName required")
	}
	if t.Title == "" {
		return errors.New("gov-param-change: title required")
	}
	if t.Description == "" {
		return errors.New("gov-param-change: description required")
	}
	if len(t.Changes) == 0 {
		return errors.New("gov-param-change: at least one change required")
	}
	for i, c := range t.Changes {
		if c.Subspace == "" {
			return fmt.Errorf("gov-param-change: changes[%d].subspace required", i)
		}
		if c.Key == "" {
			return fmt.Errorf("gov-param-change: changes[%d].key required", i)
		}
		if len(c.Value) == 0 {
			return fmt.Errorf("gov-param-change: changes[%d].value required", i)
		}
	}
	if t.InitialDeposit == "" {
		return errors.New("gov-param-change: initialDeposit required")
	}
	if t.Fees == "" {
		return errors.New("gov-param-change: fees required")
	}
	if t.Gas == 0 {
		return errors.New("gov-param-change: gas required (must be > 0)")
	}
	return nil
}

func (t GovParamChangeTask) ToTaskRequest() TaskRequest {
	p := map[string]interface{}{
		"chainId":        t.ChainID,
		"keyName":        t.KeyName,
		"title":          t.Title,
		"description":    t.Description,
		"changes":        t.Changes,
		"initialDeposit": t.InitialDeposit,
		"fees":           t.Fees,
		"gas":            t.Gas,
	}
	if t.Memo != "" {
		p["memo"] = t.Memo
	}
	return TaskRequest{Type: t.TaskType(), Params: &p}
}
