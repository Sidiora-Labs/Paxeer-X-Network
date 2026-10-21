# Monorepo layout

This repository is the Paxeer X Network monorepo: the Paxeer X chain node (`paxd`), the LayerX kernel (`layerxd`), the on-chain contracts, the off-chain services, the client SDKs and the user-facing applications. Each subsystem keeps its own build, qualification and trust boundary. The target layout is specified in [`spec/monorepo-layout/spec.kvx`](../spec/monorepo-layout/spec.kvx); this page describes the tree as it is and where each tree that has not moved yet is going.

## Target groups

| Group | Holds |
| --- | --- |
| `apps/` | User-facing frontends |
| `services/` | Off-chain programs |
| `sdk/` | Client and guest SDKs |
| `core/` | The chain: kernel, node packages, framework fork, programs runtime and pinned upstream libraries |
| `tests/` | Every test tree |
| `tools/` | Developer and operator tools |
| `contracts/` | On-chain sources |

`api/`, `deploy/`, `docker/`, `docs/`, `scripts/`, `spec/` and the root build and community files stay at the root.

## Current tree

| Path | Contents |
| --- | --- |
| `apps/explorer` | Block explorer frontend |
| `apps/launchpad` | Launchpad web and mobile apps and the client workspace files |
| `services/agent` | Agent daemon, MCP server, shared Rust crates, TypeScript and Python SDKs |
| `services/interop` | Interop gateway, bridge relayer and agent-commerce services |
| `services/platform` | Hosted developer platform, middleware, perps data, ramps, relay archive |
| `services/explorer` | Block explorer backend, services and deployment |
| `services/launchpad` | Launchpad services |
| `services/human` | Human control plane Rust workspace (crates, schema, tools) |
| `services/bridge` | Bridge deployment |
| `sdk/platform`, `sdk/integrations` | Platform SDKs (Go, JVM, .NET, Swift) and framework integrations |
| `sdk/launchpad` | Launchpad protocol, shared libraries and client packages |
| `sdk/js` | JavaScript SDK snapshot with its `VENDOR.kvx` pin record |
| `sdk/wallet` | Wallet SDK (`@paxeer/wallet`), a member of the `human/wallet` pnpm workspace |
| `core/deps` | Pinned snapshots of the upstream libraries the node builds from, each with a `VENDOR.kvx` pin record |
| `tests/` | Kernel C tests, `tests/fuzz`, `tests/integration`, `tests/occ`, `tests/testutil`, `tests/fixtures`, Solidity and vector suites |
| `tools/hpx`, `tools/platform`, `tools/launchpad` | Node installer and registry, platform CLI, emulator and release tooling, launchpad tools |
| `tools/loadtest`, `tools/paxctl`, `tools/uci`, `tools/wasm-tools` | Pinned tool snapshots with `VENDOR.kvx` pin records |
| `contracts/` | Root Foundry tree |
| `contracts/bridge` | Bridge vault contracts for EVM chains, the Solana program and test vectors |
| `contracts/launchpad` | Launchpad contracts |
| `deploy/` | Deployment descriptors, including `deploy/launchpad` |

## Trees that move in this layout release

| Current path | Destination |
| --- | --- |
| `human/apps/perps`, `human/apps/wallet`, `human/apps/web` | `apps/perps`, `apps/wallet`, `apps/web` |
| `human/wallet` | `services/wallet` |
| `chainflow/` | `sdk/chainflow` |

## Trees that move under core in a later release

| Current path | Destination |
| --- | --- |
| `src`, `include`, `cmd`, `migrations` | `core/kernel` |
| `sdk` framework fork entries | `core/cosmos-sdk` |
| `consensus`, `interchain`, `wasm`, `wasm-runtime`, `storage`, `modules`, `precompiles`, `node`, `rpc`, `engine`, `daemon`, `admin`, `custodyproof`, `layerxproof`, `utils` | `core/<name>` |
| `programs/sdk` | `sdk/programs` |
| `programs` | `core/programs` |

## Build boundaries

- **LayerX kernel**: C17 (`-std=c17` in the root `Makefile`). `make build`, `make test`, `make test-contracts`, `make ci`. Qualified with `make ci`, `make qualify-replay`, `make qualify-arith`, `make qualify-faults` and `make qualify-fuzz`. Replay qualification needs GCC 13, Clang 18, Docker, an amd64 musl runner, and an AArch64 cross-compiler plus QEMU; see [`QUALIFICATION.md`](QUALIFICATION.md).
- **Paxeer X node**: Go 1.25.6 (`go.mod`), built through `chain.mk`: `make paxeer-build`, `make paxeer-lint`, `make paxeer-test`, `make paxeer-ci`.
- **Workspaces**: Rust 1.91.1 (`rust-toolchain.toml`), Solidity 0.8.27 (`foundry.toml`), and the npm workspaces in `package.json`. `make workspace-build` builds them all; `make monorepo-ci` runs build, test and lint across them and does not replace either subsystem's own qualification.
- `make ci` runs `public-audit`, kernel tests, a two-build archive comparison, consensus symbol checks and sanitizer suites. `scripts/ci/layout-check.sh` and `scripts/ci/vendor-verify.sh` check the layout and the vendored snapshots.

## Trust boundaries

Co-location in this repository does not grant one subsystem new authority over another:

- LayerX protocol execution, balance writes and activity ordering remain under the kernel's deterministic runtime and specification.
- Custody, checkpoint registration, guarantor bonds, challenges and emergency exits remain under the chain's settlement contracts and modules.
- Shared source control does not imply shared deployment authority, validator sets or custody semantics.

## Workflows

Kernel and workspace workflows include `agent.yml`, `human.yml`, `platform.yml` and `programs-conformance.yml`. Node workflows are the `paxeer-*.yml` files, named `Paxeer / ...`; each lists the chain paths it covers explicitly, so changes outside those paths do not run node builds.

## Further reading

- [`README.md`](../README.md): network overview, build and test commands
- [`CONTRIBUTING.md`](../CONTRIBUTING.md): contribution guidelines
- [`QUALIFICATION.md`](QUALIFICATION.md): evidence levels and qualification gates
