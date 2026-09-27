package main

import (
	"fmt"
	"sort"
	"strings"

	app "github.com/sidiora-labs/paxeer-network/node"
	"github.com/sidiora-labs/paxeer-network/sdk/baseapp"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	dbm "github.com/tendermint/tm-db"
)

// missingStoreMarker is the text the state-commit root multistore reports for a
// mounted store whose tree the state does not carry and no upgrade declares.
const missingStoreMarker = "new store is not added in upgrades: "

// absentTreeMarker is what the composite state-commit store panics with, in the
// memIAVL-only write mode every node runs, when it is asked for a store whose
// tree the state does not carry. It reaches the loader before the error above
// does, so the load of an undeclared store ends the process rather than
// returning a refusal.
const absentTreeMarker = "is not in keys.MemIAVLStoreKeys"

// preState is what a data directory carries before an upgrade plan runs: the
// committed height, the module stores whose trees the state carries, and the
// mounted stores it does not.
type preState struct {
	Height    int64
	Present   []string
	Missing   []string
	LoadError string
}

// probeState reads a data directory with the application's own store loader and
// reports which of the mounted stores the state carries. It mounts the full
// list first, which is what a node does, and drops the store the loader names
// on each refusal until the load succeeds, so the refusals themselves are the
// inventory of stores an upgrade plan has to declare as added.
//
// It runs under a bare base application rather than the full one because the
// full application exits the process when its store load fails, which would
// leave no report behind.
func probeState(home string, opts appOptions, names []string) (*preState, error) {
	missing := map[string]struct{}{}
	loadError := ""
	for attempt := 0; attempt <= len(names); attempt++ {
		mounted := withoutNames(names, missing)
		height, err := loadStores(home, opts, mounted)
		if err == nil {
			return &preState{
				Height:    height,
				Present:   mounted,
				Missing:   sortedNames(missing),
				LoadError: loadError,
			}, nil
		}
		name, named := missingStoreName(err)
		if !named {
			return nil, err
		}
		if loadError == "" {
			loadError = err.Error()
		}
		if _, seen := missing[name]; seen {
			return nil, fmt.Errorf("the store loader named %s twice: %w", name, err)
		}
		missing[name] = struct{}{}
	}
	return nil, fmt.Errorf("the store loader kept naming absent stores after %d attempts", len(names)+1)
}

// loadStores mounts names over the data directory of home and loads the latest
// committed version, returning that height or the loader's refusal. A refusal
// over an absent store arrives as a panic from the state-commit store, so the
// load runs under a recover.
func loadStores(home string, opts appOptions, names []string) (height int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			height, err = 0, fmt.Errorf("the store loader panicked: %v", recovered)
		}
	}()
	encodingConfig := app.MakeEncodingConfig()
	baseAppOptions, _ := app.SetupPaxDB(home, opts, nil)
	probe := baseapp.NewBaseApp(
		"upgrade-replay-probe",
		dbm.NewMemDB(),
		encodingConfig.TxConfig.TxDecoder(),
		nil,
		opts,
		baseAppOptions...,
	)
	defer func() { _ = probe.Close() }()
	probe.MountKVStores(sdk.NewKVStoreKeys(names...))
	if err := probe.LoadLatestVersion(); err != nil {
		return 0, err
	}
	return probe.LastBlockHeight(), nil
}

// missingStoreName returns the store the loader refused to load, when that is
// what it refused over, from either the root multistore's error or the
// state-commit store's panic.
func missingStoreName(err error) (string, bool) {
	text := err.Error()
	if at := strings.LastIndex(text, missingStoreMarker); at >= 0 {
		if fields := strings.Fields(text[at+len(missingStoreMarker):]); len(fields) > 0 {
			return fields[0], true
		}
	}
	if at := strings.Index(text, absentTreeMarker); at >= 0 {
		head := text[:at]
		if quoted := strings.LastIndex(head, `store "`); quoted >= 0 {
			rest := head[quoted+len(`store "`):]
			if closing := strings.Index(rest, `"`); closing > 0 {
				return rest[:closing], true
			}
		}
	}
	return "", false
}

// withoutNames is names without the excluded ones, in the order of names.
func withoutNames(names []string, excluded map[string]struct{}) []string {
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if _, drop := excluded[name]; drop {
			continue
		}
		kept = append(kept, name)
	}
	return kept
}

// sortedNames is the sorted key set of a name set.
func sortedNames(set map[string]struct{}) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
