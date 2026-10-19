package workflow

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// seiNodePhases is the SeiNodePhase enum (the target node's phase). Used to
// validate --require-phase client-side, so a typo is a crisp usage error rather
// than an apiserver Invalid wall.
var seiNodePhases = []string{"Pending", "Initializing", "Running", "Failed", "Terminating"}

// configMigrationKinds is the spec.stateSync.migration.kind enum. Validated
// client-side so a typo is a crisp usage error rather than the CRD's kind<->payload
// CEL wall. GigaStore is the only migration today.
var configMigrationKinds = []string{"GigaStore"}

// gigaStoreBackends is the spec.stateSync.migration.gigaStore.backend enum
// (mirrors the CRD enum pebbledb;rocksdb). --backend is required with --migration:
// the explicit two-token form is the safety property, not the CRD default.
var gigaStoreBackends = []string{"pebbledb", "rocksdb"}

type renderArgs struct {
	preset       string
	name         string
	namespace    string
	target       string
	requirePhase string
	migration    string // spec.stateSync.migration.kind (verbatim; "" = standard resync)
	backend      string // spec.stateSync.migration.gigaStore.backend
	rpcServers   []string
	sets         []string
}

// validateMigrationFlags enforces the client-side --migration/--backend contract
// (checked before render touches the object), mirroring the --require-phase enum
// check. Order names the actual mistake: an orphan --backend first, then an
// unknown migration kind (so --migration Bogus reports the bad kind, not a
// missing backend), then the required pairing, then an invalid backend value.
func validateMigrationFlags(migration, backend string) error {
	if backend != "" && migration == "" {
		return cliutil.UsageError("--backend is only valid with --migration; a standard resync takes no backend")
	}
	if migration != "" && !slices.Contains(configMigrationKinds, migration) {
		return cliutil.UsageError("--migration %q unknown; supported: %s", migration, strings.Join(configMigrationKinds, ", "))
	}
	if migration != "" && backend == "" {
		return cliutil.UsageError("--migration GigaStore requires --backend <pebbledb|rocksdb>")
	}
	if backend != "" && !slices.Contains(gigaStoreBackends, backend) {
		return cliutil.UsageError("--backend %q invalid; supported: %s", backend, strings.Join(gigaStoreBackends, ", "))
	}
	return nil
}

