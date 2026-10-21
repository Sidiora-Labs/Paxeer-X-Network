# Paxeer X Network — code map

One line per top-level directory, and per second-level group under `apps/`, `services/`, `sdk/`, `tools/`, `contracts/`, `tests/`, `deploy/`, `docker/`, `docs/` and `spec/`. The layout and its build boundaries are described in [docs/MONOREPO.md](docs/MONOREPO.md); the move plan is [spec/monorepo-layout/spec.kvx](spec/monorepo-layout/spec.kvx).

Two kinds of tree are marked below:

- **Phase B** — the chain trees that still sit at the repository root. They move under `core/` in a later release; until then they keep the root paths listed here.
- **Vendored** — a pinned snapshot of an upstream Paxeer-Network repository, with its pin record in `VENDOR.kvx` and checked by `scripts/ci/vendor-verify.sh`.

LayerX names the kernel domain only: the C kernel (`layerxd`), its programs and `layerxctl`. Everything else is Paxeer X Network.

## Top level

- `apps/` — user-facing frontends: perps, wallet, web, explorer and launchpad.
- `services/` — off-chain programs: agent, interop, platform, human, wallet, explorer, launchpad and bridge.
- `sdk/` — client and guest SDKs, beside the Cosmos SDK fork that the chain builds from (phase B moves the fork to `core/cosmos-sdk`).
- `core/` — the chain's home; today it holds `core/deps`, the vendored Go and Rust dependencies of the node.
- `tests/` — every test tree: kernel C tests, Go integration and OCC tests, Python and shell drivers, Solidity tests, fixtures and vectors.
- `tools/` — developer and operator tools.
- `contracts/` — on-chain sources: the root Foundry projects, the bridge contracts and programs, and the launchpad contracts.
- `deploy/` — rollout manifests and the launchpad deployment tree.
- `docker/` — Docker build contexts, one image per subdirectory, and the local `paxd` cluster compose file.
- `docs/` — the documentation site sources, the wiki tree, README translations and the RPC references.
- `spec/` — the kvx feature specs, the workflow spec and the spec renderer.
- `scripts/` — build, CI, release and local-chain scripts, including `scripts/ci` (repository audits and gates) and `scripts/protoc.sh`.
- `api/` — protobuf definitions of the chain's Go modules; one buf module named by the root `buf.yaml`.
- `admin/` — phase B. Go package for the node's local admin service: configuration, server and service types.
- `cmd/` — phase B. Kernel command-line programs: `layerxd`, `layerxctl` (Rust), `layerx-genesis`, `layerx-verify`, `layerx-guarantor`, `layerx-handover`, `layerx-module-registry` and `layerx-archive-codec`.
- `consensus/` — phase B. The BFT consensus engine of the chain, a Tendermint Core fork embedded in `paxd` and imported from the root Go module.
- `custodyproof/` — phase B. Go verifier for kernel custody proofs, mirroring the refusal semantics of the Rust verifier in `services/agent`.
- `daemon/` — phase B. Daemon entry points: `daemon/paxd` (the chain node binary) and `daemon/layerx-custody-proof`.
- `engine/` — phase B. The parallel transaction executor of the node, with its dependency packages and tests.
- `include/` — phase B. Public C headers of the LayerX kernel (`include/layerx`).
- `interchain/` — phase B. Vendored copy of ibc-go, the IBC implementation the chain uses.
- `layerxproof/` — phase B. Go codec and verifier for LayerX kernel proofs, with test vectors.
- `migrations/` — phase B. SQLite schema files for the kernel's projection and import tables.
- `modules/` — phase B. Chain-specific modules of `paxd`: `evm`, `mint`, `oracle`, `tokenfactory`, `perps`, `spot`, `pricefeed`, `launchpad`, `xweb` and the `layerx*` modules.
- `node/` — phase B. The chain application (`node/app.go`) that wires the modules, ante handlers and upgrades together.
- `precompiles/` — phase B. EVM precompiles of the chain, including the bank, staking, governance, IBC, launchpad and `layerx*` precompiles.
- `programs/` — phase B. The LayerX programs workspace: deterministic WASM guest execution with no balance-writing authority. `programs/sdk` moves to `sdk/programs` and the rest to `core/programs`.
- `rpc/` — phase B. The EVM JSON-RPC server (`evmrpc`): the standard `eth_`, `net_`, `web3_`, `txpool_` and `debug_` namespaces plus `pax_` and `pax2_`.
- `src/` — phase B. C17 sources of the LayerX kernel: codec, crypto, ledger, modules, network, protocol, sequencer, state, storage and replica.
- `storage/` — phase B. PaxDB, the node's storage layer with separate active-state, historical-state and ledger layers.
- `utils/` — phase B. Shared Go helpers of the node: constants, data structures, logging, metrics and panic handling.
- `wasm/` — phase B. The CosmWasm module and its app wiring, protobufs and stored contract archive.
- `wasm-runtime/` — phase B. Go wrapper around the CosmWasm virtual machine used to compile, instantiate and execute contracts.
- `.github/` — GitHub workflows, CODEOWNERS, issue and pull request templates, labeler, Dependabot, CodeQL and release configuration.
- `.devcontainer/` — development container definition.
- `.agents/`, `.claude/`, `.codex/`, `.codify/`, `.cursor/`, `.gemini/`, `.opencode/` — configuration and hooks of the coding-agent tools used in this repository.

