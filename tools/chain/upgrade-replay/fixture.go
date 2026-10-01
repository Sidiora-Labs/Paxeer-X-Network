package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	abci "github.com/sidiora-labs/paxeer-network/consensus/abci/types"
	tmproto "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
	app "github.com/sidiora-labs/paxeer-network/node"
	"github.com/sidiora-labs/paxeer-network/sdk/baseapp"
	storetypes "github.com/sidiora-labs/paxeer-network/sdk/store/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
	dbm "github.com/tendermint/tm-db"
)

// generateFixture writes a chain state shaped like a pre-fork chain: its genesis
// carries only the modules such a chain has and its version map names only those
// modules. With keepStores it leaves the fork modules' stores mounted, which is
// the shape of a chain whose binary already carried them while its version map
// never did; otherwise it deletes them, which is the shape of a chain whose
// binary predates them. It is what the replay runs against until a copy of a
// live chain's state is available.
func generateFixture(home, chainID string, blocks int, keepStores bool) (int64, error) {
	if _, err := os.Stat(filepath.Join(home, "data")); err == nil {
		return 0, fmt.Errorf("fixture data already exists: %s", home)
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if blocks < 1 {
		blocks = 1
	}
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o750); err != nil {
		return 0, err
	}
	opts := newAppOptions(chainID, false)
	a := openApp(home, opts)

	cdc := app.MakeEncodingConfig().Marshaler
	genesis := app.NewDefaultGenesisState(cdc)
	dropped := make([]string, 0, len(forkModules))
	for _, name := range forkModules {
		if _, carried := genesis[name]; carried {
			delete(genesis, name)
			dropped = append(dropped, name)
		}
	}
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	if err != nil {
		_ = a.Close()
		return 0, err
	}
	fmt.Printf("the fixture genesis carries %d module entries and leaves out %s\n",
		len(genesis), strings.Join(dropped, ", "))

	if recovered := guard(func() error {
		_, initErr := a.InitChain(context.Background(), &abci.RequestInitChain{
			Time:            time.Now().UTC(),
			ChainId:         chainID,
			ConsensusParams: app.DefaultConsensusParams,
			AppStateBytes:   stateBytes,
			InitialHeight:   1,
		})
		return initErr
	}); recovered != nil {
		fmt.Printf("note: the genesis pass reported %v; the committed height and version map below say what landed\n", recovered)
	}
	if _, err := a.Commit(context.Background()); err != nil {
		_ = a.Close()
		return 0, fmt.Errorf("committing the genesis: %w", err)
	}
	if a.LastBlockHeight() != 1 {
		_ = a.Close()
		return 0, fmt.Errorf("the genesis committed height %d, want 1", a.LastBlockHeight())
	}
	if versions := a.UpgradeKeeper.GetModuleVersionMap(blockContext(a, 1, chainID, time.Now().UTC())); len(versions) == 0 {
		_ = a.Close()
		return 0, fmt.Errorf("the genesis left no module version map, so it did not land")
	}

	for height := int64(2); height <= int64(blocks); height++ {
		if _, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: a.LastCommitID().Hash,
			Header: &tmproto.Header{
				ChainID: chainID,
				Height:  height,
				Time:    time.Now().UTC(),
			},
		}); err != nil {
			_ = a.Close()
			return 0, fmt.Errorf("block %d: %w", height, err)
		}
		if _, err := a.Commit(context.Background()); err != nil {
			_ = a.Close()
			return 0, fmt.Errorf("committing block %d: %w", height, err)
		}
	}

	// A chain that never ran the fork's modules has no version map entry for
	// them, which is how the upgrade handler tells a pre-fork chain from one
	// that started with the modules in its genesis.
	ctx := blockContext(a, a.LastBlockHeight()+1, chainID, time.Now().UTC())
	versionStore := ctx.KVStore(a.GetKey(upgradetypes.StoreKey))
	for _, name := range forkModules {
		versionStore.Delete(append([]byte{upgradetypes.VersionMapByte}, []byte(name)...))
	}
	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	committed := a.CommitMultiStore().Commit(true).Version
	fmt.Printf("the fixture version map carries %d modules once the %d fork modules are dropped\n",
		len(versions), len(forkModules))
	if err := a.Close(); err != nil {
		return 0, fmt.Errorf("closing the fixture application: %w", err)
	}

	if keepStores {
		fmt.Printf("the fixture state-commit store keeps all %d module stores at height %d\n",
			len(allStoreKeys()), committed)
		return committed, nil
	}
	height, err := dropForkStores(home, opts)
	if err != nil {
		return 0, fmt.Errorf("dropping the fork stores: %w", err)
	}
	fmt.Printf("the fixture state-commit store carries %d module stores at height %d\n",
		len(legacyStoreKeys()), height)
	return height, nil
}

// dropForkStores deletes the stores the fork adds through the store upgrade
// path the application itself uses, leaving a state-commit store with only the
// trees a pre-fork chain carries.
func dropForkStores(home string, opts appOptions) (int64, error) {
	encodingConfig := app.MakeEncodingConfig()
	baseAppOptions, _ := app.SetupPaxDB(home, opts, nil)
	fixture := baseapp.NewBaseApp(
		"upgrade-replay-fixture",
		dbm.NewMemDB(),
		encodingConfig.TxConfig.TxDecoder(),
		nil,
		opts,
		baseAppOptions...,
	)
	defer func() { _ = fixture.Close() }()
	fixture.MountKVStores(sdk.NewKVStoreKeys(legacyStoreKeys()...))
	fixture.SetStoreLoader(func(cms sdk.CommitMultiStore) error {
		return cms.LoadLatestVersionAndUpgrade(&storetypes.StoreUpgrades{Deleted: forkStoreKeys})
	})
	if err := fixture.LoadLatestVersion(); err != nil {
		return 0, err
	}
	return fixture.CommitMultiStore().Commit(true).Version, nil
}

// guard runs fn and returns what it returned or panicked with.
func guard(fn func() error) (recovered interface{}) {
	defer func() {
		if panicked := recover(); panicked != nil {
			recovered = panicked
		}
	}()
	if err := fn(); err != nil {
		return err
	}
	return nil
}
