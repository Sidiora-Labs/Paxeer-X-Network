package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"

	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seictl/sidecar/wire"
)

// TaskType identifies the kind of task to execute.
// TaskType aliases wire.TaskType so engine call sites and the dispatch-map key
// type are unchanged; the consts below re-export wire.Task* likewise.
type TaskType = wire.TaskType

const (
	TaskSnapshotRestore          = wire.TaskSnapshotRestore
	TaskConfigPatch              = wire.TaskConfigPatch
	TaskConfigApply              = wire.TaskConfigApply
	TaskConfigValidate           = wire.TaskConfigValidate
	TaskConfigReload             = wire.TaskConfigReload
	TaskMarkReady                = wire.TaskMarkReady
	TaskRestartSeid              = wire.TaskRestartSeid
	TaskConfigureGenesis         = wire.TaskConfigureGenesis
	TaskConfigureStateSync       = wire.TaskConfigureStateSync
	TaskSnapshotUpload           = wire.TaskSnapshotUpload
	TaskSnapshotUploadOnce       = wire.TaskSnapshotUploadOnce
	TaskResultExport             = wire.TaskResultExport
	TaskAwaitCondition           = wire.TaskAwaitCondition
	TaskGenerateIdentity         = wire.TaskGenerateIdentity
	TaskGenerateGentx            = wire.TaskGenerateGentx
	TaskUploadGenesisArtifacts   = wire.TaskUploadGenesisArtifacts
	TaskAssembleAndUploadGenesis = wire.TaskAssembleAndUploadGenesis
	TaskSetGenesisPeers          = wire.TaskSetGenesisPeers
	TaskGovVote                  = wire.TaskGovVote
	TaskGovSoftwareUpgrade       = wire.TaskGovSoftwareUpgrade
	TaskGovParamChange           = wire.TaskGovParamChange
	TaskEvmLogicalDigest         = wire.TaskEvmLogicalDigest
	TaskMarkNotReady             = wire.TaskMarkNotReady
	TaskStopSeid                 = wire.TaskStopSeid
	TaskResetData                = wire.TaskResetData
)

// Task is a unit of work submitted by the controller. When ID is set, the
// engine uses it as the canonical task identifier (enabling deterministic
// IDs from the controller). When empty, the engine generates a random UUID.
type Task struct {
	ID     string         `json:"id,omitempty"`
	Type   TaskType       `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// TaskHandler executes a specific task type. Handlers MUST be idempotent:
// the engine may re-execute a handler after a crash recovery. The returned
// json.RawMessage is the handler's optional structured result, persisted on
// TaskResult.Result and surfaced over GET /v0/tasks/{id}; handlers with no
// result return nil. The engine stamps the result on both the success and
// error paths (a handler returning an error may still carry a result, e.g. a
// tx hash for an inclusion-undetermined gov submit).
type TaskHandler func(ctx context.Context, params map[string]any) (json.RawMessage, error)

type taskIDKey struct{}

// TaskIDFromContext returns the engine-assigned task ID for the current
// handler, or "" when ctx is not engine-produced. Sign-tx handlers use
// this to derive the per-task memo tag.
func TaskIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(taskIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithTaskID attaches a task ID to ctx for handler consumption. The engine
// calls this in newTaskContext; tests use it to bypass Submit.
func WithTaskID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskIDKey{}, id)
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// TaskError is a structured error that includes operator-actionable context.
// Task handlers return this to provide rich error detail beyond a plain string.
type TaskError struct {
	Task      string `json:"task"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
	Cause     string `json:"cause,omitempty"`
}

func (e *TaskError) Error() string {
	s := fmt.Sprintf("%s: %s: %s", e.Task, e.Operation, e.Message)
	if e.Hint != "" {
		s += fmt.Sprintf(" [hint: %s]", e.Hint)
	}
	return s
}

// TaskResult records a task and its outcome.
//
// Result carries a handler's structured output (e.g. assemble-genesis emits
// {"genesisHash":"<bare-hex>"}). It is optional and additive: handlers that
// emit nothing leave it nil and it is omitted from the wire, so the
// currently-deployed controller is unaffected. This in-band channel — read
// by the controller over the trusted GET /v0/tasks/{id} path — is the
// authenticated alternative to publishing results through attacker-writable
// shared storage.
type TaskResult struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Status      TaskStatus      `json:"status"`
	Run         int             `json:"run"`
	Params      map[string]any  `json:"params,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	SubmittedAt time.Time       `json:"submittedAt"`
	CompletedAt *time.Time      `json:"completedAt,omitempty"`
}

// StatusResponse is the shape returned by the status endpoint.
type StatusResponse struct {
	Status string `json:"status"`
}

// ExecutionConfig carries process-wide deps the engine exposes to handlers.
// Fields are nil when the corresponding subsystem is not configured.
type ExecutionConfig struct {
	// Keyring is opened from SEI_KEYRING_BACKEND. Nil when unset;
	// sign-tx handlers report a clear error rather than panic.
	Keyring keyring.Keyring

	// RPC talks to the co-located seid CometBFT RPC. Sign-tx handlers
	// use it for the chain-confusion guard and inclusion polling.
	RPC *rpc.Client

	// Checkpointer persists a pre-broadcast TxMarker so a crashed sign-tx
	// task re-adopts its in-flight tx on re-run rather than re-signing.
	// Nil when no durable store is configured.
	Checkpointer Checkpointer
}