## apps

- `apps/perps/` — the Hyperpax perps frontend, a Next.js trading application.
- `apps/wallet/` — the Next.js wallet web app (`@paxeer/wallet-app`), installable as a PWA, built on the wallet SDK in `sdk/wallet`.
- `apps/web/` — the Next.js web application of the human interface (`@layerx/human-web`), with its `layerx-ui` component library.
- `apps/explorer/` — the block explorer frontend, a fork of the Blockscout Next.js frontend.
- `apps/launchpad/` — the launchpad client pnpm workspace: `web` (Next.js) and `mobile` (React Native), with the workspace files and client qualification scripts.

## services

- `services/agent/` — the agent workspace: the interaction layer between autonomous agents and the LayerX kernel, with its Rust crates, the agent daemon, the MCP server, schemas, tools and the agent SDKs (Rust, TypeScript, Python).
- `services/interop/` — Rust workspace of edge adapters that turn external protocols into kernel evidence: interop gateway, mirror publisher and signer, bridge relayer, intent ingester, gas station and the web search sidecar.
- `services/platform/` — Rust workspace of the hosted platform services (gateway, core, authority, identity, indexer, registry, webhooks, dashboard, faucet, testnet control, feeder, perps keeper), the reference ramp, the perps data indexer, the perf-db storage engine and the developer documentation site.
- `services/human/` — the human interface Rust workspace: the human service, identity, security and movement providers, the key provider, the intents compiler, the explorer index, the API schema and its tools.
- `services/wallet/` — the wallet pnpm workspace: wallet gateway, attestor, key ceremony, demo app, deployment files and schema.
- `services/explorer/` — the block explorer backend (a fork of the Blockscout Elixir umbrella application), its Rust services (signature provider, smart contract verifier and shared libraries) and its deployment files.
- `services/launchpad/` — the launchpad Go workspace: the data services (api, discovery, indexer, notifications, pnl-tracker, ranking-algo, stats-workers, trading-charts), the media services (gateway, livestream, metadata, social, user), the security policies and the workspace `Makefile`.
- `services/bridge/` — bridge deployment: chain configurations, attestor list, governance proposals and the deploy, verify and relayer scripts.

## sdk

- `sdk/platform/` — the generated platform SDKs (Go, JVM, Swift, .NET), their generators, schema, conformance suite and generation pipeline.
- `sdk/integrations/` — framework integration examples: agents, Android, Express, FastAPI, iOS, Next.js and Spring.
- `sdk/js/` — vendored `@pax-js/cosmos`, the TypeScript types and clients generated from the chain's protobufs.
- `sdk/wallet/` — the wallet SDK (`@paxeer/wallet`): Supabase sign-in, the embedded wallet, an EIP-1193 provider and clients for the human service and the kernel.
- `sdk/chainflow/` — the Chainflow Data Gateway client libraries for Go, Python, Rust and TypeScript.
- `sdk/launchpad/` — launchpad libraries: `protocol` (contract ABIs, Go bindings and event types), `shared` (the runtime library of every launchpad service) and `packages` (the client TypeScript SDKs and contract packages).

