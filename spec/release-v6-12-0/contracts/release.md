# Release v6.12.0 contract

This contract binds the release candidate, the release artifacts, the upgrade rehearsal and the fleet rollout of the Paxeer X Network node binary paxd at v6.12.0. Code citations are at revision 211acbc6c of github.com/Sidiora-Labs/Paxeer-X-Network unless prefixed `Launch-x:` (commit c5eb7c7 of github.com/Sidiora-Labs/Launch-x). Paths under `tools/release/` that do not exist at that revision are planned paths created by the tasks of this feature.

## Build and plan

1. Base. The release candidate is built from the main branch at the revision on which task 1.3.100 (node wiring) is done. Every later step names that revision as `RELEASE_COMMIT`.
2. Plan name. The upgrade plan is named `v6.12.0`. It is declared as a Go string constant `= "v6.12.0"` under `node/`, listed in `knownUpgradePlans` (node/upgrades.go:530-534) and registered with `SetUpgradeHandler` beside the existing plans (node/upgrades.go:199-209). The Release Check identity step greps for exactly that declaration (.github/workflows/paxeer-release-check.yml:45-49).
3. Release identity. `tools/release/version.json` (tools/release/version.json:1-4) holds `{"version": "v6.12.0", "upgrade": "v6.12.0"}`. The version must match the semantic version pattern of .github/workflows/paxeer-release-check.yml:39-44 and be newer than every `paxeer-network/v*` tag (.github/workflows/paxeer-release-check.yml:57-65).
4. Tag. The release tag is `paxeer-network/v6.12.0` (tools/release/build-paxd.sh:19-20). The tag namespace is the one chain.mk resolves for its version string (chain.mk:9).
5. Toolchain. The build refuses to run unless `go env GOVERSION` equals the `go` line of go.mod (tools/release/build-paxd.sh:11-17, go.mod:3), with `GOTOOLCHAIN=local`. The Rust toolchain is the channel of rust-toolchain.toml:2.
6. Kernel libraries. Before the Go build, the release script runs `make paxd-native-libs`, the Makefile target the baseline wiring adds, which builds the kernel static library `build/liblayerx.a` (Makefile:46, Makefile:211) and the sandbox static library `programs/target/debug/liblayerx_programs_sandbox.a` (Makefile:3405, Makefile:3417-3418). These are the link directories and libraries that modules/lxphost/host.go:6-7 names. The release script reads tools/release/version.json and the version check of the baseline wiring reads it too; only the release tasks edit it.
7. Version stamp. The binary carries `Version=v6.12.0`, `Commit=RELEASE_COMMIT` and `BuildTags=netgo,ledger` through the linker flags of tools/release/build-paxd.sh:35-41. The application hands the version to the base application (node/app.go:593), which reports it as `version` in the ABCI info response (sdk/baseapp/abci.go:98) served at the CometBFT `abci_info` route (consensus/internal/rpc/core/routes.go:71).
8. Determinism inputs. `SOURCE_DATE_EPOCH` is the commit time of `RELEASE_COMMIT` (tools/release/build-paxd.sh:22); the Go build runs with `-trimpath -buildvcs=false` and an empty build id (tools/release/build-paxd.sh:36, tools/release/build-paxd.sh:44); C and Rust paths are remapped to repository-relative prefixes, as the layerxd build already does with `-ffile-prefix-map` (tools/release/build-paxd.sh:64-65).

## Release artifacts

