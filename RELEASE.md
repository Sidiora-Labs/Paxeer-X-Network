# Releasing paxd

A Paxeer X Network release is one git tag, one [`tools/release/version.json`](tools/release/version.json)
and a set of files anyone can rebuild bit for bit from that tag. Applying a release to a running
network is a governance upgrade: every validator and full node halts at the plan height and restarts
on the new binary. The tooling for both halves lives in [`tools/release`](tools/release).

## Release identity

- `tools/release/version.json` holds `version` (`vX.Y.Z`) and `upgrade`, the on-chain upgrade plan
  name the release carries. The plan name must equal the constant the node registers for it (for
  `v6.12.0`, `V6120Upgrade` in [`node/upgrades_v6120.go`](node/upgrades_v6120.go), registered in
  [`node/upgrades.go`](node/upgrades.go)).
- The release tag is `paxeer-network/` + `version`. `tools/release/build-paxd.sh --print-tag` prints
  it.
- The `Paxeer / Release Check` workflow ([`.github/workflows/paxeer-release-check.yml`](.github/workflows/paxeer-release-check.yml))
  runs on pull requests that touch the node, the release tooling or the price sidecar. It refuses a
  version that is not semantic, an `upgrade` name the node does not declare, a tag that already
  exists, or a version that is not newer than the latest `paxeer-network/v*` tag, and then runs the
  reproducibility check.

## Building a release

`tools/release/build-paxd.sh` builds the release from the current checkout. Its only flag is
`--print-tag`; it reads `PAXD_DIST_DIR` (output directory), `PAXD_COMMIT`, `SOURCE_DATE_EPOCH` and
`PAXD_RELEASE_LAYERXD=1` (also build `layerxd`) from the environment.

- Toolchains are pinned and checked before anything builds: Go must equal the `go` line of `go.mod`
  (`GOTOOLCHAIN=local`), `rustc` the channel of `rust-toolchain.toml`, and the C compiler the
  `CC_VERSION` of `tools/release/toolchain.env`.
- The kernel and programs native libraries are built with `make paxd-native-libs` and their hashes
  recorded in `native-libs.sha256`.
- `paxd` is built from `./daemon/paxd` with `-trimpath -buildvcs=false`, tags `netgo ledger`, an empty
  build id and the version variables (`Name`, `AppName`, `Version`, `Commit`, `BuildTags`), with
  `CGO_ENABLED=1`, fixed `CGO_*FLAGS`, `LC_ALL=C`, `TZ=UTC` and `SOURCE_DATE_EPOCH` defaulting to the
  commit date.
- The price sidecar is built from the `layerx-oracle-feeder` package of `services/platform`.

Output goes to `dist/paxeer-network/vX.Y.Z/` (or `PAXD_DIST_DIR`):

- `paxd-vX.Y.Z-<os>-<arch>` and `price-sidecar-vX.Y.Z-<os>-<arch>`;
- on linux/amd64, the three `libwasmvm*.so` runtime libraries, each checked against
  `wasm-runtime/libwasmvm-linux.sha256`;
- `layerxd-vX.Y.Z-<os>-<arch>` when `PAXD_RELEASE_LAYERXD=1`;
- `native-libs.sha256`;
- `upgrade-info.json`, written by `tools/release/upgrade_info.py release`: the plan name and the
  download URL of the linux/amd64 `paxd` asset with its sha256, the document a governance upgrade
  proposal carries;
- `SHA256SUMS` over every other file.

`tools/release/reproducible-check.sh` builds twice into separate directories, the second time on an
empty Go build cache, prints both `SHA256SUMS` and fails unless the native library hashes and every
file hash match.

## Publishing

1. Open a pull request that sets `version` and `upgrade` in `tools/release/version.json`, with the
   upgrade handler registered under that name. `Paxeer / Release Check` validates the identity and
   runs the reproducibility check.
