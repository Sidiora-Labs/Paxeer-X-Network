package workflow

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRender_StateSyncGolden(t *testing.T) {
	got, err := render(renderArgs{
		preset:     "state-sync",
		name:       "pacific-1-rpc-0-state-sync",
		namespace:  "pacific-1",
		target:     "pacific-1-rpc-0",
		migration:  "GigaStore",
		backend:    "pebbledb",
		rpcServers: []string{"a.example:26657", "b.example:26657"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if k := got.GroupVersionKind().Kind; k != "SeiNodeTaskWorkflow" {
		t.Errorf("kind = %q; want SeiNodeTaskWorkflow", k)
	}
	if got.GetName() != "pacific-1-rpc-0-state-sync" {
		t.Errorf("name = %q; want pacific-1-rpc-0-state-sync", got.GetName())
	}
	if got.GetNamespace() != "pacific-1" {
		t.Errorf("namespace = %q; want pacific-1", got.GetNamespace())
	}

	recipe, _, _ := unstructured.NestedString(got.Object, "spec", "kind")
	if recipe != "StateSync" {
		t.Errorf("spec.kind = %q; want StateSync", recipe)
	}
	target, _, _ := unstructured.NestedString(got.Object, "spec", "target", "nodeRef", "name")
	if target != "pacific-1-rpc-0" {
		t.Errorf("spec.target.nodeRef.name = %q; want pacific-1-rpc-0", target)
	}

	servers, found, _ := unstructured.NestedStringSlice(got.Object, "spec", "stateSync", "rpcServers")
	if !found || len(servers) != 2 || servers[0] != "a.example:26657" || servers[1] != "b.example:26657" {
		t.Errorf("spec.stateSync.rpcServers = %v (found=%v); want [a.example:26657 b.example:26657]", servers, found)
	}

	// The typed migration union: spec.stateSync.migration.kind is the workflow
	// kind, and the kind<->payload CEL requires gigaStore present with a backend.
	migKind, found, err := unstructured.NestedString(got.Object, "spec", "stateSync", "migration", "kind")
	if err != nil || !found {
		t.Fatalf("spec.stateSync.migration.kind not found (found=%v err=%v)", found, err)
	}
	if migKind != "GigaStore" {
		t.Errorf("spec.stateSync.migration.kind = %q; want GigaStore", migKind)
	}
	backend, found, err := unstructured.NestedString(got.Object, "spec", "stateSync", "migration", "gigaStore", "backend")
	if err != nil || !found {
		t.Fatalf("spec.stateSync.migration.gigaStore.backend not found (found=%v err=%v)", found, err)
	}
	if backend != "pebbledb" {
		t.Errorf("spec.stateSync.migration.gigaStore.backend = %q; want pebbledb", backend)
	}
}

// The backend value must land verbatim under gigaStore for each legal backend.
func TestRender_StateSyncBackendRoundTrip(t *testing.T) {
	for _, backend := range gigaStoreBackends {
		got, err := render(renderArgs{
			preset:    "state-sync",
			name:      "n-state-sync",
			target:    "n",
			migration: "GigaStore",
			backend:   backend,
		})
		if err != nil {
			t.Fatalf("render backend=%s: %v", backend, err)
		}
		gotBackend, found, _ := unstructured.NestedString(got.Object, "spec", "stateSync", "migration", "gigaStore", "backend")
		if !found || gotBackend != backend {
			t.Errorf("spec.stateSync.migration.gigaStore.backend = %q (found=%v); want %q", gotBackend, found, backend)
		}
	}
}

// The minimal render (target only) still produces a valid StateSync workflow:
// stateSync present (CEL requires it) with no recipe params, and — critically —
// no migration union at all (a standard resync must not carry a migration).
func TestRender_MinimalStateSync(t *testing.T) {
	got, err := render(renderArgs{preset: "state-sync", name: "n-state-sync", target: "n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, found, _ := unstructured.NestedMap(got.Object, "spec", "stateSync"); !found {
		t.Error("spec.stateSync must be present for kind=StateSync (CEL requires it)")
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "spec", "stateSync", "migration"); found {
		t.Error("spec.stateSync.migration must be absent for a standard resync (no --migration)")
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "spec", "stateSync", "rpcServers"); found {
		t.Error("spec.stateSync.rpcServers should be absent when --rpc-servers is not given")
	}
}

func TestRender_MissingTargetIsUsageError(t *testing.T) {
	_, err := render(renderArgs{preset: "state-sync", name: "n-state-sync"})
	if err == nil {
		t.Fatal("expected a usage error when --target is missing")
	}
}

func TestRender_UnknownPreset(t *testing.T) {
	_, err := render(renderArgs{preset: "nope", name: "n", target: "n"})
	if err == nil {
		t.Fatal("expected an error for an unknown preset")
	}
}

func TestRender_RequirePhaseOverride(t *testing.T) {
	got, err := render(renderArgs{preset: "state-sync", name: "n-state-sync", target: "n", requirePhase: "Running"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	rp, _, _ := unstructured.NestedString(got.Object, "spec", "target", "requirePhase")
	if rp != "Running" {
		t.Errorf("spec.target.requirePhase = %q; want Running", rp)
	}
}

func TestRender_InvalidRequirePhaseRejected(t *testing.T) {
	_, err := render(renderArgs{preset: "state-sync", name: "n-state-sync", target: "n", requirePhase: "Bogus"})
	if err == nil {
		t.Fatal("expected a usage error for a --require-phase outside the SeiNode enum")
	}
}

// The client-side --migration/--backend contract: each combination that render
// must refuse before touching the object. Guards the path strings and the
// two-token safety property against a silent regression.
func TestRender_MigrationFlagValidation(t *testing.T) {
	cases := []struct {
		name      string
		migration string
		backend   string
		wantErr   string // substring the UsageError message must contain
	}{
		{"backend without migration", "", "pebbledb", "--backend is only valid with --migration"},
		{"migration without backend", "GigaStore", "", "--migration GigaStore requires --backend"},
		{"unknown migration kind", "Bogus", "pebbledb", `--migration "Bogus" unknown`},
		// An unknown kind without a backend names the bad kind, not the missing
		// backend: the enum check is ordered before the required-pairing check.
		{"unknown kind without backend", "Bogus", "", `--migration "Bogus" unknown`},
		{"invalid backend value", "GigaStore", "leveldb", `--backend "leveldb" invalid`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := render(renderArgs{
				preset:    "state-sync",
				name:      "n-state-sync",
				target:    "n",
				migration: tc.migration,
				backend:   tc.backend,
			})
			if err == nil {
				t.Fatalf("expected a usage error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q; want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// A valid pair renders without error (the happy path of the same contract).
func TestRender_MigrationValidPairAccepted(t *testing.T) {
	if _, err := render(renderArgs{
		preset: "state-sync", name: "n-state-sync", target: "n",
		migration: "GigaStore", backend: "rocksdb",
	}); err != nil {
		t.Fatalf("render valid migration pair: %v", err)
	}
}
