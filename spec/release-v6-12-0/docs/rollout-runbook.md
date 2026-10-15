# Paxeer X Network v6.12.0 rollout runbook

This runbook applies upgrade plan `v6.12.0` to every validator and full node of the Paxeer X Network at one halt height. It is executed by operators, step by step, in the order below. Each step names the host role it runs on, the exact commands in generic form, and the verify command whose exit status 0 is the condition for the next step. The binding rules are in contracts/release.md; code citations are at revision 211acbc6c of github.com/Sidiora-Labs/Paxeer-X-Network. Scripts under `tools/release/` that do not exist at that revision are created by the tasks of feature 8 before this runbook is executed.

Host roles: **validator host** (runs one or more validator homes and may also run a full node; all units on one host share one installed `paxd`), **full-node host** (runs one full node; one of them is the archive node), **edge** (proxies the public endpoints), **build host** (builds the release), **operations host** (runs fleet automation and the verification script).

## Operator inputs

Fill these fields in the access-controlled copy of this runbook before step 1. The public copy keeps them blank.

| Field | Value |
|---|---|
| Halt height H | |
| Upgrade height H1 (= H + 1) | |
| Maintenance window | |
| Public notice text | |
| Build host | |
| Release id RELEASE_ID | |

## Local values

Every command reads its host-specific values from an untracked env file, `.env.rollout-v6120`, kept in access-controlled storage and copied to each host for the window. It follows the local-values pattern of docs/runbooks/paxeer-x-fork.md:19-44. Keys:

```sh
UPGRADE_NAME=v6.12.0
RELEASE_TAG=paxeer-network/v6.12.0
HALT_HEIGHT=            # H, a literal number
UPGRADE_HEIGHT=         # H1 = H + 1, a literal number
RELEASE_ID=             # suffix of the retained previous binary
RELEASE_SHA256=         # paxd-v6.12.0-linux-amd64 line of SHA256SUMS
BIN_DIR=                # directory of the installed paxd on this host
SNAPSHOT_DIR=           # directory for validator data snapshots on this host
VALIDATOR_HOMES=        # space-separated validator homes on this host (empty on a full-node host)
FULLNODE_HOME=          # full-node home on this host (empty when none)
VALIDATOR_UNITS=        # service units of the validator homes, same order
FULLNODE_UNIT=          # service unit of the full node
SIDECAR_UNITS=          # price sidecar units on this host
NODE_RPC=               # CometBFT RPC of this host's node
NODE_EVM_RPC=           # JSON-RPC of this host's node
INVENTORY=              # operations host: file listing every node by role
AUTOMATION_UNITS=       # operations host: watchdog and resync timers
```

On every host, the commands below assume `set -a; . ./.env.rollout-v6120; set +a` has been run in the shell.

## 1. Preflight (operations host)