The remaining entries are the Cosmos SDK fork; phase B moves each of them to `core/cosmos-sdk`:

- `sdk/baseapp/` — the ABCI base application.
- `sdk/client/` — client context, CLI commands, flags and transaction broadcast.
- `sdk/codec/` — amino and protobuf codecs.
- `sdk/contrib/` — developer tooling, git hooks, images and local network scripts inherited from upstream.
- `sdk/cosmovisor/` — the upgrade process manager, its own Go module.
- `sdk/crypto/` — keys, keyring, HD derivation and Ledger support.
- `sdk/ics23/` — the ICS-23 proof specification, its own Go module.
- `sdk/internal/` — internal conversion helpers.
- `sdk/proto/` — Cosmos SDK protobuf definitions.
- `sdk/scripts/` — helper scripts inherited from upstream.
- `sdk/server/` — in-process ABCI server start and its CLI.
- `sdk/snapshots/` — state sync snapshots.
- `sdk/std/` — standard codec registration.
- `sdk/store/` — KV store building blocks of the state machine.
- `sdk/storev2/` — store v2 commitment, root multistore and state layers.
- `sdk/tasks/` — the transaction scheduler.
- `sdk/telemetry/` — metrics helpers.
- `sdk/tests/` — shared fixtures and mocks.
- `sdk/testutil/` — test helpers and the in-process test network.
- `sdk/third_party/` — third-party protobuf definitions.
- `sdk/types/` — core types: addresses, coins, messages and ABCI wrappers.
- `sdk/utils/` — small helpers and tracing.
- `sdk/version/` — version reporting.
- `sdk/x/` — the framework modules (auth, bank, staking, governance and the rest).

## core

- `core/deps/geth/` — vendored pax-geth, the Ethereum execution layer the node builds on.
- `core/deps/cosmwasm/` — vendored pax-cosmwasm, the CosmWasm virtual machine and contract crates.
- `core/deps/db-backend/` — vendored pax-db-backend, the common database interface over several backends.
- `core/deps/tm-db/` — vendored pax-tm-db.
- `core/deps/log/` — vendored paxlog, structured logging on `log/slog` with per-logger level control.
- `core/deps/utils/` — vendored pax-utils.

## tools

- `tools/hpx/` — HPX: the public installer, node manager, artifact publisher and peer registry for chain nodes.
- `tools/platform/` — Rust workspace of the platform command-line tool, the emulator and the release tooling.
- `tools/launchpad/` — launchpad tools: coverage gate, cache, delivery, storage and production load and recovery tooling.
- `tools/loadtest/` — vendored pax-load, a transaction load generator that measures chain performance.
- `tools/paxctl/` — vendored paxctl, a utility that patches node configuration and genesis files.
- `tools/uci/` — vendored pax-uci, shared continuous integration workflows.
- `tools/wasm-tools/` — vendored pax-wasm-tools, command-line tools for WebAssembly files.
- `tools/benchmark/` — benchmark scripts, scenarios and the benchmark load generator.
- `tools/bringup/` — operator scripts for serving public names and checking live systems.
- `tools/build/` — shared build pieces: `sanitizers.mk` and the npm workspace build script.
- `tools/chain/` — the `paxd tools` command group, including upgrade replay against a copy of a data directory.
- `tools/ci/` — CI reporting script and tests of the image publish and runner routing checks.
- `tools/codebase-map/` — an offline, browsable map of the checkout built from the code index.
- `tools/containers/` — check that the Docker build contexts name only current paths.
- `tools/deploy/` — off-chain image table, charting library image build and Railway project scripts.
- `tools/docs/` — generator of the system reference pages under `docs/system`.
- `tools/flyci/` — controller and runner image of the CI runners hosted on Fly.io.
- `tools/jev/` — command-line checks over dependencies, docs, failures, the ledger and pull requests through an external review service.
- `tools/ops/` — node operations: backups, keyring migration and monitoring.
- `tools/paxeer-x/` — release gate scripts, builders, fixtures and runtime inventory of the chain release.
- `tools/price-sidecar/` — the validator price sidecar service unit and configuration template.
- `tools/proofs/` — SMT proof of the kernel arithmetic.
- `tools/qualification/` — release and beta qualification drivers.
- `tools/railway/` — helper that tabulates Railway file mounts, with its test.
- `tools/release/` — reproducible `paxd` builds, staging, snapshots, rollback and rehearsal scripts.
- `tools/repotest/` — repository-level Go tests of module and link directives, and the `go generate` entry point.
- `tools/rollout/` — the rollout driver that reads `deploy/rollout.kvx`.
- `tools/runtime/` — wrapper that runs a command under the runtime clock.
- `tools/spec/` — projection of a spec into Hyperfide generations.
- `tools/specgen/` — compiler of the kvx specs into the generated rule files and markdown mirrors.
- `tools/tx-scanner/` — the `paxd tools scan-tx` command that finds transactions missing from the index.
- `tools/utils/` — shared Go helper for the tools.
- `tools/v6120-halt-export/` — command that completes a halt export fixture and replays its golden reads.
- `tools/v6120-holders-scan/` — command that completes the launchpad holders fixture from on-chain balances.
- `tools/wallet/` — test of the wallet PWA build arguments.
- `tools/workspace/` — lists of workspace gate targets and manifest roots.
- `tools/*.c`, `tools/*.sh` — kernel log inspection, corpus generation, legacy reader, reserve report, shadow compare and replay matrix utilities.

