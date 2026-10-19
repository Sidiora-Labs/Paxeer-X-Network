package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type mockS3GetObject struct {
	objects map[string][]byte
}

func (m *mockS3GetObject) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := *input.Key
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("NoSuchKey: %s", key)
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func TestAssembler_DownloadsGentxFiles(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, "config")
	os.MkdirAll(configDir, 0o755)

	s3Objects := &mockS3GetObject{objects: map[string][]byte{
		"genesis/val-0/gentx.json": []byte(`{"gentx":"val0"}`),
		"genesis/val-1/gentx.json": []byte(`{"gentx":"val1"}`),
	}}
	s3Factory := func(_ context.Context, _ string) (S3GetObjectAPI, error) {
		return s3Objects, nil
	}

	assembler := NewGenesisAssembler(homeDir, "my-bucket", "us-west-2", "genesis", s3Factory, mockUploaderFactory(newMockS3Uploader()))

	cfg := AssembleGenesisRequest{
		AccountBalance: "10000000usei",
		Namespace:      "default",
		Nodes:          []AssembleNodeEntry{{Name: "val-0"}, {Name: "val-1"}},
	}

	nodes := cfg.nodeNames()
	if err := assembler.downloadGentxFiles(context.Background(), cfg, nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gentxDir := filepath.Join(homeDir, "config", assembledGentxSubdir)
	for _, node := range []string{"val-0", "val-1"} {
		path := filepath.Join(gentxDir, fmt.Sprintf("gentx-%s.json", node))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected gentx file %s to exist", path)
		}
	}

	// Isolation guarantee: the assembler must never touch config/gentx/, which
	// generate-gentx and upload-genesis-artifacts use.
	if _, err := os.Stat(filepath.Join(homeDir, "config", "gentx")); !os.IsNotExist(err) {
		t.Errorf("assembler must not create or populate config/gentx/; stat err = %v (want IsNotExist)", err)
	}
}