1. Confirm the inventory lists every validator home, full node, the archive node and the edge; a node missing from the inventory is not upgraded.
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase preflight --receipt tools/release/rollout/v6.12.0/preflight.json
   ```
   Verify: exit 0; the receipt reports `nodes_passed` equal to `nodes_checked`, every node on the previous release version and below H.
2. Confirm the release published by the workflow matches the build host's build:
   ```sh
   bash tools/release/verify-release.sh --tag paxeer-network/v6.12.0 --dist dist/paxeer-network/v6.12.0
   ```
   Verify: exit 0; `SHA256SUMS` of the release equals the local build and `upgrade-info.json` names plan `v6.12.0` with the same sha256.
3. Confirm the rehearsal receipt `tools/release/rollout/v6.12.0/rehearsal.json` carries `"result": "pass"` and the same `new_bin_sha256` as `RELEASE_SHA256`.
   ```sh
   jq -e --arg s "$RELEASE_SHA256" '.result == "pass" and .new_bin_sha256 == $s' tools/release/rollout/v6.12.0/rehearsal.json
   ```
4. Publish the public notice text with the maintenance window.

## 2. Stage the binary (every validator host and full-node host)

1. Copy the release binary next to the installed one, check it, and keep the previous binary:
   ```sh
   install -m 0755 paxd-v6.12.0-linux-amd64 "$BIN_DIR/paxd.v6.12.0"
   echo "$RELEASE_SHA256  $BIN_DIR/paxd.v6.12.0" | sha256sum --check --strict
   [ -e "$BIN_DIR/paxd.pre-$RELEASE_ID" ] || cp -p "$BIN_DIR/paxd" "$BIN_DIR/paxd.pre-$RELEASE_ID"
   "$BIN_DIR/paxd.v6.12.0" version
   ```
2. Verify (operations host):
   ```sh
   bash tools/release/stage.sh --env-file .env.rollout-v6120 --verify
   ```
   Verify: exit 0; every host reports the staged sha256 equal to `RELEASE_SHA256`, a `paxd.pre-<RELEASE_ID>` file, and `paxd.v6.12.0 version` printing `v6.12.0`.

## 3. Pause fleet automation (operations host)

1. Stop every timer that restarts, reclones or replaces binaries:
   ```sh
   for u in $AUTOMATION_UNITS; do systemctl stop "$u"; done
   for u in $AUTOMATION_UNITS; do systemctl is-active "$u" && exit 1; done; echo paused
   ```
   Verify: the second line prints `paused`.
2. Optionally set self-remediation off on each node for the window, keeping a backup of the file (the key is `blocks-behind-threshold`, consensus/config/toml.go:627):
   ```sh
   cp -p "$HOME_DIR/config/config.toml" "$HOME_DIR/config/config.toml.pre-$RELEASE_ID"
   sed -i 's/^blocks-behind-threshold = .*/blocks-behind-threshold = 0/' "$HOME_DIR/config/config.toml"
   ```

## 4. Freeze the validators at H (every validator host)

Freeze one validator home at a time, so at least three of the four validators stay online. `halt-height` is the app.toml key of sdk/server/config/toml.go:42; at H the node commits the block and stops itself (sdk/baseapp/abci.go:310, sdk/baseapp/abci.go:322).

1. For each validator home and its unit, in order:
   ```sh
   sed -i "s/^halt-height = .*/halt-height = $HALT_HEIGHT/" "$HOME_DIR/config/app.toml"
   grep -x "halt-height = $HALT_HEIGHT" "$HOME_DIR/config/app.toml"
   systemctl restart "$UNIT"
   curl -s "$NODE_RPC/status" | jq -r .result.sync_info.latest_block_height
   ```
   Verify: the grep prints the line, and the height printed by the last command rises on two reads a few blocks apart before the next home is frozen.
2. Wait for H. On each validator host:
   ```sh
   journalctl -u "$UNIT" --since "-1h" | grep -F "halting node per configuration" | grep -F "\"height\": $HALT_HEIGHT"
   pgrep -a -x paxd | grep -F -- "--home $HOME_DIR" && exit 1; echo halted
   ```
   Verify: every validator home prints the halt line with height H and `halted`. The process check uses the exact process name; a pattern match against the full command line is not used.
3. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase frozen --receipt tools/release/rollout/v6.12.0/frozen.json
   ```
   Verify: exit 0; every validator reports last height H and every full node reports height H.

## 5. Snapshot the validator data (every validator host)

1. For each validator home:
   ```sh
   tar --zstd -cf "$SNAPSHOT_DIR/$(basename "$HOME_DIR")-pre-v6.12.0.tar.zst" --exclude=priv_validator_state.json -C "$HOME_DIR" data
   tar --zstd -tf "$SNAPSHOT_DIR/$(basename "$HOME_DIR")-pre-v6.12.0.tar.zst" >/dev/null && echo snapshot-ok
   ```
2. Verify (operations host):
   ```sh
   bash tools/release/snapshot.sh --env-file .env.rollout-v6120 --verify
   ```
   Verify: exit 0; one readable archive per validator home, none containing `priv_validator_state.json`.

## 6. Swap the full nodes (every full-node host, and the full node of a validator host)

Every full node is on v6.12.0 before any validator restarts, so no full node executes H1 on the previous binary. On a validator host the shared binary is swapped only after every validator home on that host has halted (step 4.2).

1. On each full node:
   ```sh
   systemctl stop "$FULLNODE_UNIT"
   pgrep -a -x paxd | grep -F -- "--home $FULLNODE_HOME" && exit 1
   printf '{"name":"v6.12.0","height":%s}' "$UPGRADE_HEIGHT" > "$FULLNODE_HOME/data/upgrade-info.json"
   chmod 0600 "$FULLNODE_HOME/data/upgrade-info.json"
   cp -p "$BIN_DIR/paxd.v6.12.0" "$BIN_DIR/paxd.new" && mv -f "$BIN_DIR/paxd.new" "$BIN_DIR/paxd"
   echo "$RELEASE_SHA256  $BIN_DIR/paxd" | sha256sum --check --strict
   systemctl start "$FULLNODE_UNIT"
   ```
   The upgrade file holds the two fields of sdk/store/types/store.go:56-59 at the path of sdk/x/upgrade/keeper/keeper.go:434-441; the node applies the plan only in the block whose height equals the file height (node/upgrades.go:594-596).
2. Verify (operations host):
   ```sh
   bash tools/release/swap.sh --env-file .env.rollout-v6120 --role full --verify
   ```
   Verify: exit 0; every full node runs `paxd` with sha256 `RELEASE_SHA256`, holds the upgrade file for H1 and waits at height H.

## 7. Swap and start the validators (every validator host)

1. For each validator home and its unit:
   ```sh
   printf '{"name":"v6.12.0","height":%s}' "$UPGRADE_HEIGHT" > "$HOME_DIR/data/upgrade-info.json"
   chmod 0600 "$HOME_DIR/data/upgrade-info.json"
   sed -i 's/^halt-height = .*/halt-height = 0/' "$HOME_DIR/config/app.toml"
   ```