2. Merge it to `main`.
3. Tag the merge commit and push the tag:

   ```sh
   git tag -s paxeer-network/vX.Y.Z -m "Paxeer X Network vX.Y.Z" <commit>
   git push origin paxeer-network/vX.Y.Z
   ```

4. `Paxeer / Release Publish` ([`.github/workflows/paxeer-release-publish.yml`](.github/workflows/paxeer-release-publish.yml))
   runs on the tag, or by hand with an existing tag as input. It checks the tag against
   `version.json`, runs `build-paxd.sh` with `PAXD_RELEASE_LAYERXD=1`, generates release notes with git-cliff from
   [`.github/cliff.toml`](.github/cliff.toml), and creates the GitHub release with every file of the
   release directory attached. The run summary lists `SHA256SUMS` and the release URL.
5. On the build host, compare the published release with a local build of the same tag:

   ```sh
   tools/release/verify-release.sh --tag paxeer-network/vX.Y.Z --dist dist/paxeer-network/vX.Y.Z
   ```

   It requires the asset names to equal the files of the local release directory, every published
   `SHA256SUMS` line to equal the local line, and every file on both sides to match its own
   `SHA256SUMS`. It needs an authenticated `gh`.

## Verifying a published binary

Anyone can rebuild a tag and compare:

```sh
git clone https://github.com/Sidiora-Labs/Paxeer-X-Network.git
cd Paxeer-X-Network
git checkout paxeer-network/vX.Y.Z
PAXD_RELEASE_LAYERXD=1 tools/release/build-paxd.sh
gh release download paxeer-network/vX.Y.Z --pattern SHA256SUMS --dir published
diff published/SHA256SUMS dist/paxeer-network/vX.Y.Z/SHA256SUMS
```

Build on linux/amd64 with the pinned Go, Rust and C toolchains. An empty diff means the published
files are the ones the tag builds. To check downloaded files without rebuilding, run
`sha256sum --check SHA256SUMS` in the download directory.

## Rehearsing the upgrade

`tools/release/rehearse-v6120.sh` rehearses the plan on a local four-validator network built from real
chain state, without contacting a remote host. It exports a stopped full node's data with the previous
binary, rewrites the genesis to local validator keys, halts the network with `--halt-height`, archives
every home with `snapshot.sh`, writes the node upgrade file and restarts on the release candidate. It
then checks the applied plan, the version, the precompile set, launchpad pool parity, the markets,
the feed keys and fresh sidecar prices, the programme checks and the block hashes after the upgrade,
and finally runs `rollback.sh --execute` and requires the restored chain to resume on the previous
binary. Inputs come from flags or from a filled copy of
[`tools/release/env/rehearsal.env.example`](tools/release/env/rehearsal.env.example) passed with
`--env-file`; `--check` validates the inputs and touches nothing, and `--receipt` writes the receipt
that `fleet_check.py` reads later. Run it with `--help` for every flag.

## Rolling out to a fleet

The fleet scripts read one operator input file, a filled copy of
[`tools/release/env/rollout.env.example`](tools/release/env/rollout.env.example) kept outside the
repository. It names the release id, tag and checksums, the halt height, the chain id, the binary
directory, the hosts, node homes, service units and operator key names, the snapshot and credential
directories, and the service endpoints. The scripts refuse to run while any key they read is empty.
Each script takes `--env-file`, most take `--receipt <file>` to write a JSON receipt of the step, and
`--service-manager systemd|foreground` where they start or stop nodes (default `systemd`). The
complete step-by-step procedure, with the operator inputs and the checks after each step, is the
[v6.12.0 rollout runbook](spec/release-v6-12-0/docs/rollout-runbook.md). The scripts currently carry the
`v6.12.0` file names and plan name.

The order of a rollout:

1. **Inputs and preflight.** `fleet_check.py --env-file <file> --phase inputs` validates the operator
   inputs, the release binaries in the working directory and ssh access to every host;
   `--phase preflight` checks every node before the window. `verify-release.sh` confirms the published
   release.