// TestAssembler_DownloadClearsStaleFiles verifies the assemble dir is wiped on
// every run, so a leftover gentx from a crashed prior attempt can't inflate the
// collected set on a retry.
func TestAssembler_DownloadClearsStaleFiles(t *testing.T) {
	homeDir := t.TempDir()

	s3Objects := &mockS3GetObject{objects: map[string][]byte{
		"genesis/val-0/gentx.json": []byte(`{"gentx":"val0"}`),
	}}
	s3Factory := func(_ context.Context, _ string) (S3GetObjectAPI, error) {
		return s3Objects, nil
	}
	assembler := NewGenesisAssembler(homeDir, "my-bucket", "us-west-2", "genesis", s3Factory, mockUploaderFactory(newMockS3Uploader()))

	// Pre-seed a stale file in the assemble dir as if a prior run had crashed.
	gentxDir := assembler.assembledGentxDir()
	if err := os.MkdirAll(gentxDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(gentxDir, "gentx-stale.json")
	if err := os.WriteFile(stale, []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	cfg := AssembleGenesisRequest{AccountBalance: "10000000usei", Namespace: "default", Nodes: []AssembleNodeEntry{{Name: "val-0"}}}
	if err := assembler.downloadGentxFiles(context.Background(), cfg, cfg.nodeNames()); err != nil {
		t.Fatalf("downloadGentxFiles: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale gentx file survived download; stat err = %v (want IsNotExist)", err)
	}
	if _, err := os.Stat(filepath.Join(gentxDir, "gentx-val-0.json")); err != nil {
		t.Errorf("expected downloaded gentx-val-0.json to exist: %v", err)
	}
}

func TestAssembler_MissingParams(t *testing.T) {
	handler := NewGenesisAssembler(t.TempDir(), "b", "r", "chain", nil, nil).Handler()

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"missing accountBalance", map[string]any{"namespace": "ns", "nodes": []any{map[string]any{"name": "n"}}}},
		{"missing namespace", map[string]any{"accountBalance": "10usei", "nodes": []any{map[string]any{"name": "n"}}}},
		{"missing nodes", map[string]any{"accountBalance": "10usei", "namespace": "ns"}},
		{"empty nodes", map[string]any{"accountBalance": "10usei", "namespace": "ns", "nodes": []any{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handler(context.Background(), tt.params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAssembler_S3DownloadFailure(t *testing.T) {
	homeDir := t.TempDir()
	s3Factory := func(_ context.Context, _ string) (S3GetObjectAPI, error) {
		return &mockS3GetObject{objects: map[string][]byte{}}, nil
	}

	handler := NewGenesisAssembler(homeDir, "b", "r", "c", s3Factory, nil).Handler()
	_, err := handler(context.Background(), map[string]any{
		"accountBalance": "10000000usei", "namespace": "default",
		"nodes": []any{map[string]any{"name": "missing-node"}},
	})
	if err == nil {
		t.Fatal("expected error when S3 download fails")
	}
}

func TestParseAssembleNodes(t *testing.T) {
	// Test that AssembleNodeEntry JSON round-trips correctly.
	raw := `[{"name":"val-0"},{"name":"val-1"}]`
	var nodes []AssembleNodeEntry
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 || nodes[0].Name != "val-0" || nodes[1].Name != "val-1" {
		t.Errorf("nodes = %v, want [{val-0} {val-1}]", nodes)
	}
}

func TestParseAssembleNodes_MissingName(t *testing.T) {
	// Test that empty names are caught by the handler validation.
	raw := `[{"other":"field"}]`
	var nodes []AssembleNodeEntry
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The node should unmarshal but with empty Name — the handler validates this.
	if nodes[0].Name != "" {
		t.Fatalf("expected empty name, got %q", nodes[0].Name)
	}
}

func TestAssembler_UploadGenesis_ReturnsHashOfUploadedBytes(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(`{"chain_id":"test-chain-1","app_state":{"staking":{"params":{}}}}`)
	if err := os.WriteFile(filepath.Join(configDir, "genesis.json"), body, 0o644); err != nil {
		t.Fatalf("write genesis: %v", err)
	}

	mock := newMockS3Uploader()
	a := NewGenesisAssembler(homeDir, "my-bucket", "us-west-2", "test-chain-1", nil, mockUploaderFactory(mock))

	gotHash, err := a.uploadGenesis(context.Background(), AssembleGenesisRequest{})
	if err != nil {
		t.Fatalf("uploadGenesis: %v", err)
	}

	// The returned hash is the SHA-256 of the exact uploaded bytes.
	uploaded, ok := mock.uploads["my-bucket/test-chain-1/genesis.json"]
	if !ok {
		t.Fatal("genesis.json was not uploaded")
	}
	sum := sha256.Sum256(uploaded)
	wantHash := hex.EncodeToString(sum[:])
	if gotHash != wantHash {
		t.Errorf("returned hash = %q, want sha256(uploaded bytes) = %q", gotHash, wantHash)
	}
	// Byte-exactness: the uploaded bytes equal the on-disk genesis (no re-marshal).
	if !bytes.Equal(uploaded, body) {
		t.Errorf("uploaded bytes differ from on-disk genesis; got %q", uploaded)
	}
	// No "sha256:" prefix — bare hex.
	if strings.Contains(gotHash, ":") {
		t.Errorf("hash %q must be bare hex (no algorithm prefix)", gotHash)
	}
	// The hash travels only in-band: no sibling .sha256 object is written
	// to the attacker-writable prefix.
	if _, ok := mock.uploads["my-bucket/test-chain-1/genesis.json.sha256"]; ok {
		t.Error("sibling genesis.json.sha256 object must not be uploaded; the hash travels in-band only")
	}

	// The in-band result the handler emits is {"genesisHash":"<bare-hex>"}
	// carrying exactly sha256(uploaded bytes).
	resultJSON, err := json.Marshal(AssembleGenesisResult{GenesisHash: gotHash})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded AssembleGenesisResult
	if err := json.Unmarshal(resultJSON, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if decoded.GenesisHash != wantHash {
		t.Errorf("in-band result genesisHash = %q, want sha256(uploaded) = %q", decoded.GenesisHash, wantHash)
	}
}

// genesisWithAppState writes a Tendermint genesis.json containing the given
// JSON-encoded app_state body. Used by override tests that need real
// module-shaped data to walk into.
func genesisWithAppState(t *testing.T, homeDir, appStateJSON string) string {
	t.Helper()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	genFile := filepath.Join(configDir, "genesis.json")
	body := `{
		"chain_id": "test-chain-1",
		"genesis_time": "2026-01-01T00:00:00Z",
		"initial_height": "1",
		"consensus_params": {
			"block": {"max_bytes": "22020096", "max_gas": "-1"},
			"evidence": {"max_age_num_blocks": "100000", "max_age_duration": "172800000000000", "max_bytes": "1048576"},
			"validator": {"pub_key_types": ["ed25519"]},
			"version": {}
		},
		"validators": [],
		"app_hash": "",
		"app_state": ` + appStateJSON + `
	}`
	if err := os.WriteFile(genFile, []byte(body), 0o644); err != nil {
		t.Fatalf("write genesis: %v", err)
	}
	return genFile
}

func TestAssembler_ApplyOverrides_NoOpOnEmpty(t *testing.T) {
	homeDir := t.TempDir()
	genFile := genesisWithAppState(t, homeDir, `{"staking":{"params":{"unbonding_time":"1814400s"}}}`)
	mtimeBefore := mustModTime(t, genFile)

	a := NewGenesisAssembler(homeDir, "b", "r", "test-chain-1", nil, nil)
	if err := a.applyOverrides(nil); err != nil {
		t.Fatalf("nil overrides: %v", err)
	}
	if err := a.applyOverrides(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("empty overrides: %v", err)
	}

	if mustModTime(t, genFile) != mtimeBefore {
		t.Errorf("genesis.json was rewritten on no-op overrides")
	}
}

func TestAssembler_ApplyOverrides_PatchesGenesisFile(t *testing.T) {
	homeDir := t.TempDir()
	genFile := genesisWithAppState(t, homeDir, `{
		"staking": {"params": {"unbonding_time": "1814400s", "max_validators": 100}},
		"gov":     {"params": {"max_deposit_period": "172800s"}}
	}`)

	a := NewGenesisAssembler(homeDir, "b", "r", "test-chain-1", nil, nil)
	err := a.applyOverrides(map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
		"gov.params.max_deposit_period": json.RawMessage(`"60s"`),
	})
	if err != nil {
		t.Fatalf("applyOverrides: %v", err)
	}

	// Read the file back and confirm the leaves changed.
	body, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("reading genesis back: %v", err)
	}
	var doc struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parsing genesis: %v", err)
	}
	var staking struct {
		Params struct {
			UnbondingTime string `json:"unbonding_time"`
			MaxValidators int    `json:"max_validators"`
		} `json:"params"`
	}
	if err := json.Unmarshal(doc.AppState["staking"], &staking); err != nil {
		t.Fatalf("parsing staking: %v", err)
	}
	if staking.Params.UnbondingTime != "600s" {
		t.Errorf("unbonding_time = %q, want 600s", staking.Params.UnbondingTime)
	}
	if staking.Params.MaxValidators != 100 {
		t.Errorf("max_validators = %d, want preserved 100", staking.Params.MaxValidators)
	}

	var gov struct {
		Params struct {
			MaxDepositPeriod string `json:"max_deposit_period"`
		} `json:"params"`
	}
	if err := json.Unmarshal(doc.AppState["gov"], &gov); err != nil {
		t.Fatalf("parsing gov: %v", err)
	}
	if gov.Params.MaxDepositPeriod != "60s" {
		t.Errorf("max_deposit_period = %q, want 60s", gov.Params.MaxDepositPeriod)
	}
}

func TestAssembler_ApplyOverrides_BubblesBadKeyError(t *testing.T) {
	homeDir := t.TempDir()
	_ = genesisWithAppState(t, homeDir, `{"staking":{"params":{"unbonding_time":"1814400s"}}}`)

	a := NewGenesisAssembler(homeDir, "b", "r", "test-chain-1", nil, nil)
	err := a.applyOverrides(map[string]json.RawMessage{
		"nope.params.x": json.RawMessage(`"y"`),
	})
	if err == nil {
		t.Fatal("expected error for unknown module override")
	}
	if !strings.Contains(err.Error(), "unknown module") {
		t.Errorf("error = %q, want substring 'unknown module'", err.Error())
	}
}

// TestAssembleGenesisRequest_OverridesRoundTrip verifies the new Overrides
// field deserializes from the wire shape the controller emits.
func TestAssembleGenesisRequest_OverridesRoundTrip(t *testing.T) {
	wire := `{
		"accountBalance": "10000000usei",
		"namespace": "default",
		"nodes": [{"name": "val-0"}],
		"overrides": {
			"staking.params.unbonding_time": "600s",
			"staking.params.max_validators": 50
		}
	}`
	var got AssembleGenesisRequest
	if err := json.Unmarshal([]byte(wire), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Overrides) != 2 {
		t.Fatalf("overrides len = %d, want 2", len(got.Overrides))
	}
	if string(got.Overrides["staking.params.unbonding_time"]) != `"600s"` {
		t.Errorf("unbonding_time raw = %q, want %q",
			string(got.Overrides["staking.params.unbonding_time"]), `"600s"`)
	}
	if string(got.Overrides["staking.params.max_validators"]) != "50" {
		t.Errorf("max_validators raw = %q, want %q",
			string(got.Overrides["staking.params.max_validators"]), "50")
	}
}
