# Paxeer X Network v6.12.0 rollout runbook

This runbook applies upgrade plan `v6.12.0` to every validator and full node of the Paxeer X Network at one halt height, installs the price sidecars and cuts over the data services and the application. It is executed by operators, step by step, in the order below. Each step names the host role it runs on, the exact commands in generic form, and the verify command whose exit status 0 is the condition for the next step. The binding rules are in contracts/release.md; code citations are at revision 211acbc6c of github.com/Sidiora-Labs/Paxeer-X-Network unless prefixed `Hyperpax:` (github.com/paxlabs-inc/Hyperpax). Scripts under `tools/release/` that do not exist at that revision are created by the tasks of feature 8 before this runbook is executed. Every step that writes a receipt writes it under `$RECEIPT_DIR` (`tools/release/rollout/v6.12.0`) with the receipt key set of contracts/release.md#post-rollout-verification.

Host roles: **validator host** (runs one or more validator homes and may also run a full node; all units on one host share one installed `paxd`), **full-node host** (runs one full node; one of them is the archive node), **edge** (proxies the public endpoints), **build host** (builds the release), **operations host** (runs fleet automation, the data services deploys and the verification script).

## Operator inputs

Fill these fields in the access-controlled copy of this runbook before step 0. The public copy keeps them blank. The halt height is chosen after implementation (decision.11).

| Field | Value |
|---|---|
| Halt height H | |
| Upgrade height H1 (= H + 1) | |
| Maintenance window | |
| Public notice text | |
| Build host | |
| Release id RELEASE_ID | |

## Local values

Every command reads its values from an untracked env file, `.env.rollout-v6120`, a filled copy of `tools/release/env/rollout.env.example` kept in access-controlled storage and copied to each host for the window. It follows the local-values pattern of docs/runbooks/paxeer-x-fork.md:19-44. List values are space-separated; the lists of homes, units and operator keys of one host are in the same order. Keys:

```sh
RELEASE_ID=             # suffix of the retained previous binary
RELEASE_TAG=            # paxeer-network/v6.12.0
RELEASE_SHA256=         # paxd-v6.12.0-linux-amd64 line of SHA256SUMS
SIDECAR_SHA256=         # price-sidecar-v6.12.0-linux-amd64 line of SHA256SUMS
HALT_HEIGHT=            # H, a literal number
CHAIN_ID=               # Cosmos chain id
KEYRING_BACKEND=        # keyring backend of the operator keys
BIN_DIR=                # directory of the installed paxd on this host
VALIDATOR_HOSTS=        # operations host: every validator host
VALIDATOR_HOMES=        # validator homes on this host (empty on a full-node host)
VALIDATOR_UNITS=        # service units of VALIDATOR_HOMES
OPERATOR_KEY_NAMES=     # operator key names of VALIDATOR_HOMES
FULLNODE_HOSTS=         # operations host: every full-node host
FULLNODE_HOMES=         # full-node homes on this host (empty when none)
FULLNODE_UNITS=         # service units of FULLNODE_HOMES
ARCHIVE_HOST=           # operations host: the archive node host
EDGE_HOST=              # operations host: the edge
OPS_HOST=               # the operations host
BUILD_HOST=             # the build host
SNAPSHOT_DIR=           # directory for the home snapshots on this host
CREDENTIAL_DIR=         # access-controlled credential directory of the sidecar on this host
AUTOMATION_UNITS=       # operations host: watchdog and resync timers
OLD_FEEDER_UNITS=       # layerx-oracle-feeder units on this host
SIDECAR_UNITS=          # price-sidecar units on this host, one per validator home
CHAINFLOW_KEY_FILE=     # Chainflow feed key file in access-controlled storage
CONSENSUS_RPC=          # CometBFT RPC of this host's node
EVM_RPC=                # JSON-RPC of this host's node
DATA_SERVICE_URLS=      # operations host: base URLs of the four data services
APP_URL=                # operations host: base URL of the application
APP_BUILD_ID=           # build id of the application release build
INVENTORY_FILE=         # operations host: file listing every node by role
POOL_COUNT=             # operations host: pool count the markets, full and post checks expect when no --rehearsal receipt is given
RECEIPT_DIR=            # tools/release/rollout/v6.12.0
```

On every host, the commands below assume this has been run in the shell:

```sh
set -a; . ./.env.rollout-v6120; set +a
read -ra V_HOMES <<< "$VALIDATOR_HOMES"; read -ra V_UNITS <<< "$VALIDATOR_UNITS"; read -ra V_KEYS <<< "$OPERATOR_KEY_NAMES"
read -ra F_HOMES <<< "$FULLNODE_HOMES"; read -ra F_UNITS <<< "$FULLNODE_UNITS"
H1=$((HALT_HEIGHT + 1))
```