// render builds a SeiNodeTaskWorkflow CR from an embedded preset and the CLI
// arguments. The result is the exact JSON the apiserver receives, so it is the
// object the jsoncontract tests lock against.
func render(args renderArgs) (*unstructured.Unstructured, error) {
	if args.preset == "" {
		return nil, cliutil.UsageError("--preset is required (known: %s)", strings.Join(presetNames(), ", "))
	}
	if args.name == "" {
		return nil, cliutil.UsageError("name is required")
	}
	if args.target == "" {
		return nil, cliutil.UsageError("--target <node> is required: the SeiNode this workflow operates on")
	}

	data, err := loadPreset(args.preset)
	if err != nil {
		return nil, cliutil.UsageError("%s", err.Error())
	}

	jsonBytes, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse preset %q: %w", args.preset, err)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(jsonBytes); err != nil {
		return nil, fmt.Errorf("decode preset %q: %w", args.preset, err)
	}
	u.SetGroupVersionKind(kind.GVK)

	// Target node — the single SeiNode this workflow acts on. Locked field name
	// spec.target.nodeRef.name (SeiNodeTaskTarget, shared with SeiNodeTask).
	if err := unstructured.SetNestedField(u.Object, args.target, "spec", "target", "nodeRef", "name"); err != nil {
		return nil, fmt.Errorf("apply --target: %w", err)
	}
	if args.requirePhase != "" {
		if !slices.Contains(seiNodePhases, args.requirePhase) {
			return nil, cliutil.UsageError("--require-phase %q invalid; legal SeiNode phases: %s",
				args.requirePhase, strings.Join(seiNodePhases, ", "))
		}
		if err := unstructured.SetNestedField(u.Object, args.requirePhase, "spec", "target", "requirePhase"); err != nil {
			return nil, fmt.Errorf("apply --require-phase: %w", err)
		}
	}

	// StateSync recipe parameters. --migration selects the typed migration union
	// (spec.stateSync.migration); rpcServers overrides witness resolution (the
	// controller fails the plan closed on fewer than two). A standard resync
	// (migration == "") writes nothing under stateSync.
	if err := validateMigrationFlags(args.migration, args.backend); err != nil {
		return nil, err
	}
	if args.migration != "" {
		// apply can render other presets; migration is StateSync-only. Gate on the
		// rendered recipe kind so a --migration against a non-StateSync preset is a
		// crisp usage error, not a stray field the CEL union would reject.
		if recipe, _, _ := unstructured.NestedString(u.Object, "spec", "kind"); recipe != "StateSync" {
			return nil, cliutil.UsageError("--migration only applies to the StateSync recipe")
		}
		if err := unstructured.SetNestedField(u.Object, args.migration, "spec", "stateSync", "migration", "kind"); err != nil {
			return nil, fmt.Errorf("apply --migration: %w", err)
		}
		// The CRD's kind<->payload CEL requires the gigaStore payload present when
		// kind=GigaStore. The backend write below already materializes it (--backend
		// is required); this explicit payload write documents the union requirement
		// and stays correct if a future kind carries an all-optional payload.
		if err := unstructured.SetNestedField(u.Object, map[string]interface{}{}, "spec", "stateSync", "migration", "gigaStore"); err != nil {
			return nil, fmt.Errorf("apply --migration gigaStore payload: %w", err)
		}
		if err := unstructured.SetNestedField(u.Object, args.backend, "spec", "stateSync", "migration", "gigaStore", "backend"); err != nil {
			return nil, fmt.Errorf("apply --backend: %w", err)
		}
	}
	if len(args.rpcServers) > 0 {
		servers := make([]interface{}, len(args.rpcServers))
		for i, s := range args.rpcServers {
			servers[i] = s
		}
		if err := unstructured.SetNestedSlice(u.Object, servers, "spec", "stateSync", "rpcServers"); err != nil {
			return nil, fmt.Errorf("apply --rpc-servers: %w", err)
		}
	}

	for _, expr := range args.sets {
		if err := cliutil.ApplySet(u.Object, expr); err != nil {
			return nil, cliutil.UsageError("apply --set %q: %s", expr, err.Error())
		}
	}

	// Reassert identity after --set so a --set metadata.namespace=... cannot
	// silently retarget the object.
	u.SetName(args.name)
	if args.namespace != "" {
		u.SetNamespace(args.namespace)
	}

	// NOT A TRUST BOUNDARY — anyone with `kubectl edit` can forge these.
	annotations := u.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["seictl.sei.io/preset"] = args.preset
	annotations["seictl.sei.io/version"] = cliutil.Version
	u.SetAnnotations(annotations)

	// A workflow with no recipe kind can never be adopted — fail before SSA.
	if recipe, _, _ := unstructured.NestedString(u.Object, "spec", "kind"); recipe == "" {
		return nil, cliutil.UsageError("preset %q produced no spec.kind", args.preset)
	}

	return u, nil
}

// configPatchRemovedError is the erroring shim for the removed --config-patch
// flag. The flag stays defined so urfave recognizes it; a non-empty value is a
// hard usage error rather than a silent auto-translate (alpha, no external
// consumers — the shim is the safe removal).
func configPatchRemovedError() error {
	return cliutil.UsageError("--config-patch was removed: config patching is now a typed migration. " +
		"Use --migration GigaStore --backend <pebbledb|rocksdb>. A standard resync takes no migration flag.")
}

// emitMigrationPreamble prints the loud destructive-migration warning to w
// before Apply (fires for --dry-run too), matching the "seictl: ..." line style.
func emitMigrationPreamble(w io.Writer, migration, backend, node string) {
	// w is an io.Writer interface, so errcheck (enabled in .golangci.yml) requires
	// the return handled — its default exclusion only covers a literal os.Stderr.
	_, _ = fmt.Fprintf(w, "seictl: MIGRATION %s backend=%s DESTRUCTIVE wipe-and-resync of %s; "+
		"local state discarded, node re-bootstraps on %s\n", migration, backend, node, backend)
}
