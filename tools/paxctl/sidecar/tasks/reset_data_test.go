package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newResetDataer builds a ResetDataer over homeDir whose RPC probe reports the
// given serving state (true = seid up, which the reset must refuse).
func newResetDataer(homeDir string, rpcUp bool) *ResetDataer {
	return &ResetDataer{
		homeDir: homeDir,
		probeUp: func(context.Context) bool { return rpcUp },
	}
}

// seedHome lays out a realistic home root: data/ with chain files, config/ with
// identity, the sidecar task ledger, and the state-sync marker.
func seedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	mustWrite(t, filepath.Join(home, "data", "blockstore.db", "000001.log"), "blocks")
	mustWrite(t, filepath.Join(home, "data", "application.db", "CURRENT"), "app")
	mustWrite(t, filepath.Join(home, "data", privValidatorStateFile), `{"height":"987","round":0,"step":3}`)
	mustWrite(t, filepath.Join(home, "config", "config.toml"), "cfg")
	mustWrite(t, filepath.Join(home, "config", "node_key.json"), "nodekey")
	mustWrite(t, filepath.Join(home, "config", "priv_validator_key.json"), "conskey")
	mustWrite(t, filepath.Join(home, "sidecar.db"), "ledger")
	mustWrite(t, filepath.Join(home, stateSyncMarkerFile), "")
	return home
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runReset(t *testing.T, d *ResetDataer) ResetDataResult {
	t.Helper()
	raw, err := d.Handler()(context.Background(), nil)
	if err != nil {
		t.Fatalf("reset-data: %v", err)
	}
	var res ResetDataResult
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("decoding result: %v", err)
		}
	}
	return res
}

func TestResetData_WipesDataPreservesHomeRoot(t *testing.T) {
	home := seedHome(t)
	res := runReset(t, newResetDataer(home, false))

	// data/ chain files gone.
	if exists(filepath.Join(home, "data", "blockstore.db")) {
		t.Error("blockstore.db survived the wipe")
	}
	if exists(filepath.Join(home, "data", "application.db")) {
		t.Error("application.db survived the wipe")
	}
	// Home-root siblings untouched — the design's core correctness rule.
	for _, p := range []string{
		filepath.Join(home, "config", "config.toml"),
		filepath.Join(home, "config", "node_key.json"),
		filepath.Join(home, "config", "priv_validator_key.json"),
		filepath.Join(home, "sidecar.db"),
	} {
		if !exists(p) {
			t.Errorf("home-root file destroyed by wipe: %s", p)
		}
	}
	if res.WipedBytes == 0 {
		t.Error("expected non-zero wipedBytes for a seeded data dir")
	}
}

func TestResetData_RemovesStateSyncMarker(t *testing.T) {
	home := seedHome(t)
	runReset(t, newResetDataer(home, false))
	if exists(filepath.Join(home, stateSyncMarkerFile)) {
		t.Error("state-sync marker survived the reset")
	}
}

func TestResetData_WritesEmptyPrivValidatorState(t *testing.T) {
	home := seedHome(t)
	runReset(t, newResetDataer(home, false))

	got, err := os.ReadFile(filepath.Join(home, "data", privValidatorStateFile))
	if err != nil {
		t.Fatalf("reading priv_validator_state: %v", err)
	}
	if string(got) != emptyPrivValidatorState {
		t.Errorf("priv_validator_state = %q, want %q", got, emptyPrivValidatorState)
	}
}

func TestResetData_IdempotentOverAlreadyWiped(t *testing.T) {
	home := seedHome(t)
	runReset(t, newResetDataer(home, false))

	// Second run over the already-wiped dir: success, and the sign-state is
	// the same fresh-empty content.
	res := runReset(t, newResetDataer(home, false))
	got, err := os.ReadFile(filepath.Join(home, "data", privValidatorStateFile))
	if err != nil {
		t.Fatalf("reading priv_validator_state after re-run: %v", err)
	}
	if string(got) != emptyPrivValidatorState {
		t.Errorf("priv_validator_state after re-run = %q, want %q", got, emptyPrivValidatorState)
	}
	// Only the small state file remains, so the second wipe measures little.
	if res.WipedBytes >= int64(len(emptyPrivValidatorState))+64 {
		t.Errorf("second-run wipedBytes unexpectedly large: %d", res.WipedBytes)
	}
}

func TestResetData_MissingDataDirIsSuccess(t *testing.T) {
	home := t.TempDir() // no data/ at all
	res := runReset(t, newResetDataer(home, false))
	if res.WipedBytes != 0 {
		t.Errorf("expected 0 wipedBytes for absent data dir, got %d", res.WipedBytes)
	}
	if !exists(filepath.Join(home, "data", privValidatorStateFile)) {
		t.Error("expected a fresh priv_validator_state to be created")
	}
}

func TestResetData_MeasurementFailureYieldsUnknownSize(t *testing.T) {
	home := seedHome(t)
	d := newResetDataer(home, false)
	d.measure = func(string) (int64, error) { return 0, fmt.Errorf("simulated measurement failure") }

	res := runReset(t, d)

	if res.WipedBytes != -1 {
		t.Errorf("WipedBytes = %d, want -1 (unknown) on measurement failure", res.WipedBytes)
	}
	// Measurement must not gate the wipe: the reset still cleared data/ and
	// wrote a fresh sign-state.
	if exists(filepath.Join(home, "data", "blockstore.db")) {
		t.Error("data not wiped after measurement failure — measurement gated the reset")
	}
	if !exists(filepath.Join(home, "data", privValidatorStateFile)) {
		t.Error("fresh priv_validator_state not written after measurement failure")
	}
}

func TestResetData_RefusesWhenRPCServing(t *testing.T) {
	home := seedHome(t)
	_, err := newResetDataer(home, true).Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected refusal when seid RPC is serving")
	}
	// Data must be untouched on refusal.
	if !exists(filepath.Join(home, "data", "blockstore.db")) {
		t.Error("blockstore.db was wiped despite RPC-serving refusal")
	}
}
