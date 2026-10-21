<p align="center"><img src="https://supabase.paxeer.app/storage/v1/object/public/json/readme_paxeer.png" alt="Paxeer X Network" width="1540"></p>

<h1 align="center">Paxeer X Network</h1>

Paxeer X Network is one network with two execution domains: the Paxeer X chain (`paxd`, Go, EVM chain ID 125) and the LayerX kernel (`layerxd`, C17), the deterministic execution and accounting domain for autonomous agents. This repository is its monorepo: the chain node, the LayerX kernel, the on-chain contracts, the off-chain services, the client SDKs and the user-facing applications.

Official site: [paxeer.network](https://paxeer.network/). Documentation: [docs.paxeer.app](https://docs.paxeer.app/). Source: [Sidiora-Labs/Paxeer-X-Network](https://github.com/Sidiora-Labs/Paxeer-X-Network).

## What is in the network

- **The chain.** `paxd` is the Paxeer X node, written in Go with BFT consensus and an EVM execution layer. Its main package is `daemon/paxd`; the state machine modules are under `modules/` and the RPC servers under `rpc/`.
- **The LayerX kernel.** LayerX is the kernel domain: the C17 daemon `layerxd` executes and accounts for agent activity deterministically and returns a signed receipt for each activity, `layerxctl` is its operator CLI, and `programs/` holds the kernel programs runtime. It runs today as a separate daemon built from `cmd/layerxd`, and its checkpoints settle on the chain through the `layerxAnchor` precompile. Folding the kernel into the chain is specified in [`spec/layerx-merge/spec.kvx`](spec/layerx-merge/spec.kvx), which is in draft.
- **The EVM and precompile surface.** Contracts reach the native modules through stateful precompiles under `precompiles/`. `precompiles/setup.go` registers `bank`, `wasmd`, `json`, `addr`, `staking`, `gov`, `distribution`, `oracle`, `ibc`, `pointer`, `pointerview`, `p256Verify`, `solo`, `layerxVerify` (`0x1012`), `layerxCustody` (`0x1013`), `layerxAnchor` (`0x1014`), `layerxExchange` (`0x1015`), `layerxBridge` (`0x1016`), `launchpad` (`0x1017`), `feeToken` (`0x1018`) and `xweb` (`0x1019`). The v6.12.0 set built in `node/app.go` and served once that upgrade activates adds `perps` at `0x1015` ahead of `layerxExchange`, the second-generation `launchpad` at `0x1017`, `pricefeed` at `0x1020`, and the spot set `spot_pecor`, `spot_orders`, `spot_stoporders`, `spot_quoter` and `spot_vault` at `0x1021`-`0x1025`.
- **Off-chain services.** The agent daemon and MCP server, the interop gateway and bridge relayer, the hosted developer platform (gateway, indexer, registry, webhooks, perps data), the human control plane, the embedded wallet service, the explorer backend and the launchpad services.
- **Applications.** The Hyperpax perps frontend, the wallet, the web app, the explorer frontend and the launchpad web and mobile apps.

## Try the network

The limited beta has not opened yet. The gateway API becomes available when it does. This is a mainnet beta on real value, so there is no faucet for general use; approved developers receive test allocations from the team.

To connect to the Paxeer X chain (chain ID 125), use one of the public JSON-RPC names listed in [`docs/site/docs/reference/public-rpc.md`](docs/site/docs/reference/public-rpc.md). The public endpoint checklist is [`docs/wiki/Getting-Started-Beta.md`](docs/wiki/Getting-Started-Beta.md), and the wallet, funding and payments path is [`docs/wiki/PaymentsQuickstart.md`](docs/wiki/PaymentsQuickstart.md). Further reference: [assets](docs/wiki/Assets.md), [RPC methods](docs/wiki/PublicRpc.md), [commitment levels](docs/wiki/CommitmentLevels.md), [public payment API](docs/wiki/PublicAPI.md). The local kernel cluster walkthrough is [`docs/wiki/Quickstart.md`](docs/wiki/Quickstart.md); the wiki index is [`docs/wiki/Home.md`](docs/wiki/Home.md).

## Repository layout

| Path | Contents |
| --- | --- |
| `apps/` | User-facing frontends: `apps/perps` (the Hyperpax perps frontend), `apps/wallet` (wallet app), `apps/web` (web app), `apps/explorer` (block explorer frontend), `apps/launchpad` (launchpad web and mobile apps) |
| `services/` | Off-chain programs: `services/agent` (agent daemon, MCP server, Rust, TypeScript and Python SDKs), `services/interop` (interop gateway, bridge relayer), `services/platform` (hosted developer platform), `services/explorer` (block explorer backend), `services/launchpad` (launchpad services), `services/human` (human control plane Rust workspace), `services/wallet` (embedded wallet service, whose pnpm workspace also lists `sdk/wallet`), `services/bridge` (bridge deployment) |
| `sdk/` | Client SDKs: `sdk/platform` (Go, JVM, .NET, Swift), `sdk/integrations`, `sdk/launchpad` (launchpad protocol, shared libraries and client packages), `sdk/chainflow` (Chainflow client libraries for Go, Python, Rust and TypeScript), `sdk/wallet` (wallet SDK), `sdk/js`; it also holds the application framework fork the node builds from |
| `contracts/` | On-chain sources: the root Foundry tree, `contracts/bridge` (EVM vaults, the Solana program and test vectors) and `contracts/launchpad` |
| `core/deps/` | Pinned snapshots of the upstream libraries the node builds from, each with a `VENDOR.kvx` pin record |
| `tests/` | Kernel C tests, fuzz targets (`tests/fuzz`), integration tests (`tests/integration`), OCC tests (`tests/occ`), `tests/testutil`, `tests/fixtures`, Solidity and vector suites |
| `tools/` | Developer and operator tools, including `tools/hpx`, `tools/platform` (CLI, emulator, release), `tools/launchpad`, `tools/loadtest`, `tools/paxctl`, `tools/uci` and `tools/wasm-tools` |
| `migrations/` | SQL for kernel genesis import sections, rebuildable projections and the history index |
| `api/` | Protobuf definitions (buf module) |
| `deploy/`, `docker/` | Deployment descriptors (including `deploy/launchpad`) and container builds |
| `docs/` | Wiki, documentation site sources, translations and qualification notes |
| `spec/` | Feature specifications and the kvx workflow |
| `scripts/` | CI, qualification, release and workspace scripts |

The chain and kernel trees stay at the root in this release and move under `core/` in a later one: `src`, `include`, `cmd` and `migrations` (the kernel), `modules`, `precompiles`, `engine`, `consensus`, `node`, `rpc`, `storage`, `wasm`, `wasm-runtime`, `daemon`, `admin`, `custodyproof`, `layerxproof`, `utils`, `interchain`, the framework fork in `sdk`, and `programs` (whose guest SDK moves to `sdk/programs`). [`docs/MONOREPO.md`](docs/MONOREPO.md) describes the layout and the destinations of the trees that move; the target layout is specified in [`spec/monorepo-layout/spec.kvx`](spec/monorepo-layout/spec.kvx).

## SDKs

| Language | Path |
| --- | --- |
| Rust | `services/agent/crates/layerx-sdk` |
| Python | `services/agent/sdk/python` |
| TypeScript | `services/agent/sdk/typescript` |
| Go | `sdk/platform/go` |
| JVM | `sdk/platform/jvm` |
| .NET | `sdk/platform/dotnet` |
| Swift | `sdk/platform/swift` |

The agent daemon is `services/agent/crates/layerx-agentd` and the MCP server is `services/agent/crates/layerx-mcp`. The developer CLI in `tools/platform/cli` installs those transports with `layerx install mcp` and `layerx install a2a`.

## Build

Toolchains: Go 1.25.6 (`go.mod`), a C17 compiler for the kernel (root `Makefile`), Rust 1.91.1 (`rust-toolchain.toml`), Solidity 0.8.27 with Foundry (`foundry.toml`), and Node.js for the npm workspaces in `package.json`. Replay qualification also needs GCC 13, Clang 18, Docker, an amd64 musl runner, and an AArch64 cross-compiler with QEMU; see [`docs/QUALIFICATION.md`](docs/QUALIFICATION.md).

The node, from the repository root:

```sh
make paxeer-build      # build/paxd through chain.mk
make paxd-build        # also builds the kernel library and programs runtime the node links, and stamps the release version
make -f chain.mk build # the chain.mk target directly
```

The LayerX kernel and its tools:

```sh
make build
make layerxd
make layerxctl
```

The services and workspaces:

```sh
make agent-build
make interop-build
make platform-build
make human-build
make programs-build
make workspace-build   # every workspace above plus the node and the root Foundry tree
```

## Test

```sh
make test                  # kernel test suite
scripts/ci/run-tests.sh    # the kernel suite at -O0 and -O2
make test-contracts
make paxeer-test           # node Go tests through chain.mk
make paxeer-lint           # node lint
make agent-test interop-test platform-test human-test programs-test
make bridge-test-evm bridge-test-solana
make ci                    # public audit, kernel tests, reproducible build, consensus symbol scan, sanitizers
make paxeer-ci             # node lint and tests
make monorepo-ci           # every workspace: build, test and lint
```

`make public-audit` runs `scripts/ci/public-repo-audit.sh`, which every change must pass. `scripts/ci/layout-check.sh` and `scripts/ci/vendor-verify.sh` check the layout and the vendored snapshots. A local pass is not authorization to deploy contracts, move custody, or handle real assets.

## Specifications

Work is planned and tracked in kvx specifications under [`spec/`](spec/): each feature has a `spec.kvx` with its requirements, acceptance criteria and a dependency-ordered task list. The workflow itself is defined in [`spec/workflow.kvx`](spec/workflow.kvx); `spec/specgen` runs `tools/specgen`, which generates the agent instruction files such as `CLAUDE.md` and `AGENTS.md` from it.

## Project

- [CONTRIBUTING.md](CONTRIBUTING.md): how to propose and land changes
- [SECURITY.md](SECURITY.md): report vulnerabilities privately, never in a public issue
- [SUPPORT.md](SUPPORT.md): where to ask for help
- [GOVERNANCE.md](GOVERNANCE.md): how decisions are made
- [CHANGELOG.md](CHANGELOG.md): release notes
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Paxeer X Network is developed by Sidiora Labs.