`tools/release/stage.sh`, `swap.sh`, `snapshot.sh`, `rollback.sh` and `fleet_check.py` refuse to run while any key they read is empty.

## 0. Approve the inputs (operations host)

1. Fill the operator input table of the access-controlled copy and every key of `.env.rollout-v6120`, and obtain the approval of the filled table.
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase inputs --receipt "$RECEIPT_DIR/halt-height.json"
   ```
   Verify: exit 0; the receipt carries `inputs_sha256`, `inputs_approved` true and `result` pass, and no height.

## 1. Preflight (operations host)

1. Confirm `$INVENTORY_FILE` lists every validator home, full node, the archive node and the edge; a node missing from the inventory is not upgraded.
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase preflight --receipt "$RECEIPT_DIR/preflight.json"
   ```
   Verify: exit 0; the receipt reports `nodes_passed` equal to `nodes_checked`, every node on the previous release version and below H.
2. Confirm the release published by the workflow matches the build host's build:
   ```sh
   bash tools/release/verify-release.sh --tag "$RELEASE_TAG" --dist dist/paxeer-network/v6.12.0
   ```
   Verify: exit 0 with `verify-release: 9 assets match`; `upgrade-info.json` names plan `v6.12.0` with the sha256 `RELEASE_SHA256`.
3. Confirm the rehearsal receipt:
   ```sh
   jq -e --arg s "$RELEASE_SHA256" '.result == "pass" and .rollback_rehearsed == true and .new_bin_sha256 == $s' "$RECEIPT_DIR/rehearsal.json"
   ```
4. Publish the public notice text with the maintenance window.

## 2. Stage the binaries (every validator host and full-node host)

1. Copy `paxd-v6.12.0-linux-amd64`, `price-sidecar-v6.12.0-linux-amd64` and `upgrade-info.json` of the release to the host, then:
   ```sh
   bash tools/release/stage.sh --env-file .env.rollout-v6120 --local
   ```
   The script installs `$BIN_DIR/paxd.v6.12.0`, checks it with `sha256sum --check --strict` against `RELEASE_SHA256`, runs `ldd` on it and refuses any `not found` line, creates `$BIN_DIR/paxd.pre-$RELEASE_ID` only when absent, and installs `$BIN_DIR/price-sidecar` checked against `SIDECAR_SHA256`.
   ```sh
   "$BIN_DIR/paxd.v6.12.0" version
   ```
   Verify: prints `v6.12.0` (node/app.go:593).
2. Verify (operations host):
   ```sh
   bash tools/release/stage.sh --env-file .env.rollout-v6120 --verify --receipt "$RECEIPT_DIR/stage.json"
   ```
   Verify: exit 0; `hosts_staged` equals the hosts of the inventory.

## 3. Retire the old feeder and pause fleet automation

1. On every host, stop and disable the earlier feeder units, which send `pushOracle` to 0x1015 (platform/hosted/feeder/src/lib.rs:62):
   ```sh
   for u in $OLD_FEEDER_UNITS; do systemctl disable --now "$u"; done
   for u in $OLD_FEEDER_UNITS; do systemctl is-active --quiet "$u" && exit 1; done; echo feeder-stopped
   ```
2. On the operations host, stop every timer that restarts, reclones or replaces binaries:
   ```sh
   for u in $AUTOMATION_UNITS; do systemctl stop "$u"; done
   bash tools/release/stage.sh --env-file .env.rollout-v6120 --automation-status
   ```
   Verify: exit 0; no automation unit and no old feeder unit active on any host.
3. Optionally set self-remediation off on each node for the window, keeping a backup of the file (the key is `blocks-behind-threshold`, consensus/config/toml.go:627):
   ```sh
   for h in "${V_HOMES[@]}" "${F_HOMES[@]}"; do
     cp -p "$h/config/config.toml" "$h/config/config.toml.pre-$RELEASE_ID"
     sed -i 's/^blocks-behind-threshold = .*/blocks-behind-threshold = 0/' "$h/config/config.toml"
   done
   ```
   The new value takes effect when the node restarts in step 4 or step 6.

## 4. Freeze the validators at H (every validator host)

Freeze one validator home at a time, so at least three of the four validators stay online. `halt-height` is the app.toml key of sdk/server/config/toml.go:42; at H the node commits the block and stops itself (sdk/baseapp/abci.go:310, sdk/baseapp/abci.go:322).