2. Once per host, after step 1 for every home on it:
   ```sh
   cp -p "$BIN_DIR/paxd.v6.12.0" "$BIN_DIR/paxd.new" && mv -f "$BIN_DIR/paxd.new" "$BIN_DIR/paxd"
   echo "$RELEASE_SHA256  $BIN_DIR/paxd" | sha256sum --check --strict
   for u in $VALIDATOR_UNITS; do systemctl start "$u"; done
   journalctl -u "$UNIT" --since "-10m" | grep -F '"applying upgrade"'
   ```
   Verify: each validator log carries `applying upgrade` with name `v6.12.0` (node/upgrades.go:602) once at least three of the four validators run.
3. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase validators --receipt tools/release/rollout/v6.12.0/validators.json
   ```
   Verify: exit 0; four validators on version `v6.12.0`, `q upgrade applied v6.12.0` at H1, one block hash at H1 on every node.

## 8. Start the price sidecars (every validator host)

1. ```sh
   for u in $SIDECAR_UNITS; do systemctl start "$u"; systemctl is-active "$u"; done
   ```
2. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase oracle --receipt tools/release/rollout/v6.12.0/oracle.json
   ```
   Verify: exit 0; every listed market carries a price inside the freshness window of contracts/pricefeed.md, and the feed-key registry holds one registered key per validator.

## 9. Lagging nodes (any host whose node was below H at step 4.3)

A node that had not reached H keeps the previous binary until it has executed H, then is swapped by hand.

1. ```sh
   sed -i "s/^halt-height = .*/halt-height = $HALT_HEIGHT/" "$HOME_DIR/config/app.toml"
   systemctl restart "$UNIT"
   ```
2. After the halt line for H appears (step 4.2 commands), repeat step 6.1 for that node and set `halt-height = 0` before the start.
3. The archive node is swapped this way and never recloned. Data moves and age-based deletion are never run together; set-aside data is removed only by an explicit operator decision.
4. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase full --receipt tools/release/rollout/v6.12.0/full.json
   ```
   Verify: exit 0; every node of the inventory on `v6.12.0` and past H1.

## 10. Resume fleet automation (operations host)

1. Restore any config.toml changed in step 3.2, restart that node, then:
   ```sh
   for u in $AUTOMATION_UNITS; do systemctl start "$u"; systemctl is-active "$u"; done
   ```
   Verify: every unit prints `active`.

## 11. Post-rollout verification (operations host)

1. ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase post --receipt tools/release/rollout/v6.12.0/post-rollout.json
   ```
   Verify: exit 0; on every node: version `v6.12.0` in the ABCI info (sdk/baseapp/abci.go:98), plan applied at H1, one block hash at H1, every address of contracts/address-table.md served, the launchpad pool count equal to the rehearsal count, eighteen perpetual markets, a fresh price for every listed market.
2. Publish the completion notice. Keep `paxd.pre-<RELEASE_ID>` and the snapshots until the next release.

## 12. Rollback

Decide with the verify output of step 7.3. Run on every validator host and full-node host.

1. **H1 not committed** (fewer than two thirds of the voting power has signed H1):
   ```sh
   for u in $VALIDATOR_UNITS $FULLNODE_UNIT; do systemctl stop "$u"; done
   cp -p "$BIN_DIR/paxd.pre-$RELEASE_ID" "$BIN_DIR/paxd.new" && mv -f "$BIN_DIR/paxd.new" "$BIN_DIR/paxd"
   for h in $VALIDATOR_HOMES $FULLNODE_HOME; do rm -f "$h/data/upgrade-info.json"; sed -i 's/^halt-height = .*/halt-height = 0/' "$h/config/app.toml"; done
   for u in $VALIDATOR_UNITS $FULLNODE_UNIT; do systemctl start "$u"; done
   ```
2. **H1 committed**: restore every validator home from its step-5 snapshot, keeping the current `priv_validator_state.json`, then run the commands of 12.1:
   ```sh
   systemctl stop "$UNIT"
   cp -p "$HOME_DIR/data/priv_validator_state.json" "$SNAPSHOT_DIR/$(basename "$HOME_DIR")-pvs.json"
   mv "$HOME_DIR/data" "$HOME_DIR/data.v6.12.0-$RELEASE_ID"
   tar --zstd -xf "$SNAPSHOT_DIR/$(basename "$HOME_DIR")-pre-v6.12.0.tar.zst" -C "$HOME_DIR"
   cp -p "$SNAPSHOT_DIR/$(basename "$HOME_DIR")-pvs.json" "$HOME_DIR/data/priv_validator_state.json"
   ```
   Full nodes are restored from a restored node's data. A stale validator home is never started on another host.
3. Verify (operations host):
   ```sh
   bash tools/release/rollback.sh --env-file .env.rollout-v6120 --verify
   ```
   Verify: exit 0; every node on the previous binary's sha256, no upgrade file on any home, and the chain height rising past H.
4. Resume fleet automation (step 10) and publish a notice.
