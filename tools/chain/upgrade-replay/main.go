// Command upgrade-replay proves an upgrade plan on a copy of a chain's state.
//
// It opens a data directory with the real application, mounts the plan's store
// upgrades the way a node does when upgrade-info.json names the plan, applies
// the plan's handler in a context at the upgrade height, asserts what the fork
// owes the chain, commits once and reopens the store. Every assertion is
// printed; any failed assertion makes the command exit non-zero.
//
// It touches nothing but the data directory it is given, which it writes to, so
// it must be handed a disposable copy. With no data directory it first generates
// a chain state shaped like a pre-fork chain and replays against that.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
)

func main() {
	mode := flag.String("mode", "replay", "replay a plan over a data directory, apply it in the current process, or write a fixture: replay | apply | fixture")
	dataDir := flag.String("data", "", "chain data directory to replay against; it is written to, so hand over a disposable copy")
	home := flag.String("home", "", "throwaway application home whose data directory is the one replayed (default: a directory beside it)")
	plan := flag.String("plan", app.V610Upgrade, "name of the upgrade plan to replay")
	chainID := flag.String("chain-id", defaultChainID, "chain id of the replayed block header and of a generated fixture")
	out := flag.String("out", "", "application home to write a fixture into (fixture mode)")
	blocks := flag.Int("blocks", 5, "blocks to commit after the genesis of a fixture (fixture mode)")
	keepStores := flag.Bool("keep-stores", false, "leave the fork modules' stores in a generated fixture, the shape of a chain whose binary carried them while its version map never did (fixture mode)")
	preHeight := flag.Int64("pre-height", 0, "committed height the state carried before the plan (apply mode)")
	preMissing := flag.String("pre-missing", "", "comma separated stores the state did not carry before the plan (apply mode)")
	flag.Parse()

	if err := run(*mode, *dataDir, *home, *plan, *chainID, *out, *blocks, *keepStores, *preHeight, *preMissing); err != nil {
		fmt.Fprintf(os.Stderr, "upgrade-replay: %v\n", err)
		os.Exit(1)
	}
}

func run(mode, dataDir, home, plan, chainID, out string, blocks int, keepStores bool, preHeight int64, preMissing string) error {
	switch mode {
	case "fixture":
		if out == "" {
			return errors.New("fixture mode needs -out, the application home to write the fixture into")
		}
		height, err := generateFixture(out, chainID, blocks, keepStores)
		if err != nil {
			return err
		}
		if plan == app.V610Upgrade {
			if err := runReplay(filepath.Join(out, "data"), filepath.Join(out, "activation-home"), app.ActivationUpgrade, chainID); err != nil {
				return err
			}
			pre, err := probeState(filepath.Join(out, "activation-home"), newAppOptions(chainID, false), allStoreKeys())
			if err != nil {
				return err
			}
			height = pre.Height
		}
		fmt.Printf("fixture written at height %d: %s\n", height, filepath.Join(out, "data"))
		return nil
	case "replay":
		return runReplay(dataDir, home, plan, chainID)
	case "apply":
		return runApply(dataDir, home, plan, chainID, preHeight, preMissing)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

// runReplay reads the state, reports what it carries and applies the plan in a
// child process.
func runReplay(dataDir, home, plan, chainID string) error {
	if dataDir == "" {
		return errors.New("replay mode needs -data, the chain data directory to replay against")
	}
	resolved, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}
	if home == "" {
		home = filepath.Join(filepath.Dir(resolved), "upgrade-replay-home")
	}
	if err := prepareHome(home, resolved); err != nil {
		return err
	}
	opts := newAppOptions(chainID, stateStorePresent(home))
	if stateStorePresent(home) {
		fmt.Println("the state carries a versioned state store, so it is opened beside the commit store on the node's backend")
	} else {
		fmt.Println("the state carries no versioned state store, so the commit store is opened alone")
	}

	pre, err := probeState(home, opts, allStoreKeys())
	if err != nil {
		return fmt.Errorf("reading the state: %w", err)
	}
	fmt.Printf("the state is at height %d and carries %d of the %d module stores\n",
		pre.Height, len(pre.Present), len(allStoreKeys()))
	if len(pre.Missing) == 0 {
		fmt.Println("the state carries every module store, so it loads without a store upgrade")
	} else {
		fmt.Printf("the state does not carry %d stores: %s\n", len(pre.Missing), strings.Join(pre.Missing, ", "))
		fmt.Printf("the store loader's first refusal: %s\n", pre.LoadError)
	}
	if plan == app.V610Upgrade && pre.ActivationHeight == 0 {
		if err := applyInChild(resolved, home, app.ActivationUpgrade, chainID, pre); err != nil {
			return err
		}
		pre, err = probeState(home, opts, allStoreKeys())
		if err != nil {
			return err
		}
		if pre.ActivationHeight != pre.Height {
			return fmt.Errorf("activation did not commit at the preceding height")
		}
	}
	return applyInChild(resolved, home, plan, chainID, pre)
}

// runApply applies the plan in the current process and reports every assertion.
func runApply(dataDir, home, plan, chainID string, preHeight int64, preMissing string) error {
	if home == "" {
		return errors.New("apply mode needs -home, the application home the replay prepared")
	}
	if preHeight <= 0 {
		return errors.New("apply mode needs -pre-height, the committed height the state carried")
	}
	pre := &preState{Height: preHeight}
	for _, name := range strings.Split(preMissing, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			pre.Missing = append(pre.Missing, trimmed)
		}
	}
	failures := 0
	if recovered := guard(func() error {
		failures = replay(home, newAppOptions(chainID, stateStorePresent(home)), plan, chainID, pre)
		return nil
	}); recovered != nil {
		fmt.Printf("  FAIL the state could not be carried through the plan: %v\n", recovered)
		return fmt.Errorf("the plan did not qualify on %s: %v", dataDir, recovered)
	}
	if failures > 0 {
		return fmt.Errorf("%d assertions failed over %s", failures, dataDir)
	}
	fmt.Println("every assertion held: the requested plan is qualified on this copied state")
	return nil
}

// applyInChild runs the apply stage as a child process, because an application
// whose plan does not declare every store the state lacks ends the process
// while loading, which would leave the parent's report unfinished.
func applyInChild(dataDir, home, plan, chainID string, pre *preState) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	fmt.Println()
	child := exec.Command(self,
		"-mode", "apply",
		"-data", dataDir,
		"-home", home,
		"-plan", plan,
		"-chain-id", chainID,
		"-pre-height", strconv.FormatInt(pre.Height, 10),
		"-pre-missing", strings.Join(pre.Missing, ","),
	)
	var recorded bytes.Buffer
	child.Stdout = io.MultiWriter(os.Stdout, &recorded)
	child.Stderr = io.MultiWriter(os.Stderr, &recorded)
	err = child.Run()
	if err == nil {
		return nil
	}
	if name, named := missingStoreName(errors.New(recorded.String())); named {
		return fmt.Errorf("plan %s does not declare the store %s the state lacks: the application could not load it", plan, name)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("the plan did not qualify on this state: the apply stage exited %d", exit.ExitCode())
	}
	return err
}
