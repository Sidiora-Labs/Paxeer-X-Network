package tasks

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var resetDataLog = seilog.NewLogger("seictl", "task", "reset-data")

// privValidatorStateFile is CometBFT's last-sign-state file. It lives inside
// data/ (unlike node_key.json / priv_validator_key.json, which live in config/
// and are therefore outside the wipe), so the reset rewrites it fresh.
const privValidatorStateFile = "priv_validator_state.json"

// emptyPrivValidatorState is the reset last-sign-state (unsafe-reset-all
// semantics): height is a JSON string, round and step are numbers. RPC nodes
// do not sign, and validators are excluded from the recipe, so writing a fresh
// zero state is always safe here.
const emptyPrivValidatorState = `{"height":"0","round":0,"step":0}` + "\n"

// ResetDataResult is the reset-data task's structured result. WipedBytes is the
// pre-wipe on-disk size of data/ (regular files only), surfaced so the
// workflow's hold event can report how much was cleared. It is -1 when the
// measurement failed: the count is observability, never a gate, so a failed
// measurement does not block the reset. Symlinked entries are counted by their
// own (link) size rather than their target, so a data dir that uses symlinks
// may undercount — acceptable for an observability figure.
type ResetDataResult struct {
	WipedBytes int64 `json:"wipedBytes"`
}

// ResetDataer clears the chain data directory for a state-sync re-bootstrap.
// The wipe is scoped to <homeDir>/data/ and nothing else: the home root holds
// config/ (node identity), the sidecar's task ledger (sidecar.db — the database
// that resumes this very wipe after a crash), and the hold sentinel/markers.
// Wiping the home root would destroy the machinery mid-flight; this is the
// design's most important correctness rule.
//
// The reset needs no atomicity of its own. A partially deleted data directory
// is only dangerous if seid starts on it, and the node hold guarantees it does
// not. As defense-in-depth the handler refuses to run while seid's local RPC is
// serving (i.e. the node is not actually held). It is content-idempotent: an
// already-wiped directory is success.
type ResetDataer struct {
	homeDir string
	probeUp func(ctx context.Context) bool
	// measure returns the pre-wipe size of data/. A test seam; defaults to
	// dirSize when nil.
	measure func(dir string) (int64, error)
}

// NewResetDataer builds a ResetDataer rooted at homeDir with the real local-RPC
// serving probe.
func NewResetDataer(homeDir string) *ResetDataer {
	statusClient := rpc.NewStatusClient("", nil)
	return &ResetDataer{
		homeDir: homeDir,
		probeUp: func(ctx context.Context) bool { return seidRPCUp(ctx, statusClient) },
		measure: dirSize,
	}
}

// Handler returns an engine.TaskHandler for the reset-data task type. Params
// are empty; the result carries the pre-wipe byte count.
func (d *ResetDataer) Handler() engine.TaskHandler {
	return engine.TypedHandlerWithResult(func(ctx context.Context, _ struct{}) (ResetDataResult, error) {
		return d.reset(ctx)
	})
}

func (d *ResetDataer) reset(ctx context.Context) (ResetDataResult, error) {
	// Defense-in-depth: a serving RPC means seid is running, so the node is not
	// held and a wipe would race a live process. Refuse rather than wipe under
	// it (mirrors restart-seid's refusal to report a stop that did not happen).
	if d.probeUp(ctx) {
		return ResetDataResult{}, fmt.Errorf("reset-data: seid RPC is serving; node is not held — refusing to wipe a live data directory")
	}

	dataDir := filepath.Join(d.homeDir, "data")

	// Measurement is observability only — never let it gate the wipe. On any
	// non-ENOENT failure, log and proceed with an unknown (-1) size.
	measure := d.measure
	if measure == nil {
		measure = dirSize
	}
	size, err := measure(dataDir)
	if err != nil {
		resetDataLog.Warn("measuring data dir failed; proceeding with unknown size", "dir", dataDir, "err", err)
		size = -1
	}

	if err := wipeDirContents(dataDir); err != nil {
		return ResetDataResult{}, fmt.Errorf("reset-data: wiping %s: %w", dataDir, err)
	}

	// Recreate data/ (the wipe may have removed it if it was empty of anything
	// but itself) and drop a fresh zero sign-state.
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return ResetDataResult{}, fmt.Errorf("reset-data: recreating %s: %w", dataDir, err)
	}
	statePath := filepath.Join(dataDir, privValidatorStateFile)
	if err := os.WriteFile(statePath, []byte(emptyPrivValidatorState), 0o600); err != nil {
		return ResetDataResult{}, fmt.Errorf("reset-data: writing %s: %w", statePath, err)
	}

	// Clear the state-sync completion marker (home root, outside data/) so the
	// downstream configure-state-sync reruns instead of short-circuiting.
	markerPath := filepath.Join(d.homeDir, stateSyncMarkerFile)
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return ResetDataResult{}, fmt.Errorf("reset-data: removing marker %s: %w", markerPath, err)
	}

	resetDataLog.Info("data directory reset", "dir", dataDir, "wipedBytes", size)
	return ResetDataResult{WipedBytes: size}, nil
}

// wipeDirContents removes every entry under dir, leaving dir itself. A missing
// dir is success (content-idempotent: already-empty is the goal state), and a
// concurrent peer removing an entry first (ENOENT) is tolerated so a rehydrated
// re-run cannot fail on a half-wiped tree.
func wipeDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	return nil
}

// dirSize sums the on-disk size of regular files under dir. A missing dir is
// zero. Errors from entries that vanish mid-walk (ENOENT) are ignored — the
// count is observability, not a correctness signal.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}
