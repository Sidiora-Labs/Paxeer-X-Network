package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	gigaconfig "github.com/sidiora-labs/paxeer-network/engine/executor/config"
	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	layerxanchortypes "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	layerxexchangetypes "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	xwebtypes "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	app "github.com/sidiora-labs/paxeer-network/node"
	feetokenprecompile "github.com/sidiora-labs/paxeer-network/precompiles/feetoken"
	launchpadprecompile "github.com/sidiora-labs/paxeer-network/precompiles/launchpad"
	anchorprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxanchor"
	bridgeprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxbridge"
	custodyprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxcustody"
	exchangeprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxexchange"
	verifyprecompile "github.com/sidiora-labs/paxeer-network/precompiles/layerxverify"
	xwebprecompile "github.com/sidiora-labs/paxeer-network/precompiles/xweb"
	"github.com/sidiora-labs/paxeer-network/sdk/baseapp"
	storetypes "github.com/sidiora-labs/paxeer-network/sdk/store/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	paxkeys "github.com/sidiora-labs/paxeer-network/storage/common/keys"
	paxutils "github.com/sidiora-labs/paxeer-network/storage/common/utils"
	paxconfig "github.com/sidiora-labs/paxeer-network/storage/config"
	"github.com/sidiora-labs/paxeer-network/wasm/x/wasm"
	dbm "github.com/tendermint/tm-db"

	tmproto "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
)

// defaultChainID is the chain the activation is planned for. It only reaches
// the replayed block header and the fixture's genesis; nothing in the harness
// contacts a node.
const defaultChainID = "hyperpax_125-1"

// forkStoreKeys are the module KV stores a pre-fork chain does not carry. They
// are the stores an activation plan has to declare as added, taken from each
// module's own types package.
var forkStoreKeys = []string{
	layerxcustodytypes.StoreKey,
	layerxanchortypes.StoreKey,
	layerxexchangetypes.StoreKey,
	layerxbridgetypes.StoreKey,
	launchpadtypes.StoreKey,
	xwebtypes.StoreKey,
}

// forkModules are the modules a pre-fork chain's version map does not carry.
var forkModules = []string{
	layerxcustodytypes.ModuleName,
	layerxanchortypes.ModuleName,
	layerxexchangetypes.ModuleName,
	layerxbridgetypes.ModuleName,
	launchpadtypes.ModuleName,
	xwebtypes.ModuleName,
}

// forkPrecompile is one of the eight addresses the fork brings online.
type forkPrecompile struct {
	Name    string
	Address string
}

// forkPrecompiles are the eight custom precompile addresses whose reachability
// the activation decides, in address order.
var forkPrecompiles = []forkPrecompile{
	{"layerxverify", verifyprecompile.LayerXVerifyAddress},
	{"layerxcustody", custodyprecompile.LayerXCustodyAddress},
	{"layerxanchor", anchorprecompile.LayerXAnchorAddress},
	{"layerxexchange", exchangeprecompile.ExchangeAddress},
	{"layerxbridge", bridgeprecompile.BridgeAddress},
	{"launchpad", launchpadprecompile.LaunchpadAddress},
	{"feetoken", feetokenprecompile.FeeTokenAddress},
	{"xweb", xwebprecompile.XWebAddress},
}

// allStoreKeys is the canonical, sorted mount list of module KV stores, the
// same list the application mounts.
func allStoreKeys() []string {
	names := make([]string, len(paxkeys.MemIAVLStoreKeys))
	copy(names, paxkeys.MemIAVLStoreKeys)
	sort.Strings(names)
	return names
}