2. **Stage.** On each host, with the release binaries in the working directory,
   `stage.sh --env-file <file> --local` checks the checksums and shared libraries, keeps the running
   binary as `paxd.pre-<RELEASE_ID>` and installs the new `paxd` and price sidecar beside it.
   `stage.sh --verify` checks every host over ssh, `--check` validates the inputs only and
   `--automation-status` reports the fleet automation and old feeder units that must be stopped for
   the window.
3. **Freeze.** Each validator sets `halt-height` in `app.toml` to the plan halt height, one at a time,
   and stops itself after committing that block. `fleet_check.py --phase frozen` requires every
   validator to report that height.
4. **Snapshot.** With the nodes stopped, `snapshot.sh --env-file <file>` archives the `data` directory
   of every validator and full-node home into the snapshot directory and checks each archive,
   including the validator signing state at the halt height. `snapshot.sh --verify` re-checks the
   archives, and `rollback.sh --readiness` confirms the rollback material is in place.
5. **Swap.** `swap.sh --env-file <file> --role full`, then `--role validator`, writes
   `data/upgrade-info.json` into each home (through `upgrade_info.py node`), resets `halt-height` to
   `0`, swaps the binary after checksum and shared-library checks, starts the units and waits for
   each node to report ready. `--halt-height` and `--release-doc` override the input file; `--verify`
   re-checks a swapped host. `fleet_check.py --phase validators` follows.
6. **Price sidecars.** Each validator operator creates a feed key with
   `feed_key.py new --out <key file>`, registers it with the price feed precompile using the call data
   that `feed_key.py payload <public key>` prints, and starts its price sidecar from the unit and
   configuration template in [`tools/price-sidecar`](tools/price-sidecar).
   `fleet_check.py --phase oracle` requires fresh prices and one feed key per bonded validator.
7. **Services and verification.** The off-chain data services and frontends are cut over, then
   `fleet_check.py` runs the `markets`, `precompiles`, `services`, `full` and `post` phases. With no
   `--phase` it runs `post`. Expected values come from the rehearsal receipt (`--rehearsal <file>`);
   `--no-rehearsal` takes them from the input file instead.

### Rollback

`rollback.sh --env-file <file> --execute` restores the fleet to the previous release: on every host it
prepares, stops every node, moves each home's `data` aside and restores it from its snapshot
archive, reinstalls the
retained `paxd.pre-<RELEASE_ID>` binary and starts the nodes, one phase across all hosts before the
next. `--readiness` checks the rollback material without changing anything and `--verify` checks a
rolled-back fleet. Run on the operations host it drives every host over ssh; `--local` runs on the
current host only, where `--phase prepare|stop|restore|start` runs a single phase.

## Testing the release tooling

`tools/release/tests/run-all.sh` runs every release tooling suite once (build, identity, reproducibility,
verify-release, rehearsal, stage, swap, snapshot, rollback, env templates and workflows) followed by
the operator-input and receipt checks.

## Sign-off checklist

- [ ] `version` and `upgrade` in `tools/release/version.json` are correct and the upgrade handler for
      `upgrade` is registered in `node/upgrades.go`.
- [ ] `Paxeer / Release Check` passed on the release commit.
- [ ] `Paxeer / Release Publish` is green and the release carries `paxd`, the price sidecar,
      `layerxd`, the libwasmvm libraries, `native-libs.sha256`, `upgrade-info.json` and
      `SHA256SUMS`.
- [ ] `verify-release.sh` passed on the build host, and at least one maintainer rebuilt the tag and got
      the same `SHA256SUMS`.
- [ ] The upgrade rehearsal passed on the release candidate and its receipt is kept for
      `fleet_check.py`.
- [ ] The governance upgrade proposal names the `upgrade` plan and carries `upgrade-info.json`, and
      validators had the release URL and hashes before the vote ended.
- [ ] The rollout pins the published `paxd` sha256.
