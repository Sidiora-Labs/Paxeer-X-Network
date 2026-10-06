# Releasing paxd

A Paxeer Network release is one git tag, one `version.json`, and a set of binaries anyone can rebuild bit for bit.

## Version and tag scheme

- `version.json` holds `version` (`vX.Y.Z`) and `upgrade`, the on-chain upgrade plan name the release carries (for example `v6.11`). The plan name must match the constant registered in the code (`modules/layerxcustody/types/governance_activation.go` for `v6.11`).
- The release tag is `paxeer-network/vX.Y.Z` and must equal `paxeer-network/` + `version`.
- Every tag must be newer than the latest existing `paxeer-network/v*` tag. The `Paxeer / Release Check` workflow enforces this on pull requests.

## Reproducible build

`tools/release/build-paxd.sh` builds the release from the current checkout:

- Go toolchain must be exactly the version in `go.mod` (`GOTOOLCHAIN=local`, the script refuses any other).
- `-trimpath -buildvcs=false`, linker flags `-buildid= -checklinkname=0` plus the version variables (`Name`, `AppName`, `Version` from `version.json`, `Commit` = the git commit, `BuildTags=netgo,ledger`).
- `CGO_ENABLED=1`, `CC=gcc`, fixed `CGO_*FLAGS`, `LC_ALL=C`, `TZ=UTC`, `SOURCE_DATE_EPOCH` = the commit date.
- Output goes to `dist/paxeer-network/vX.Y.Z/`: `paxd-vX.Y.Z-<os>-<arch>`, the three `libwasmvm*.so` runtime libraries (checked against `wasm-runtime/libwasmvm-linux.sha256`), `layerxd-vX.Y.Z-<os>-<arch>` when `PAXD_RELEASE_LAYERXD=1`, and `SHA256SUMS`.

`tools/release/reproducible-check.sh` builds twice into separate directories, the second time on an empty Go build cache, prints both `SHA256SUMS` and fails unless every file hash matches.

## Procedure

1. Open a pull request that sets `version.json` to the new `version` and `upgrade`. `Paxeer / Release Check` validates the identity and runs the reproducibility check.
2. Merge it to `main`.
3. Tag the merge commit and push the tag:

   ```sh
   git tag -s paxeer-network/vX.Y.Z -m "Paxeer Network vX.Y.Z" <commit>
   git push origin paxeer-network/vX.Y.Z
   ```

4. `Paxeer / Release Publish` runs on the tag: it checks the tag against `version.json`, runs `build-paxd.sh` with `layerxd`, generates release notes from `cliff.toml` with git-cliff, and creates the GitHub release with the binaries, the libwasmvm libraries and `SHA256SUMS` attached. The run summary lists the hashes.

## Sign-off checklist

- [ ] `version.json` `version` and `upgrade` are correct and the upgrade handler for `upgrade` is registered in `node/upgrades.go`.
- [ ] `Paxeer / Release Check` passed on the release commit.
- [ ] Release Publish run is green and the release has `paxd`, `layerxd`, the libwasmvm libraries and `SHA256SUMS`.
- [ ] At least one maintainer rebuilt the tag locally (below) and got the same `SHA256SUMS`.
- [ ] The governance upgrade proposal names the `upgrade` plan and a height, and validators were given the release URL and hashes before the vote ends.
- [ ] The fleet rollout pins the published `paxd` sha256.

## Verifying a published binary

```sh
git clone https://github.com/Sidiora-Labs/Paxeer-X-Network.git
cd Paxeer-X-Network
git checkout paxeer-network/vX.Y.Z
PAXD_RELEASE_LAYERXD=1 tools/release/build-paxd.sh
gh release download paxeer-network/vX.Y.Z --pattern SHA256SUMS --dir /tmp/published
diff /tmp/published/SHA256SUMS dist/paxeer-network/vX.Y.Z/SHA256SUMS
```

Build on linux/amd64 with the `go.mod` Go version, gcc and the native libraries listed in the workflow (`libssl-dev`, `libsqlite3-dev`). An empty diff means the published binaries are the ones the tag builds. To check a downloaded binary without rebuilding, run `sha256sum --check SHA256SUMS` in the download directory.
