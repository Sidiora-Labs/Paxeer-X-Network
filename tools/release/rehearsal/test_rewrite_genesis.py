#!/usr/bin/env python3
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
from rewrite_genesis import rewrite

ROOT = Path(__file__).resolve().parents[3]
SCRIPT = Path(__file__).with_name("rewrite_genesis.py")
FIXTURE_DRIVER = r"""
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/config"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/daemon/paxd/cmd"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	svrcmd "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/cmd"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	slashingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/slashing/types"
	"github.com/spf13/viper"
	dbm "github.com/tendermint/tm-db"
)

func exportFixture(home, output string) error {
	doc, err := tmtypes.GenesisDocFromFile(filepath.Join(home, "config", "genesis.json"))
	if err != nil {
		return err
	}
	opts := viper.New()
	opts.SetConfigFile(filepath.Join(home, "config", "app.toml"))
	if err := opts.ReadInConfig(); err != nil {
		return err
	}
	opts.Set("home", home)
	opts.Set("chain-id", doc.ChainID)
	opts.Set(app.FlagSSEnable, false)
	opts.Set("evm.http_enabled", false)
	opts.Set("evm.ws_enabled", false)
	chain := app.New(dbm.NewMemDB(), nil, true, map[int64]bool{}, home, 1, false,
		config.TestConfig(), app.MakeEncodingConfig(), app.GetWasmEnabledProposals(),
		opts, app.EmptyWasmOpts, app.EmptyAppOptions)
	defer chain.Close()
	consensusParams := doc.ConsensusParams.ToProto()
	_, err = chain.InitChain(context.Background(), &abci.RequestInitChain{
		Time: doc.GenesisTime, ChainId: doc.ChainID,
		ConsensusParams: &consensusParams, AppStateBytes: doc.AppState,
	})
	if err != nil {
		return err
	}
	ctx := chain.NewContext(false, tmproto.Header{ChainID: doc.ChainID, Time: doc.GenesisTime})
	chain.SlashingKeeper.IterateValidatorSigningInfos(ctx,
		func(address sdk.ConsAddress, info slashingtypes.ValidatorSigningInfo) bool {
			info.IndexOffset = 5
			info.MissedBlocksCounter = 2
			chain.SlashingKeeper.SetValidatorSigningInfo(ctx, address, info)
			window := chain.SlashingKeeper.SignedBlocksWindow(ctx)
			bitmap := make([]uint64, (window+63)/64)
			bitmap[0] = 5
			chain.SlashingKeeper.SetValidatorMissedBlocks(ctx, address,
				slashingtypes.ValidatorMissedBlockArray{
					Address:      address.String(),
					WindowSize:   window,
					MissedBlocks: bitmap,
				})
			return false
		})
	chain.SetDeliverStateToCommit()
	if _, err = chain.Commit(context.Background()); err != nil {
		return err
	}
	exported, err := chain.ExportAppStateAndValidators(false, nil)
	if err != nil {
		return err
	}
	doc.AppState = exported.AppState
	doc.Validators = exported.Validators
	doc.InitialHeight = exported.Height
	doc.AppHash = nil
	converted := tmtypes.ConsensusParamsFromProto(*exported.ConsensusParams)
	doc.ConsensusParams = &converted
	return doc.SaveAs(output)
}

func main() {
	params.SetAddressPrefixes()
	if len(os.Args) == 4 && os.Args[1] == "export-fixture" {
		if err := exportFixture(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	root, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(root, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
"""