1. For each index `i` of `V_HOMES`, one at a time:
   ```sh
   sed -i "s/^halt-height = .*/halt-height = $HALT_HEIGHT/" "${V_HOMES[$i]}/config/app.toml"
   grep -x "halt-height = $HALT_HEIGHT" "${V_HOMES[$i]}/config/app.toml"
   systemctl restart "${V_UNITS[$i]}"
   curl -s "$CONSENSUS_RPC/status" | jq -r .sync_info.latest_block_height
   ```
   Verify: the grep prints the line, and the height printed by the last command rises on two reads a few blocks apart before the next home is frozen.
2. Wait for H. The halt line is a key=value log line (sdk/baseapp/abci.go:360). On each validator host:
   ```sh
   for i in "${!V_HOMES[@]}"; do
     journalctl -u "${V_UNITS[$i]}" --since "-1h" | grep -F "halting node per configuration" | grep -F "height=$HALT_HEIGHT" || exit 1
     pgrep -a -x paxd | grep -F -- "--home ${V_HOMES[$i]}" && exit 1
   done; echo halted
   ```
   Verify: every validator home prints the halt line with `height=H`, and `halted` is printed. The process check uses the exact process name; a pattern match against the full command line is not used.
3. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase frozen --receipt "$RECEIPT_DIR/frozen.json"
   ```
   Verify: exit 0; every validator reports last height H and every full node reports height H.

## 5. Snapshot every home at H (every validator host and full-node host)

1. Stop every full node once its status reports height H:
   ```sh
   curl -s "$CONSENSUS_RPC/status" | jq -e --argjson h "$HALT_HEIGHT" '(.sync_info.latest_block_height | tonumber) == $h'
   for u in "${F_UNITS[@]}"; do systemctl stop "$u"; done
   for h in "${F_HOMES[@]}"; do pgrep -a -x paxd | grep -F -- "--home $h" && exit 1; done; echo stopped
   ```
2. Archive the whole data directory of every validator and full-node home, `priv_validator_state.json` included:
   ```sh
   bash tools/release/snapshot.sh --env-file .env.rollout-v6120
   ```
   The script writes `$SNAPSHOT_DIR/<home>-pre-v6.12.0.tar.zst` with `tar --zstd -cf ... -C <home> data` for each home of `VALIDATOR_HOMES` and `FULLNODE_HOMES`.
3. Verify (operations host):
   ```sh
   bash tools/release/snapshot.sh --env-file .env.rollout-v6120 --verify --receipt "$RECEIPT_DIR/snapshot.json"
   bash tools/release/rollback.sh --env-file .env.rollout-v6120 --readiness --receipt "$RECEIPT_DIR/rollback.json"
   ```
   Verify: both exit 0; `homes_archived` equals the validator and full-node homes of the inventory, every archive holds `data/priv_validator_state.json` and the application store, and `readiness` is true.

## 6. Swap the full nodes (every full-node host, and the full node of a validator host)

Every full node is on v6.12.0 before any validator restarts, so no full node executes H1 on the previous binary. On a validator host the shared binary is swapped only after every validator home on that host has halted (step 4.2).

1. On each host with a full node:
   ```sh
   bash tools/release/swap.sh --env-file .env.rollout-v6120 --role full
   ```
   For each full-node home the script writes `data/upgrade-info.json` with `python3 tools/release/upgrade_info.py node --halt-height "$HALT_HEIGHT" --release-doc upgrade-info.json --sha256 "$RELEASE_SHA256"` (mode 0600), swaps `$BIN_DIR/paxd` with `cp -p` to `paxd.new` and `mv -f`, checks the sha256 and `ldd`, and starts the unit. The upgrade file holds the two fields of sdk/store/types/store.go:56-59 at the path of sdk/x/upgrade/keeper/keeper.go:434-441; the node applies the plan only in the block whose height equals the file height (node/upgrades.go:594-596).
2. Verify (operations host):
   ```sh
   bash tools/release/swap.sh --env-file .env.rollout-v6120 --role full --verify --receipt "$RECEIPT_DIR/swap-full.json"
   ```
   Verify: exit 0; every full node runs `paxd` with sha256 `RELEASE_SHA256`, holds the upgrade file for H1 and waits at height H.

## 7. Swap and start the validators (every validator host)

1. ```sh
   bash tools/release/swap.sh --env-file .env.rollout-v6120 --role validator
   ```
   The script refuses while any validator home of the host still runs, writes the upgrade file for H1 into each validator home, sets `halt-height = 0` in each app.toml, swaps the shared binary once with the sha256 and `ldd` checks, and starts every unit of `VALIDATOR_UNITS`.
2. ```sh
   for u in "${V_UNITS[@]}"; do journalctl -u "$u" --since "-10m" | grep -F "applying upgrade" | grep -F "name=v6.12.0" || exit 1; done; echo applied
   ```
   Verify: each validator log carries `applying upgrade` with `name=v6.12.0` (node/upgrades.go:602) once at least three of the four validators run.
3. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase validators --receipt "$RECEIPT_DIR/validators.json"
   ```
   Verify: exit 0; four validators on version `v6.12.0`, `q upgrade applied v6.12.0` at H1, one block hash at H1 on every node. On a failure go to step 13.