1. Directory. One release writes `dist/paxeer-network/v6.12.0/` (tools/release/build-paxd.sh:25) and nothing else.
2. Files. The directory holds `paxd-v6.12.0-linux-amd64` (tools/release/build-paxd.sh:43-44), the three pinned libwasmvm libraries checked against wasm-runtime/libwasmvm-linux.sha256 (tools/release/build-paxd.sh:46-58), `layerxd-v6.12.0-linux-amd64` when `PAXD_RELEASE_LAYERXD=1` (tools/release/build-paxd.sh:61-67), `native-libs.sha256` with the sha256 of the two kernel static libraries linked into paxd, the release upgrade document `upgrade-info.json`, and `SHA256SUMS` over every other file of the directory (tools/release/build-paxd.sh:69-70).
3. Release upgrade document. `upgrade-info.json` in the release is `{"name": "v6.12.0", "info": "<binaries JSON>"}`, where the binaries JSON maps `linux/amd64` to the release asset URL of `paxd-v6.12.0-linux-amd64` with `?checksum=sha256:<sha256>`, the format the in-repository fork runbook passes as upgrade info (docs/runbooks/paxeer-x-fork.md:127). The sha256 equals the `paxd-v6.12.0-linux-amd64` line of `SHA256SUMS`.
4. Node upgrade file. On a node, `<home>/data/upgrade-info.json` is `{"name": "v6.12.0", "height": H1}` with H1 = halt height + 1, the two fields the store reads (sdk/store/types/store.go:56-59) from the path the upgrade keeper resolves (sdk/x/upgrade/keeper/keeper.go:434-441), written with mode 0600 as the keeper writes it (sdk/x/upgrade/keeper/keeper.go:414-431). `applyNamedUpgrade` applies the plan only when the file names a pending known plan and its height equals the block height (node/upgrades.go:574-604).
5. Reproducibility. Two builds of `RELEASE_COMMIT`, the second on an empty Go cache and with the kernel libraries rebuilt from clean, produce byte-identical `SHA256SUMS` (tools/release/reproducible-check.sh:9-20), and the sums name a paxd binary (tools/release/reproducible-check.sh:21-24).
6. Publication. Pushing the tag runs Release Publish (.github/workflows/paxeer-release-publish.yml:3-11), which checks the tag against version.json (.github/workflows/paxeer-release-publish.yml:32-44), builds with the release script (.github/workflows/paxeer-release-publish.yml:53-56) after installing the pinned Rust toolchain, and creates the GitHub release with every file of the release directory (.github/workflows/paxeer-release-publish.yml:63-73). The release title is `Paxeer X Network v6.12.0` (the title at .github/workflows/paxeer-release-publish.yml:71 is the required change).
7. Release Check. Every pull request that touches the paths of .github/workflows/paxeer-release-check.yml:3-17 runs the identity step and the reproducible build (.github/workflows/paxeer-release-check.yml:76-79) with the Rust toolchain installed. Both workflows are enabled in the repository settings before the tag is pushed.

## Upgrade rehearsal

1. Purpose. The rehearsal applies plan v6.12.0 to the real chain state at a halt height on a local network of four validators of equal power, the consensus shape of the fleet, before any fleet host is touched.
2. Inputs. A copy of the data directory and genesis of a stopped, synced full node; the running release binary as the old binary; the release candidate as the new binary; the Chainflow feed configuration of contracts/pricefeed.md for the local price sidecars. All are named in an untracked env file, the pattern of the fork rehearsal (platform/hosted/paxeer/fork-rehearsal.sh:25-31).
3. Export. The old binary exports the copied state with `paxd export --height <E>` (sdk/server/export.go:191), as the fork rehearsal exports (platform/hosted/paxeer/fork-rehearsal.sh:334-358).
4. Validator set. The exported genesis is rewritten so each of the four bonded validators keeps its operator, tokens and delegations and takes a disposable local consensus key; the CometBFT validator list and the slashing signing infos follow the new keys; the result passes `paxd validate-genesis` (platform/hosted/paxeer/fork-rehearsal.sh:444-445).
5. Halt. The four local nodes run the old binary with `--halt-height HR`; each commits HR and halts itself (sdk/baseapp/abci.go:310, sdk/baseapp/abci.go:322) with the log line `halting node per configuration` (sdk/baseapp/abci.go:360).
6. Upgrade. Each home receives the node upgrade file `{"name": "v6.12.0", "height": HR+1}`; the four nodes restart on the new binary and apply the plan in block HR+1 (node/upgrades.go:574-604), logging `applying upgrade` once (node/upgrades.go:602).
7. Assertions. The rehearsal passes only when all of these hold on all four nodes:
   - `paxd q upgrade applied v6.12.0` reports height HR+1 (platform/hosted/paxeer/fork-rehearsal.sh:536-540 is the existing query shape);
   - the ABCI info version is `v6.12.0`;
   - block HR+1 and every later block carry the same block hash on all four nodes, and the chain passes HR+10;
   - every address of contracts/address-table.md answers its view call without revert at HR+1, and each address new in v6.12.0 answers `0x` at HR (precompile presence switches at the activation height);
   - the launchpad pool count `getPoolCount()` (Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:63) served by 0x1017 equals the sum of the pool counts of the two deployment-generation registries read at HR (Launch-x:contracts/contracts/data/PoolRegistry.sol:111-113), and every migrated pool returns the reserves `getReserves()` (Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:65-69) its contract returned at HR;
   - the perpetuals market count is eighteen, with the markets of contracts/perps-markets.md;
   - one feed key per validator is registered with registerFeedKey, and after the sidecars run every listed market has a price whose update height lies inside the freshness window of contracts/pricefeed.md;
   - no node log carries `panic`.