## contracts

- `contracts/bridge/` — the bridge on-chain side: EVM contracts (Foundry), the Solana program and its admin tool, cross-chain vectors and the attestation documents.
- `contracts/launchpad/` — the launchpad AMM contracts, a Hardhat project outside the root Foundry build.
- `contracts/challenge/` — kernel checkpoint challenge manager.
- `contracts/config/` — feature flags, static configuration and the proof and vector fixtures the kernel contracts read.
- `contracts/crypto/` — Ed25519 verification in Solidity.
- `contracts/custody/` — asset registry, vault and reserve reconciler.
- `contracts/deployment/` — deployment blueprint, predeploys, preinstalls and the deployment validator.
- `contracts/governance/` — timelocks and the proxy governance proposal.
- `contracts/interfaces/` — shared Solidity interfaces.
- `contracts/libraries/` — shared Solidity libraries: arithmetic, encoding, hashing, checkpoints and errors.
- `contracts/manager/` — validator manager container and migrator.
- `contracts/scripts/` — Hardhat deploy script.
- `contracts/security/` — reentrancy lock, governance and upgrade guards.
- `contracts/src/` — Paxeer X chain contracts: pointer contracts, `WPAX`, precompile interfaces and test helpers (built with `foundry.paxeer.toml`).
- `contracts/storage/` — withdrawal nullifier registry.
- `contracts/test/` — Foundry and Hardhat tests of the chain contracts.
- `contracts/wasm/` — compiled CosmWasm contracts used by the pointer tests.

## tests

- `tests/agent/` — Python agent SDK tests.
- `tests/arith/` — kernel arithmetic reference and property tests.
- `tests/bridge/` — custody and bridge credit drivers and fixtures.
- `tests/chain/` — Go tests of chain modules: distribution, epoch, mint and fee token.
- `tests/ci/` — tests of the CI recipe and runner routing.
- `tests/codec/` — kernel codec tests.
- `tests/crypto/` — kernel hash, Ed25519, Merkle and constant-time tests.
- `tests/daemon/` — kernel daemon drivers and chain contract tests.
- `tests/explorer/` — explorer gate and data boundary tests.
- `tests/fixtures/` — shared fixtures: genesis sets, assets, custody, governance, programs, trading payloads and the example chain.
- `tests/fuzz/` — kernel fuzz harnesses and corpus.
- `tests/integration/` — YAML-driven integration tests that run against the local four-node docker cluster.
- `tests/ledger/` — kernel ledger tests: accounts, send, receive and receipts.
- `tests/modules/` — kernel module tests and drivers: assets, escrow, budgets, streams and services.
- `tests/network/` — gateway, oracle and web adapter tests.
- `tests/occ/` — optimistic concurrency control tests of the chain.
- `tests/oracle/` — oracle adapter isolation check.
- `tests/paxeer/` — anchor submission, membership sync and data availability tests.
- `tests/platform/` — platform maintenance head tests.
- `tests/programs/` — program runtime and ABI tests.
- `tests/protocol/` — kernel protocol tests: envelope, dispatch, arena and activity codec.
- `tests/qualification/` — kernel qualification and wallet capability tests.
- `tests/relay-archive/` — relay archive end-to-end tests.
- `tests/replay/` — golden replay test and corpus.
- `tests/replica/` — replica replay matrix.
- `tests/sdk/` — TypeScript program terminal test.
- `tests/sequencer/` — admission and oracle and web root tests.
- `tests/solidity/` — Foundry tests of the kernel contracts.
- `tests/state/` — kernel state tests: allowances, grants, identity and journal.
- `tests/storage/` — kernel log, write-ahead log and recovery tests.
- `tests/support/` — shared test headers and genesis helpers.
- `tests/testutil/` — Go test helpers for keepers, networks and fuzzing.
- `tests/tools/` — kernel verify CLI test.
- `tests/vectors/` — shared test vectors: codec, checkpoints, finality evidence and replay corpus.
- `tests/wallet/` — wallet deploy, live check and secret scan tests.
- `tests/workspace/` — workspace dashboard test.
- `tests/*.c`, `tests/*.py` — kernel test harness and the remaining top-level kernel tests.