// legacyStoreKeys is the mount list without the stores the fork adds: the
// store set a pre-fork chain carries.
func legacyStoreKeys() []string {
	fork := make(map[string]struct{}, len(forkStoreKeys))
	for _, name := range forkStoreKeys {
		fork[name] = struct{}{}
	}
	names := make([]string, 0, len(paxkeys.MemIAVLStoreKeys))
	for _, name := range allStoreKeys() {
		if _, ok := fork[name]; ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

// appOptions is the option set daemon/paxd hands the application, reduced to
// what a replay needs: the state-commit backend on, the versioned state store
// on when the state carries one, snapshots and the EVM servers off, and the
// Giga executor off.
type appOptions struct {
	values map[string]interface{}
}

// newAppOptions mirrors the store options daemon/paxd reads out of app.toml.
// With stateStore set, the versioned state store is opened beside the commit
// store on the node's own backend and with the node's own defaults, which is
// how a copy of a live data directory has to be opened; without it the commit
// store stands alone, which is the shape of a generated fixture.
func newAppOptions(chainID string, stateStore bool) appOptions {
	values := map[string]interface{}{
		"chain-id":                 chainID,
		app.FlagSCEnable:           true,
		app.FlagSCSnapshotInterval: uint32(0),
		app.FlagSSEnable:           stateStore,
		app.FlagSnapshotInterval:   uint64(0),
		gigaconfig.FlagEnabled:     false,
		gigaconfig.FlagOCCEnabled:  false,
		"evm.http_enabled":         false,
		"evm.ws_enabled":           false,
	}
	if stateStore {
		values[app.FlagSSBackend] = paxconfig.DefaultSSBackend
		values[app.FlagSSAsyncWriterBuffer] = paxconfig.DefaultSSAsyncBuffer
		values[app.FlagSSKeepRecent] = paxconfig.DefaultSSKeepRecent
		values[app.FlagSSPruneInterval] = paxconfig.DefaultSSPruneInterval
		values[app.FlagSSImportNumWorkers] = paxconfig.DefaultSSImportWorkers
	}
	return appOptions{values: values}
}

// stateStorePresent reports whether the data directory under home carries the
// versioned state store a node writes beside its commit store, resolved the way
// the store code resolves it, so a legacy directory counts as well.
func stateStorePresent(home string) bool {
	info, err := os.Stat(paxutils.GetStateStorePath(home, paxconfig.DefaultSSBackend))
	return err == nil && info.IsDir()
}

func (o appOptions) Get(key string) interface{} { return o.values[key] }

// resolveDataDir accepts either a chain data directory or the application home
// that holds it, and returns the data directory.
func resolveDataDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	if isDataDir(abs) {
		return abs, nil
	}
	nested := filepath.Join(abs, "data")
	if isDataDir(nested) {
		return nested, nil
	}
	return "", fmt.Errorf("%s holds no state-commit store: expected committer.db or state_commit/memiavl in it or in its data subdirectory", abs)
}

// isDataDir reports whether dir carries a state-commit store, at either the
// legacy or the current path.
func isDataDir(dir string) bool {
	for _, marker := range []string{"committer.db", filepath.Join("state_commit", "memiavl")} {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// prepareHome builds the throwaway application home the replay runs against:
// its data directory is the copy under replay, and the wasm and config
// directories that sit beside that copy are linked in when they exist, because
// the application reads both from the home.
func prepareHome(home, dataDir string) error {
	if err := os.MkdirAll(home, 0o750); err != nil {
		return err
	}
	if err := linkInto(home, "data", dataDir); err != nil {
		return err
	}
	parent := filepath.Dir(dataDir)
	for _, name := range []string{"wasm", "config"} {
		src := filepath.Join(parent, name)
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue
		}
		if err := linkInto(home, name, src); err != nil {
			return err
		}
	}
	return nil
}

// linkInto points home/name at target, replacing a link this harness wrote
// earlier and refusing to replace anything else.
func linkInto(home, name, target string) error {
	link := filepath.Join(home, name)
	info, err := os.Lstat(link)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		current, err := os.Readlink(link)
		if err != nil {
			return err
		}
		if current == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	case err == nil:
		return fmt.Errorf("%s already exists and is not a link this harness wrote", link)
	case !os.IsNotExist(err):
		return err
	}
	return os.Symlink(target, link)
}

// openApp builds the application over home exactly as daemon/paxd builds it:
// the same store keys, the same state-commit backend, custom EVM precompiles
// on and pruning nothing. It reads upgrade-info.json from home/data, so a plan
// written there before this call mounts that plan's added stores.
func openApp(home string, opts appOptions) *app.App {
	return app.New(
		dbm.NewMemDB(),
		nil,
		true,
		map[int64]bool{},
		home,
		1,
		true,
		nil,
		app.MakeEncodingConfig(),
		wasm.EnableAllProposals,
		opts,
		app.EmptyWasmOpts,
		app.EmptyAppOptions,
		baseapp.SetPruning(storetypes.PruneNothing),
	)
}

// blockContext is a context over the committed multistore at height, with the
// begin-block header the upgrade module's begin blocker would carry and no gas
// limit, which is what x/upgrade applies a plan under.
func blockContext(a *app.App, height int64, chainID string, blockTime time.Time) sdk.Context {
	header := tmproto.Header{ChainID: chainID, Height: height, Time: blockTime}
	return a.NewUncachedContext(false, header).WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))
}