8. Result. The rehearsal writes `rehearsal-result.env` with `upgrade`, `halt_height`, `export_height`, `final_height`, `old_bin_sha256`, `new_bin_sha256`, `pools`, `markets`, `oracle_fresh`, `precompiles_served`, the key set of platform/hosted/paxeer/fork-rehearsal.sh:568-576 extended by the last four. `new_bin_sha256` must equal the `SHA256SUMS` line of the release.

## Rollout

1. Roles. Validator hosts run validator homes; a validator host may also run a full node and every unit on one host shares one installed paxd binary. Full-node hosts run one full node each; one full node is the archive node. The edge proxies the public endpoints. The build host builds the release. The operations host runs fleet automation (watchdog and resync timers) and the verification script.
2. Mechanism. The fleet uses the operator-file path: every validator halts at H with `--halt-height H`, every home receives the node upgrade file for H1 = H + 1, and every node restarts on v6.12.0. The governance-proposal path of docs/runbooks/paxeer-x-fork.md:119-214 is not used for this release.
3. Order. Preflight; stage binaries; pause automation; freeze validators at H; validator data snapshot; full-node swap; validator swap and start; oracle sidecars start; verification; lagging nodes; resume automation. The steps, commands and checks are in docs/rollout-runbook.md.
4. Invariants.
   - No unit runs v6.12.0 before every validator has committed H on the old binary; a host whose binary is shared is swapped only after every validator home on it has halted.
   - Every height is written as a literal number into every remote command.
   - Process checks match the exact process name (`pgrep -a -x paxd`); a pattern match against a full command line is not used.
   - Every swap is atomic (`cp -p` to a temporary name in the same directory, then `mv -f`) and checked against the release sha256 before the unit starts.
   - Fleet automation that restarts, reclones or replaces binaries is stopped for the whole window and resumed only after verification passes.
   - Data moves and age-based deletion are never paired in one run; set-aside data is removed only by an explicit operator decision.
   - The archive node is never recloned.
5. Operator inputs. Halt height, maintenance window, public notice text and build host are fields filled into the access-controlled copy of the runbook before execution; this contract carries no values for them.

## Rollback

1. Before H1 commits. While fewer than two thirds of the voting power has committed H1, every node returns to the retained previous binary `paxd.pre-<RELEASE_ID>`, its node upgrade file is removed, and it restarts on the previous binary.
2. After H1 commits. Every validator home is restored from its pre-swap data snapshot, keeping the current `priv_validator_state.json` so no key signs a height twice; the previous binary is restored; full nodes are restored from a restored node's data.
3. Material. Stage keeps `paxd.pre-<RELEASE_ID>` on every host; the validator snapshot keeps one archive per validator home; neither is deleted before post-rollout verification passes.

## Post-rollout verification

1. Script. `python3 tools/release/fleet_check.py --env-file <file> --phase <phase>` checks every node named in the env file and exits 0 only when every check passes.
2. Checks per node: ABCI info version `v6.12.0`; latest height above H1 and not catching up; `q upgrade applied v6.12.0` at H1; the block hash at H1 equal on every node; every address of contracts/address-table.md served (view call without revert); the launchpad pool count equal to the rehearsal count; eighteen perpetual markets; a fresh oracle price for every listed market.
3. Receipt. With `--receipt <path>` the script writes a JSON summary of counts and pass flags only: `version`, `plan`, `nodes_checked`, `nodes_passed`, `precompiles_served`, `pools`, `markets`, `oracle_fresh`, `result`. No height, host or endpoint enters a receipt.