## deploy

- `deploy/launchpad/` — launchpad deployment: Dockerfiles, compose files, Railway values, production recovery and the deploy script.
- `deploy/*.kvx` — rollout manifests read by `tools/rollout`.

## docker

- `docker/common/` — shared environment file helper.
- `docker/localnode/`, `docker/rpcnode/` — images of the local `paxd` cluster nodes.
- `docker/monitornode/`, `docker/docker_compose_monitoring/` — Prometheus and Grafana configuration for the local cluster.
- `docker/paxeer/`, `docker/layerx/` — `paxd` node images, the second with the web search sidecar.
- `docker/kernel/` — the kernel app image: kernel daemon with the platform and interop services it runs beside.
- `docker/relay-archive/` — relay archive image of the kernel daemon.
- `docker/redis/` — Redis image with environment file decoding.
- `docker/hpx-registry/` — the HPX node registry service.
- `docker/flyci-controller/`, `docker/flyci-runner/` — Fly.io CI controller and runner images.
- `docker/bridge-relayer/` — bridge relayer and mirror signer.
- `docker/gas-station/` — gas station service.
- `docker/intent-ingester/` — intent ingester.
- `docker/interop-gateway/` — interop gateway.
- `docker/interop-mirror/` — mirror publisher, verifier and signer.
- `docker/x-websearch/` — web search sidecar.
- `docker/search-front/` — nginx front for the search names.
- `docker/human-service/` — human service with its providers, the agent daemon and `layerxctl`.
- `docker/web/` — the human web app.
- `docker/wallet-attestor/`, `docker/wallet-gateway/`, `docker/wallet-pwa/` — wallet attestor, wallet gateway and wallet PWA.
- `docker/ramps/` — reference ramp.
- `docker/platform-*/` — one image per platform service: agent-boundary, authority, core, dashboard, dashboard-web, faucet, gateway, identity, indexer, internal, node, registry, registry-builder, testnet and webhooks.
- `docker/explorer-*/` — explorer backend, Elixir builder, frontend, signature provider and smart contract verifier.

## docs

- `docs/site/` — the MkDocs Material documentation site and its wiki renderer.
- `docs/wiki/` — the original wiki tree the root README links into.
- `docs/readme/` — translations of the root README.
- `docs/system/` — generated system reference: chain and kernel modules, precompiles, routes, RPC, CLI, governance and SDK.
- `docs/runbooks/` — validator runbook.
- `docs/build/` — older wiki renderer.
- `docs/swagger/`, `docs/swagger-ui/` — Swagger UI assets and the package that embeds them in `paxd`.
- `docs/*.md` — monorepo layout, qualification levels and the chain overview and RPC notes.

## spec

- `spec/layerx-merge/` — the LayerX merge feature spec, its contracts and docs.
- `spec/release-v6-12-0/` — the chain release feature spec, its contracts, docs and Hyperfide projections.
- `spec/monorepo-layout/` — the monorepo layout feature spec.
- `spec/perf-db-adoption/` — the perf-db adoption feature spec.
- `spec/workflow.kvx`, `spec/specgen` — the workflow spec and the script that renders the generated rule files.
