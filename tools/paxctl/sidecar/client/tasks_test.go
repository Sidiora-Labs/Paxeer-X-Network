package client

import (
	"encoding/json"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func genNonEmptyString() gopter.Gen {
	return gen.AlphaString().SuchThat(func(v string) bool { return len(v) > 0 })
}

func genSnapshotRestoreTask() gopter.Gen {
	return gen.Int64Range(0, 300000000).Map(func(h int64) SnapshotRestoreTask {
		return SnapshotRestoreTask{TargetHeight: h}
	})
}

func genSnapshotUploadTask() gopter.Gen {
	return gen.Const(SnapshotUploadTask{})
}

func genConfigureGenesisTask() gopter.Gen {
	return gen.Const(ConfigureGenesisTask{})
}

func genConfigPatchTask() gopter.Gen {
	return gopter.CombineGens(
		genNonEmptyString(),
		genNonEmptyString(),
	).Map(func(v []interface{}) ConfigPatchTask {
		return ConfigPatchTask{
			Files: map[string]map[string]any{
				"config.toml": {
					"p2p": map[string]any{"persistent-peers": v[0].(string)},
				},
				"app.toml": {
					"pruning": v[1].(string),
				},
			},
		}
	})
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("SnapshotRestoreTask round-trips through TaskRequest", prop.ForAll(
		func(task SnapshotRestoreTask) bool {
			if err := task.Validate(); err != nil {
				return false
			}
			req := task.ToTaskRequest()
			if req.Type != TaskTypeSnapshotRestore {
				return false
			}
			if task.TargetHeight == 0 {
				return req.Params == nil
			}
			rebuilt := snapshotRestoreTaskFromParams(*req.Params)
			return rebuilt.TargetHeight == task.TargetHeight
		},
		genSnapshotRestoreTask(),
	))
	properties.TestingRun(t)
}

func TestSnapshotUploadRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("SnapshotUploadTask round-trips through TaskRequest", prop.ForAll(
		func(task SnapshotUploadTask) bool {
			if err := task.Validate(); err != nil {
				return false
			}
			req := task.ToTaskRequest()
			return req.Type == TaskTypeSnapshotUpload && req.Params == nil
		},
		genSnapshotUploadTask(),
	))
	properties.TestingRun(t)
}

func TestSnapshotUploadOnceRoundTrip(t *testing.T) {
	task := SnapshotUploadOnceTask{}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeSnapshotUploadOnce {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeSnapshotUploadOnce)
	}
	if req.Params != nil {
		t.Errorf("Params = %v, want nil (empty params)", req.Params)
	}
}

func TestConfigureGenesisRoundTrip_S3(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("ConfigureGenesisTask round-trips through TaskRequest", prop.ForAll(
		func(task ConfigureGenesisTask) bool {
			if err := task.Validate(); err != nil {
				return false
			}
			req := task.ToTaskRequest()
			return req.Type == TaskTypeConfigureGenesis && req.Params == nil
		},
		genConfigureGenesisTask(),
	))
	properties.TestingRun(t)
}

func TestConfigPatchRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("ConfigPatchTask round-trips through TaskRequest", prop.ForAll(
		func(task ConfigPatchTask) bool {
			if err := task.Validate(); err != nil {
				return false
			}
			req := task.ToTaskRequest()
			if req.Type != TaskTypeConfigPatch {
				return false
			}
			if req.Params == nil {
				return false
			}
			files, ok := (*req.Params)["files"]
			if !ok {
				return false
			}
			filesMap, ok := files.(map[string]interface{})
			if !ok {
				return false
			}
			return len(filesMap) == len(task.Files)
		},
		genConfigPatchTask(),
	))
	properties.TestingRun(t)
}

func TestConfigureStateSyncRoundTrip(t *testing.T) {
	task := ConfigureStateSyncTask{}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeConfigureStateSync {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeConfigureStateSync)
	}
	if req.Params != nil {
		t.Errorf("Params = %v, want nil", req.Params)
	}
}