## 8. Register the feed keys and start the price sidecars (every validator host)

The v6.12.0 handler registers no feed key (contracts/pricefeed.md, Activation item 2); each operator registers its own feed key after H1 with the steps below.

1. For each validator home, generate its feed key into the credential directory and associate the operator EVM address (modules/evm/client/cli/tx.go:81):
   ```sh
   for i in "${!V_HOMES[@]}"; do
     python3 tools/release/feed_key.py new --out "$CREDENTIAL_DIR/${V_UNITS[$i]}/feed.key" > "$CREDENTIAL_DIR/${V_UNITS[$i]}/feed.pub"
     "$BIN_DIR/paxd" tx evm associate-address --evm-rpc="$EVM_RPC" --from="${V_KEYS[$i]}" --home "${V_HOMES[$i]}" --keyring-backend "$KEYRING_BACKEND" --chain-id "$CHAIN_ID"
   done
   ```
2. Register each key with 0x1020 (modules/evm/client/cli/tx.go:304); `feed_key.py payload` prints the call data of `registerFeedKey(bytes pubKey)` (contracts/pricefeed.md, Precompile interface) over the 33-byte compressed public key:
   ```sh
   for i in "${!V_HOMES[@]}"; do
     payload=$(python3 tools/release/feed_key.py payload "$(cat "$CREDENTIAL_DIR/${V_UNITS[$i]}/feed.pub")")
     "$BIN_DIR/paxd" tx evm call-contract 0x0000000000000000000000000000000000001020 "$payload" --from="${V_KEYS[$i]}" --evm-rpc="$EVM_RPC" --home "${V_HOMES[$i]}" --keyring-backend "$KEYRING_BACKEND" --chain-id "$CHAIN_ID"
   done
   ```
   Verify: `getFeedKey` of each validator returns the key and `getValidatorByFeedAccount` of its feed account returns the validator (contracts/pricefeed.md, Activation item 4).
3. Install and start the sidecars: copy `tools/price-sidecar/price-sidecar.service` once per unit of `SIDECAR_UNITS`, fill a copy of `tools/price-sidecar/config.template.json` per unit with the Chainflow feed base address, `CHAIN_ID`, the validator and its feed account, and place `feed.key` and `$CHAINFLOW_KEY_FILE` as `chainflow.key` in `$CREDENTIAL_DIR/<unit>/`, which the unit loads with `LoadCredential`:
   ```sh
   systemctl daemon-reload
   for u in $SIDECAR_UNITS; do systemctl enable --now "$u"; systemctl is-active "$u"; done
   ```
   Verify: every unit prints `active` and its `/readyz` answers 200 (contracts/pricefeed.md, Sidecar item 7).
4. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase oracle --receipt "$RECEIPT_DIR/oracle.json"
   ```
   Verify: exit 0; `getPrice(m).fresh` is true for every market of contracts/pricefeed.md, fresh meaning `block_time_ms - timestampMs <= max_price_age_ms` (contracts/pricefeed.md, Freshness and read API item 2), and `feed_keys` equals the bonded validators. On a failure go to step 13.

## 9. Cut over the data services and the application (operations host)

1. Deploy `perps-data-indexer` with `PERPS_START_HEIGHT` = H1 (contracts/data-services.md), wait for its `/healthz/ready` 200, then deploy `perps-gateway`, `perps-explorer` and `perps-portal` in that order, each only after the previous answers `/healthz/ready` 200. Charts and market data come from Chainflow; the gateway streams the venue's own fills, book and account state.
2. Deploy the Hyperpax release build with `Hyperpax:deploy/scripts/deploy.sh`, `NEXT_PUBLIC_RELEASE_SPOT_OPEN` empty and the build id `APP_BUILD_ID`; the script's health check reads `/api/build-id` (Hyperpax:deploy/scripts/deploy.sh:147).
3. Verify:
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase services --receipt "$RECEIPT_DIR/services.json"
   ```
   Verify: exit 0; every URL of `DATA_SERVICE_URLS` answers `/healthz/ready` 200 and `$APP_URL/api/build-id` returns `APP_BUILD_ID`. On a failure go to step 13.