def run(command):
    completed = subprocess.run(
        [str(value) for value in command],
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if completed.returncode:
        raise RuntimeError(
            f"command failed (exit {completed.returncode}): "
            + " ".join(str(value) for value in command)
            + "\n"
            + completed.stdout
            + completed.stderr
        )
    return completed.stdout


class RewriteGenesisTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="rehearsal-genesis-")
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.work = Path(cls.temporary.name)
        driver = cls.work / "fixture.go"
        driver.write_text(FIXTURE_DRIVER, encoding="utf-8")
        cls.paxd = cls.work / "paxd"
        run(["go", "build", "-p", "2", "-o", cls.paxd, driver])
        cls.homes = [cls.work / f"validator-{index}" for index in range(4)]
        addresses = []
        for index, home in enumerate(cls.homes):
            cls.command(home, "init", f"validator-{index}", "--chain-id", "rehearsal")
            key = json.loads(
                cls.command(
                    home,
                    "keys",
                    "add",
                    "validator",
                    "--keyring-backend",
                    "test",
                    "--output",
                    "json",
                )
            )
            addresses.append(key["address"])
        source = cls.homes[0]
        genesis_path = source / "config/genesis.json"
        genesis = json.loads(genesis_path.read_text())
        denom = genesis["app_state"]["staking"]["params"]["bond_denom"]
        for address in addresses:
            cls.command(source, "add-genesis-account", address, "1000000000" + denom)
        genesis_text = genesis_path.read_text()
        gentxs = cls.work / "gentxs"
        gentxs.mkdir()
        for index, home in enumerate(cls.homes):
            (home / "config/genesis.json").write_text(genesis_text)
            cls.command(
                home,
                "gentx",
                "validator",
                "100000000" + denom,
                "--chain-id",
                "rehearsal",
                "--keyring-backend",
                "test",
                "--output-document",
                str(gentxs / f"validator-{index}.json"),
            )
        cls.command(source, "collect-gentxs", "--gentx-dir", str(gentxs))
        cls.command(source, "validate-genesis")
        cls.export_path = cls.work / "exported.json"
        run([cls.paxd, "export-fixture", source, cls.export_path])
        cls.exported = json.loads(cls.export_path.read_text())
        cls.keys = {}
        cls.new_consensus = {}
        cls.old_consensus = {}
        messages = [
            json.loads(path.read_text())["body"]["messages"][0]
            for path in sorted(gentxs.glob("*.json"))
        ]
        for index, message in enumerate(messages):
            operator = message["validator_address"]
            cls.old_consensus[operator] = cls.command(
                cls.homes[index], "tendermint", "show-address"
            ).strip()
            home = cls.work / f"local-{index}"
            cls.command(home, "init", f"local-{index}", "--chain-id", "rehearsal")
            key = json.loads((home / "config/priv_validator_key.json").read_text())
            cls.keys[operator] = {"address": key["address"], "pub_key": key["pub_key"]}
            cls.new_consensus[operator] = cls.command(
                home, "tendermint", "show-address"
            ).strip()
        cls.keys_path = cls.work / "keys.json"
        cls.keys_path.write_text(json.dumps(cls.keys))

    @classmethod
    def command(cls, home, *args):
        return run([cls.paxd, *args, "--home", home])

    def test_four_validators_equal_power(self):
        before = copy.deepcopy(self.exported)
        keys_before = copy.deepcopy(self.keys)
        rewritten = rewrite(self.exported, self.keys)
        self.assertEqual(self.exported, before)
        self.assertEqual(self.keys, keys_before)
        operators = sorted(self.keys)
        self.assertEqual(
            rewritten["app_state"]["staking"]["last_validator_powers"],
            [{"address": operator, "power": "100"} for operator in operators],
        )
        self.assertEqual(rewritten["app_state"]["staking"]["last_total_power"], "400")
        self.assertEqual(len(rewritten["validators"]), 4)
        for row, operator in zip(rewritten["validators"], operators):
            self.assertEqual(row["power"], "100")
            self.assertEqual(row["address"], self.keys[operator]["address"])
            self.assertEqual(row["pub_key"], self.keys[operator]["pub_key"])
        for old, new in zip(
            before["app_state"]["staking"]["validators"],
            rewritten["app_state"]["staking"]["validators"],
        ):
            expected = copy.deepcopy(old)
            expected["consensus_pubkey"]["key"] = self.keys[old["operator_address"]][
                "pub_key"
            ]["value"]
            self.assertEqual(new, expected)
        self.assertEqual(rewritten["app_state"]["bank"]["supply"], [])
        preserved = copy.deepcopy(rewritten)
        preserved["validators"] = before["validators"]
        for name in ("validators", "last_validator_powers", "last_total_power"):
            preserved["app_state"]["staking"][name] = before["app_state"]["staking"][
                name
            ]
        preserved["app_state"]["slashing"] = before["app_state"]["slashing"]
        preserved["app_state"]["bank"]["supply"] = before["app_state"]["bank"]["supply"]
        self.assertEqual(preserved, before)
        output = self.work / "rewritten.json"
        run(
            [
                sys.executable,
                SCRIPT,
                self.export_path,
                "--keys",
                self.keys_path,
                "--output",
                output,
            ]
        )
        self.assertEqual(json.loads(output.read_text()), rewritten)
        self.command(self.homes[0], "validate-genesis", str(output))

    def test_slashing_windows_follow_new_keys(self):
        rewritten = rewrite(self.exported, self.keys)
        old_slashing = self.exported["app_state"]["slashing"]
        new_slashing = rewritten["app_state"]["slashing"]
        old_infos = {entry["address"]: entry for entry in old_slashing["signing_infos"]}
        new_infos = {entry["address"]: entry for entry in new_slashing["signing_infos"]}
        old_bits = {entry["address"]: entry for entry in old_slashing["missed_blocks"]}
        new_bits = {entry["address"]: entry for entry in new_slashing["missed_blocks"]}
        self.assertEqual(set(new_infos), set(self.new_consensus.values()))
        self.assertEqual(set(new_bits), set(self.new_consensus.values()))
        for operator in self.keys:
            old = self.old_consensus[operator]
            new = self.new_consensus[operator]
            expected = copy.deepcopy(old_infos[old])
            expected["address"] = new
            expected["validator_signing_info"]["address"] = new
            expected["validator_signing_info"]["start_height"] = rewritten[
                "initial_height"
            ]
            self.assertEqual(new_infos[new], expected)
            expected = copy.deepcopy(old_bits[old])
            expected["address"] = new
            self.assertEqual(new_bits[new], expected)
            self.assertEqual(new_bits[new]["missed_blocks"][0], "5")
            self.assertEqual(set(new_bits[new]["missed_blocks"][1:]), {"0"})
            self.assertEqual(
                new_infos[new]["validator_signing_info"]["missed_blocks_counter"], "2"
            )
        self.assertEqual(new_slashing["params"], old_slashing["params"])

    def test_refuses_unequal_power_without_mutating_export(self):
        exported = copy.deepcopy(self.exported)
        exported["app_state"]["staking"]["last_validator_powers"][0]["power"] = "99"
        before = copy.deepcopy(exported)
        with self.assertRaisesRegex(
            ValueError, "^bonded validators must have equal voting power$"
        ):
            rewrite(exported, self.keys)
        self.assertEqual(exported, before)

    def test_refuses_wrong_validator_count_and_key_mapping(self):
        exported = copy.deepcopy(self.exported)
        exported["app_state"]["staking"]["validators"].pop()
        with self.assertRaisesRegex(
            ValueError, "^export must contain exactly four bonded validators$"
        ):
            rewrite(exported, self.keys)
        keys = copy.deepcopy(self.keys)
        keys.pop(next(iter(keys)))
        with self.assertRaisesRegex(
            ValueError, "^keys must name exactly the bonded operators$"
        ):
            rewrite(self.exported, keys)

    def test_refuses_duplicate_mismatched_and_malformed_keys(self):
        operators = list(self.keys)
        keys = copy.deepcopy(self.keys)
        keys[operators[1]] = keys[operators[0]]
        with self.assertRaisesRegex(
            ValueError, "^replacement public keys must be distinct$"
        ):
            rewrite(self.exported, keys)
        keys = copy.deepcopy(self.keys)
        keys[operators[0]]["address"] = keys[operators[1]]["address"]
        with self.assertRaisesRegex(
            ValueError, "^replacement address does not match public key$"
        ):
            rewrite(self.exported, keys)
        keys = copy.deepcopy(self.keys)
        keys[operators[0]]["pub_key"]["value"] = "invalid!"
        with self.assertRaisesRegex(
            ValueError, "^public key must be canonical base64$"
        ):
            rewrite(self.exported, keys)


if __name__ == "__main__":
    unittest.main(verbosity=2)