// Wire contract the controller depends on: RpcServers must surface verbatim
// under the "rpcServers" params key the sidecar handler reads.
func TestConfigureStateSyncRpcServersWire(t *testing.T) {
	witnesses := []string{
		"syncer-0-0-0.syncer-0-0.arctic-1.svc.cluster.local:26657",
		"syncer-0-1-0.syncer-0-1.arctic-1.svc.cluster.local:26657",
	}
	req := ConfigureStateSyncTask{RpcServers: witnesses}.ToTaskRequest()
	if req.Params == nil {
		t.Fatal("Params = nil, want rpcServers populated")
	}
	got, ok := (*req.Params)["rpcServers"].([]string)
	if !ok {
		t.Fatalf("params[rpcServers] = %T, want []string", (*req.Params)["rpcServers"])
	}
	if len(got) != len(witnesses) {
		t.Fatalf("rpcServers = %v, want %v", got, witnesses)
	}
	for i := range witnesses {
		if got[i] != witnesses[i] {
			t.Errorf("rpcServers[%d] = %q, want %q", i, got[i], witnesses[i])
		}
	}
}

func TestMarkReadyRoundTrip(t *testing.T) {
	task := MarkReadyTask{}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeMarkReady {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeMarkReady)
	}
	if req.Params != nil {
		t.Errorf("Params = %v, want nil", req.Params)
	}
}

func TestWorkflowHoldTasksRoundTrip(t *testing.T) {
	cases := []struct {
		task     TaskBuilder
		wantType string
	}{
		{MarkNotReadyTask{}, TaskTypeMarkNotReady},
		{StopSeidTask{}, TaskTypeStopSeid},
		{ResetDataTask{}, TaskTypeResetData},
	}
	for _, tc := range cases {
		t.Run(tc.wantType, func(t *testing.T) {
			if err := tc.task.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			req := tc.task.ToTaskRequest()
			if req.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", req.Type, tc.wantType)
			}
			if req.Params != nil {
				t.Errorf("Params = %v, want nil (empty-payload task)", req.Params)
			}
		})
	}
}

func TestAwaitCatchingUpRoundTrip(t *testing.T) {
	task := AwaitConditionTask{Condition: ConditionCatchingUp}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeAwaitCondition {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeAwaitCondition)
	}
	if req.Params == nil {
		t.Fatal("expected non-nil Params")
	}
	p := *req.Params
	if p["condition"] != ConditionCatchingUp {
		t.Errorf("condition = %v, want %q", p["condition"], ConditionCatchingUp)
	}
	// targetHeight is meaningless for catchingUp and must be omitted.
	if _, ok := p["targetHeight"]; ok {
		t.Errorf("targetHeight should be omitted for catchingUp, got %v", p["targetHeight"])
	}
}

func TestSetGenesisPeersRoundTrip(t *testing.T) {
	task := SetGenesisPeersTask{}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeSetGenesisPeers {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeSetGenesisPeers)
	}
	if req.Params != nil {
		t.Errorf("Params = %v, want nil", req.Params)
	}
}