## 10. Lagging nodes (any host whose node was below H at step 4.3)

A node that had not reached H keeps the previous binary until it has executed H, then is archived and swapped.

1. For that home and its unit:
   ```sh
   sed -i "s/^halt-height = .*/halt-height = $HALT_HEIGHT/" "<home>/config/app.toml"
   systemctl restart "<unit>"
   ```
2. After its halt line for H appears (the commands of step 4.2), run step 5.2 and step 6.1 for that host; `swap.sh` sets `halt-height = 0` before the start.
3. The archive node is swapped this way and never recloned. Data moves and age-based deletion are never run together; set-aside data is removed only by an explicit operator decision.
4. Verify (operations host):
   ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase full --receipt "$RECEIPT_DIR/full.json"
   ```
   Verify: exit 0; every node of the inventory on `v6.12.0` and past H1.

## 11. Resume fleet automation (operations host)

1. Restore every config.toml backed up in step 3.3 from `config.toml.pre-$RELEASE_ID` and restart that node, then:
   ```sh
   for u in $AUTOMATION_UNITS; do systemctl start "$u"; done
   bash tools/release/stage.sh --env-file .env.rollout-v6120 --automation-status --receipt "$RECEIPT_DIR/automation.json"
   ```
   Verify: exit 0; `units_active` equals the automation units, and the old feeder units stay disabled.

## 12. Post-rollout verification and the spot route (operations host)

1. ```sh
   python3 tools/release/fleet_check.py --env-file .env.rollout-v6120 --phase post --receipt "$RECEIPT_DIR/post-rollout.json"
   ```
   Verify: exit 0; on every node: version `v6.12.0` in the ABCI info (sdk/baseapp/abci.go:98), plan applied at H1, one block hash at H1, every served address of contracts/address-table.md answering and 0x1026 and 0x1027 without code, the launchpad pool count equal to the rehearsal count, eighteen markets at 0x1015 and 0x1020, a fresh price for every market, one feed key per validator, the uhpx and SID pointers through 0x100A and USDL `decimals()` 6. On a failure go to step 13.
2. Deploy the Hyperpax spot-open build with `NEXT_PUBLIC_RELEASE_SPOT_OPEN=1` through `Hyperpax:deploy/scripts/deploy.sh` and check `/api/build-id` again.
3. Publish the completion notice. Keep `paxd.pre-<RELEASE_ID>` and the snapshots until the next release.

## 13. Rollback

Rollback restores the whole fleet in one window to the state at H on the previous binary (contracts/release.md#rollback). It runs when step 7.3, 8.4, 9.3 or 12.1 fails.

1. Halt every node: on every validator host and full-node host stop every unit.
   ```sh
   for u in $SIDECAR_UNITS "${V_UNITS[@]}" "${F_UNITS[@]}"; do systemctl stop "$u"; done
   for h in "${V_HOMES[@]}" "${F_HOMES[@]}"; do pgrep -a -x paxd | grep -F -- "--home $h" && exit 1; done; echo halted
   ```
2. On every validator host and full-node host:
   ```sh
   bash tools/release/rollback.sh --env-file .env.rollout-v6120 --execute
   ```
   For each home the script moves `data` to `data.v6.12.0-$RELEASE_ID`, extracts `$SNAPSHOT_DIR/<home>-pre-v6.12.0.tar.zst`, which carries the `priv_validator_state.json` recorded at H, restores `$BIN_DIR/paxd.pre-$RELEASE_ID` with `cp -p` to `paxd.new` and `mv -f`, removes `data/upgrade-info.json`, sets `halt-height = 0` and restarts the units of `VALIDATOR_UNITS` and `FULLNODE_UNITS`. The validators sign H1 again on the previous binary. `data.v6.12.0-$RELEASE_ID` is kept and never started on any host.
3. Verify (operations host):
   ```sh
   bash tools/release/rollback.sh --env-file .env.rollout-v6120 --verify --receipt "$RECEIPT_DIR/rollback.json"
   ```
   Verify: exit 0; every node on the previous binary's sha256, no upgrade file on any home, `rollback_executed` true and the chain height rising past H.
4. Restart the old feeder units only if the operators decide to run the previous release for longer, resume fleet automation (step 11) and publish a notice.