func TestSnapshotRestoreValidation(t *testing.T) {
	// SnapshotRestoreTask has no required fields — TargetHeight=0 means "use latest"
	cases := []struct {
		name string
		task SnapshotRestoreTask
	}{
		{"zero height (latest)", SnapshotRestoreTask{}},
		{"with target height", SnapshotRestoreTask{TargetHeight: 100000000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.task.Validate(); err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestSnapshotUploadValidation(t *testing.T) {
	task := SnapshotUploadTask{}
	if err := task.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigureGenesisValidation(t *testing.T) {
	task := ConfigureGenesisTask{}
	if err := task.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestConfigPatchToTaskRequest_NestedValuesPreserved(t *testing.T) {
	task := ConfigPatchTask{
		Files: map[string]map[string]any{
			"config.toml": {
				"statesync": map[string]any{
					"use-local-snapshot": true,
					"backfill-blocks":    int64(0),
				},
				"p2p": map[string]any{
					"persistent-peers": "abc@1.2.3.4:26656",
				},
			},
			"app.toml": {
				"pruning":           "nothing",
				"snapshot-interval": int64(2000),
			},
		},
	}

	req := task.ToTaskRequest()

	// Simulate the JSON round-trip that happens on the wire.
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded TaskRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	files, ok := (*decoded.Params)["files"].(map[string]any)
	if !ok {
		t.Fatal("expected files to be a map after JSON round-trip")
	}

	configToml, ok := files["config.toml"].(map[string]any)
	if !ok {
		t.Fatal("expected config.toml entry to be a map")
	}
	statesync, ok := configToml["statesync"].(map[string]any)
	if !ok {
		t.Fatal("expected statesync section to be a map")
	}
	if statesync["use-local-snapshot"] != true {
		t.Errorf("use-local-snapshot = %v, want true", statesync["use-local-snapshot"])
	}

	appToml, ok := files["app.toml"].(map[string]any)
	if !ok {
		t.Fatal("expected app.toml entry to be a map")
	}
	if appToml["pruning"] != "nothing" {
		t.Errorf("pruning = %v, want nothing", appToml["pruning"])
	}
	// JSON unmarshals numbers as float64.
	if appToml["snapshot-interval"] != float64(2000) {
		t.Errorf("snapshot-interval = %v, want 2000", appToml["snapshot-interval"])
	}
}

func TestConfigPatchValidationRejectsEmpty(t *testing.T) {
	task := ConfigPatchTask{}
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty ConfigPatchTask")
	}
}

func TestStatusResponseJSONRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("StatusResponse JSON round-trips", prop.ForAll(
		func(status StatusResponseStatus) bool {
			sr := StatusResponse{Status: status}
			data, err := json.Marshal(sr)
			if err != nil {
				return false
			}
			var decoded StatusResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			return decoded.Status == sr.Status
		},
		gen.OneConstOf(Initializing, Ready),
	))
	properties.TestingRun(t)
}

func TestTaskRequestJSONRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("TaskRequest JSON round-trips preserve type", prop.ForAll(
		func(taskType string) bool {
			req := TaskRequest{Type: taskType}
			data, err := json.Marshal(req)
			if err != nil {
				return false
			}
			var decoded TaskRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}
			return decoded.Type == taskType
		},
		gen.OneConstOf(
			TaskTypeSnapshotRestore,
			TaskTypeConfigPatch,
			TaskTypeMarkReady,
			TaskTypeConfigureGenesis,
			TaskTypeConfigureStateSync,
			TaskTypeSnapshotUpload,
		),
	))
	properties.TestingRun(t)
}

func TestResultExportRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())
	properties.Property("ResultExportTask round-trips through TaskRequest", prop.ForAll(
		func(bucket, region string) bool {
			task := ResultExportTask{Bucket: bucket, Region: region}
			if err := task.Validate(); err != nil {
				return false
			}
			req := task.ToTaskRequest()
			if req.Type != TaskTypeResultExport {
				return false
			}
			rebuilt := resultExportTaskFromParams(*req.Params)
			return rebuilt.Bucket == task.Bucket &&
				rebuilt.Region == task.Region
		},
		genNonEmptyString(),
		genNonEmptyString(),
	))
	properties.TestingRun(t)
}

func TestResultExportValidation(t *testing.T) {
	cases := []struct {
		name string
		task ResultExportTask
		ok   bool
	}{
		{"valid", ResultExportTask{Bucket: "b", Region: "r"}, true},
		{"missing bucket", ResultExportTask{Region: "r"}, false},
		{"missing region", ResultExportTask{Bucket: "b"}, false},
		{"all empty", ResultExportTask{}, false},
		{"comparison fields with canonicalRpc", ResultExportTask{Bucket: "b", Region: "r", CanonicalRPC: "http://c:26657", MigrationMode: true, ContinueOnDivergence: true}, true},
		{"continueOnDivergence without canonicalRpc", ResultExportTask{Bucket: "b", Region: "r", ContinueOnDivergence: true}, false},
		{"migrationMode without canonicalRpc", ResultExportTask{Bucket: "b", Region: "r", MigrationMode: true}, false},
		{"evmRpc without canonicalRpc", ResultExportTask{Bucket: "b", Region: "r", ShadowEVMRPC: "http://s:8545"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.task.Validate()
			if tc.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestResultExportTask_WithCanonicalRPC(t *testing.T) {
	task := ResultExportTask{
		Bucket:       "b",
		Prefix:       "shadow-results/pacific-1/",
		Region:       "eu-central-1",
		CanonicalRPC: "http://canonical-rpc:26657",
	}
	req := task.ToTaskRequest()
	p := *req.Params
	if p["canonicalRpc"] != "http://canonical-rpc:26657" {
		t.Errorf("canonicalRpc = %v, want %q", p["canonicalRpc"], "http://canonical-rpc:26657")
	}

	rebuilt := resultExportTaskFromParams(p)
	if rebuilt.CanonicalRPC != task.CanonicalRPC {
		t.Errorf("round-trip CanonicalRPC = %q, want %q", rebuilt.CanonicalRPC, task.CanonicalRPC)
	}
	if rebuilt.Bucket != task.Bucket {
		t.Errorf("round-trip Bucket = %q, want %q", rebuilt.Bucket, task.Bucket)
	}
}

func TestResultExportTask_WithoutCanonicalRPC_OmitsParam(t *testing.T) {
	task := ResultExportTask{Bucket: "b", Region: "r"}
	req := task.ToTaskRequest()
	p := *req.Params
	if _, ok := p["canonicalRpc"]; ok {
		t.Errorf("expected canonicalRpc to be absent, got %v", p["canonicalRpc"])
	}
	// Comparison-mode keys are omitted entirely at their zero value.
	for _, k := range []string{"migrationMode", "continueOnDivergence", "shadowEvmRpc", "canonicalEvmRpc", "traceRpc"} {
		if _, ok := p[k]; ok {
			t.Errorf("expected %q to be absent at zero value, got %v", k, p[k])
		}
	}
}

func TestResultExportTask_ComparisonTuningParams(t *testing.T) {
	task := ResultExportTask{
		Bucket:               "b",
		Region:               "eu-central-1",
		CanonicalRPC:         "http://canonical-rpc:26657",
		MigrationMode:        true,
		ContinueOnDivergence: true,
		ShadowEVMRPC:         "http://shadow:8545",
		CanonicalEVMRPC:      "http://canonical:8545",
		TraceRPC:             "http://trace:8545",
	}
	p := *task.ToTaskRequest().Params

	if p["migrationMode"] != true {
		t.Errorf("migrationMode = %v, want true", p["migrationMode"])
	}
	if p["continueOnDivergence"] != true {
		t.Errorf("continueOnDivergence = %v, want true", p["continueOnDivergence"])
	}
	if p["shadowEvmRpc"] != task.ShadowEVMRPC {
		t.Errorf("shadowEvmRpc = %v, want %q", p["shadowEvmRpc"], task.ShadowEVMRPC)
	}
	if p["canonicalEvmRpc"] != task.CanonicalEVMRPC {
		t.Errorf("canonicalEvmRpc = %v, want %q", p["canonicalEvmRpc"], task.CanonicalEVMRPC)
	}
	if p["traceRpc"] != task.TraceRPC {
		t.Errorf("traceRpc = %v, want %q", p["traceRpc"], task.TraceRPC)
	}
}

func TestAwaitConditionValidation(t *testing.T) {
	cases := []struct {
		name string
		task AwaitConditionTask
		ok   bool
	}{
		{"valid height", AwaitConditionTask{Condition: ConditionHeight, TargetHeight: 1000}, true},
		{"valid height with action", AwaitConditionTask{Condition: ConditionHeight, TargetHeight: 1000, Action: ActionSIGTERM}, true},
		{"zero height", AwaitConditionTask{Condition: ConditionHeight, TargetHeight: 0}, false},
		{"negative height", AwaitConditionTask{Condition: ConditionHeight, TargetHeight: -1}, false},
		{"unknown condition", AwaitConditionTask{Condition: "foo", TargetHeight: 1000}, false},
		{"empty condition", AwaitConditionTask{TargetHeight: 1000}, false},
		{"unknown action", AwaitConditionTask{Condition: ConditionHeight, TargetHeight: 1000, Action: "BAD"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.task.Validate()
			if tc.ok && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestAwaitConditionToTaskRequest(t *testing.T) {
	task := AwaitConditionTask{
		Condition:    ConditionHeight,
		TargetHeight: 5000,
		Action:       ActionSIGTERM,
	}
	req := task.ToTaskRequest()
	if req.Type != TaskTypeAwaitCondition {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeAwaitCondition)
	}
	if req.Params == nil {
		t.Fatal("expected non-nil Params")
	}
	p := *req.Params
	if p["condition"] != ConditionHeight {
		t.Errorf("condition = %v, want %q", p["condition"], ConditionHeight)
	}
	if p["targetHeight"] != int64(5000) {
		t.Errorf("targetHeight = %v, want 5000", p["targetHeight"])
	}
	if p["action"] != ActionSIGTERM {
		t.Errorf("action = %v, want %q", p["action"], ActionSIGTERM)
	}
}

func TestAwaitConditionToTaskRequest_NoAction(t *testing.T) {
	task := AwaitConditionTask{
		Condition:    ConditionHeight,
		TargetHeight: 100,
	}
	req := task.ToTaskRequest()
	p := *req.Params
	if _, ok := p["action"]; ok {
		t.Errorf("expected action to be absent, got %v", p["action"])
	}
}

func TestAwaitConditionJSONRoundTrip(t *testing.T) {
	task := AwaitConditionTask{
		Condition:    ConditionHeight,
		TargetHeight: 8000,
		Action:       ActionSIGTERM,
	}
	req := task.ToTaskRequest()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded TaskRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != TaskTypeAwaitCondition {
		t.Errorf("decoded Type = %q, want %q", decoded.Type, TaskTypeAwaitCondition)
	}
	p := *decoded.Params
	if p["condition"] != ConditionHeight {
		t.Errorf("condition = %v, want %q", p["condition"], ConditionHeight)
	}
	// JSON numbers decode as float64.
	if p["targetHeight"] != float64(8000) {
		t.Errorf("targetHeight = %v (type %T), want 8000", p["targetHeight"], p["targetHeight"])
	}
}

// snapshotRestoreTaskFromParams reconstructs a SnapshotRestoreTask from
// a generic params map. Useful for round-trip testing.
func snapshotRestoreTaskFromParams(params map[string]interface{}) SnapshotRestoreTask {
	var t SnapshotRestoreTask
	switch h := params["targetHeight"].(type) {
	case float64:
		t.TargetHeight = int64(h)
	case int64:
		t.TargetHeight = h
	}
	return t
}

// resultExportTaskFromParams reconstructs a ResultExportTask from
// a generic params map.
func resultExportTaskFromParams(params map[string]interface{}) ResultExportTask {
	s := func(k string) string { v, _ := params[k].(string); return v }
	return ResultExportTask{
		Bucket:       s("bucket"),
		Prefix:       s("prefix"),
		Region:       s("region"),
		CanonicalRPC: s("canonicalRpc"),
	}
}
