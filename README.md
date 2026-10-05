<p align="center"><img src="https://supabase.paxeer.app/storage/v1/object/public/json/Tiny%20worker%20on%20a%20floating%20moss%20island.png" alt="Paxeer X Network" width="1540"></p>

<h1 align="center">Paxeer X Network</h1>

<h1 align="center">
  
English · [Español](docs/readme/README.es.md) · [日本語](docs/readme/README.ja.md) · [Русский](docs/readme/README.ru.md) · [简体中文](docs/readme/README.zh-CN.md) · [Português](docs/readme/README.pt-BR.md) · [Deutsch](docs/readme/README.de.md) · [Français](docs/readme/README.fr.md)
  
</h1>
<p align="center">
  <!-- ═══ Network Identity ═══ -->
  <img src="https://img.shields.io/badge/Paxeer%20X-Network-6C3BFF?style=for-the-badge" alt="Paxeer X Network" />
  <img src="https://img.shields.io/badge/Chain%20ID-125%20(0x7D)-1F6FEB?style=for-the-badge&logo=chainlink&logoColor=white" alt="Chain ID 125" />
  <img src="https://img.shields.io/badge/EVM-Compatible-3C3C3D?style=for-the-badge&logo=ethereum&logoColor=white" alt="EVM Compatible" />
  <img src="https://img.shields.io/badge/LayerX-Kernel%20(C17)-FF6B00?style=for-the-badge&logo=databricks&logoColor=white" alt="LayerX kernel" />
  <img src="https://img.shields.io/badge/Solidity-Smart%20Contracts-363636?style=for-the-badge&logo=solidity&logoColor=white" alt="Solidity" />
</p>

<p align="center">
  <!-- ═══ Language Stack ═══ -->
  <img src="https://img.shields.io/badge/C-00599C?style=for-the-badge&logo=c&logoColor=white" alt="C" />
  <img src="https://img.shields.io/badge/Rust-CE422B?style=for-the-badge&logo=rust&logoColor=white" alt="Rust" />
  <img src="https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/WebAssembly-654FF0?style=for-the-badge&logo=webassembly&logoColor=white" alt="WebAssembly" />
  <img src="https://img.shields.io/badge/Python-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python" />
</p>

<p align="center">
  <!-- ═══ Repo Health ═══ -->
  <img src="https://img.shields.io/github/actions/workflow/status/Sidiora-Labs/Paxeer-X-Network/ci.yml?style=for-the-badge&logo=githubactions&logoColor=white&label=CI" alt="CI" />
  <img src="https://img.shields.io/github/license/Sidiora-Labs/Paxeer-X-Network?style=for-the-badge" alt="License" />
  <img src="https://img.shields.io/github/last-commit/Sidiora-Labs/Paxeer-X-Network?style=for-the-badge&logo=git&logoColor=white" alt="Last Commit" />
  <img src="https://img.shields.io/github/stars/Sidiora-Labs/Paxeer-X-Network?style=for-the-badge&logo=github" alt="Stars" />
</p>

## What Paxeer X Network is
Paxeer X Network is one network with two execution domains: the Paxeer X chain (`paxd`, Go, EVM chain ID 125) and the LayerX kernel (`layerxd`, C17), the deterministic execution and accounting domain for autonomous agents. Official site: [paxeer.network](https://paxeer.network/). Documentation: [docs.paxeer.app](https://docs.paxeer.app/).

Inside the LayerX kernel, every state-changing operation enters as a signed, canonically encoded `Activity`. The kernel verifies the actor and its authority, consumes the account sequence, orders the activity on one global sequence, applies a deterministic state transition, and returns a signed receipt tied to the resulting state root.

The append-only activity log is the authority. Database indexes are disposable projections and can be rebuilt by replaying that log. Consensus-critical execution excludes floating point, local clock decisions, database iteration order, and other sources of nondeterminism. `402LXP` is the only component allowed to write balances. Protocol modules emit validated transfer sets rather than mutating funds themselves.

Ordinary agent activity is executed and ordered inside the LayerX kernel. Periodic checkpoints settle to the Paxeer X chain, which holds custody, checkpoint registration, guarantor bonds, challenges, withdrawals, disputes, and emergency exits. An ordinary kernel action does not require a Paxeer X chain transaction.

This repository is the Sidiora Labs monorepo for Paxeer X Network: the Paxeer X chain and the LayerX kernel in one repository. Co-location keeps the kernel, the chain, contracts, and developer surfaces auditable in one place. Each subsystem keeps its own build, release, deployment, and trust boundary. The governing specification is [`spec/paxeer-x/spec.kvx`](spec/paxeer-x/spec.kvx), rendered as [`spec/paxeer-x/design.md`](spec/paxeer-x/design.md); release notes are in [`CHANGELOG.md`](CHANGELOG.md).

## Try the network

The limited beta has not opened yet. The gateway API becomes available when it does. This is a mainnet beta on real value, so there is no faucet for general use; approved developers receive test allocations from the team.

The public EVM JSON-RPC names for chain ID 125 are listed in
[`docs/site/docs/reference/public-rpc.md`](docs/site/docs/reference/public-rpc.md).
The public endpoint checklist is
[`docs/wiki/Getting-Started-Beta.md`](docs/wiki/Getting-Started-Beta.md).
The wallet, custody-credit funding, Asset, Programs, and HTTP 402 path is
[`docs/wiki/PaymentsQuickstart.md`](docs/wiki/PaymentsQuickstart.md). The
`layerx wallet` and `layerx token` commands are in `platform/cli`, and the
LXT-20 program token interface is in
`programs/crates/layerx-programs-registry/src/lxt20.rs`. Encodings:
[`docs/wiki/Assets.md`](docs/wiki/Assets.md). RPC methods:
[`docs/wiki/PublicRpc.md`](docs/wiki/PublicRpc.md). Evidence levels:
[`docs/wiki/CommitmentLevels.md`](docs/wiki/CommitmentLevels.md). The public
payment API is [`docs/wiki/PublicAPI.md`](docs/wiki/PublicAPI.md).

To run everything locally, follow [`docs/wiki/Quickstart.md`](docs/wiki/Quickstart.md): install the `layerx` CLI from `platform/cli`, bring up a disposable beta cluster with `make platform-beta-cluster-up`, source `build/beta-cluster/env`, then create a key, claim from that cluster's private-network faucet, submit a payment, verify the receipt, and deploy a program.

```sh
layerx key create quickstart
```

```sh
layerx --json payment test \
  --from "$LAYERX_TEST_SOURCE_DID" \
  --to "$LAYERX_TEST_DESTINATION_DID" \
  --currency "$LAYERX_TEST_ASSET" \
  --amount "$LAYERX_TEST_AMOUNT" \
  --idempotency-key paymentquickstart1
```

```sh
layerx --json receipt verify \
  --receipt receipt.hex \
  --batch-id "$batch_id" \
  --asset "$asset" \
  --previous-state-root "$previous_root" \
  --resulting-state-root "$resulting_root" \
  --sequencer-public-key "$sequencer_key"
```

```sh
layerx --json program deploy \
  quickstart-program/target/wasm32-unknown-unknown/release/quickstart_program.wasm \
  --program-id <program_id> \
  --idempotency-key <idempotency_key> \
  --key quickstart \
  --account-sequence 0 \
  --not-before-ms <not_before_ms> \
  --expires-at-ms <expires_at_ms> \
  --previous-state-root <previous_state_root>
```

## Build from source

The core runtime is C17 (`-std=c17` in the root `Makefile`). Agent, human, and platform workspaces use Rust 1.91.1 (`rust-toolchain.toml`). The kernel settlement contracts in `contracts/` use Solidity 0.8.27 (`foundry.toml`). Replay qualification needs GCC 13, Clang 18, Docker, an amd64 musl runner, and an AArch64 cross-compiler plus QEMU; see [`docs/QUALIFICATION.md`](docs/QUALIFICATION.md).

```sh
make build
make test
make test-contracts
make ci
```

Paxeer X chain targets (`paxd`, defined in `chain.mk`), without changing directories:

```sh
make paxeer-build
make paxeer-lint
make paxeer-test
make paxeer-ci
```

`make ci` runs `public-audit`, kernel tests, a two-build archive comparison, consensus symbol checks, and sanitizer suites. `make monorepo-ci` is a separate cross-subsystem gate. A local pass is not authorization to deploy contracts, move custody, or handle real assets.

## Repository layout

| Path | Purpose |
| --- | --- |
| `src/`, `include/` | C17 LayerX kernel runtime, state machine, storage, sequencing, replay, and settlement integration |
| `cmd/` | Kernel daemons and tools (`layerxd`, `layerxctl`, `layerx-guarantor`, genesis, verify) |
| `agent/` | Rust agent interface, SDK, daemon, MCP server, encoding, cryptography, and proof verification |
| `human/` | Human control plane, typed intent compiler, KMS, explorer index, and the web and wallet applications |
| `platform/` | Developer platform, hosted services, middleware, SDKs, emulator, CLI, and release tooling |
| `programs/` | Programs runtime, registry, interpreter, sandbox, market, and program SDKs for the LayerX kernel |
| `interop/` | Agent-commerce and cross-network interoperability surfaces, including the bridge relayer |
| `contracts/` | Solidity contracts on the Paxeer X chain for custody, checkpoints, guarantor bonding, claims, disputes, and exits |
| `bridge/` | Bridge vault contracts and deployment runbook for EVM chains and Solana |
| `explorer/` | Block explorer, a Blockscout fork kept apart from the rest of the monorepo |
| `go.mod`, `chain.mk`, `daemon/`, `node/`, `modules/`, `consensus/`, `sdk/`, `rpc/`, `precompiles/`, `storage/`, `wasm/`, `docker/` | Paxeer X chain node (`paxd`), EVM/RPC compatibility, storage engines, modules, precompiles, and subsystem-local builds |
| `spec/` | Normative KVX specification, generated design, requirements, and task graph |
| `tests/`, `fuzz/` | Native, contract, replay, invariant, fault, and fuzz suites |
| `migrations/` | SQL for kernel genesis import sections, rebuildable projections, and the history index |
| `docs/` | Wiki, documentation site sources, monorepo notes, and qualification documentation |

## Documentation

- Hosted documentation: [docs.paxeer.app](https://docs.paxeer.app/)
- Wiki index: [`docs/wiki/Home.md`](docs/wiki/Home.md)
- Getting started: [`docs/wiki/Getting-Started-Beta.md`](docs/wiki/Getting-Started-Beta.md)
- Payments developer path: [`docs/wiki/PaymentsQuickstart.md`](docs/wiki/PaymentsQuickstart.md)
- Public JSON-RPC: [`docs/wiki/PublicRpc.md`](docs/wiki/PublicRpc.md)
- Assets and tokens: [`docs/wiki/Assets.md`](docs/wiki/Assets.md)
- Commitment levels: [`docs/wiki/CommitmentLevels.md`](docs/wiki/CommitmentLevels.md)
- Monorepo layout and release tags: [`docs/MONOREPO.md`](docs/MONOREPO.md)
- Qualification gates: [`docs/QUALIFICATION.md`](docs/QUALIFICATION.md)
- Specification: [`spec/paxeer-x/spec.kvx`](spec/paxeer-x/spec.kvx)
- Release notes: [`CHANGELOG.md`](CHANGELOG.md)

## SDKs and integrations

| Language | Path |
| --- | --- |
| Rust | `agent/crates/layerx-sdk` |
| Python | `agent/sdk/python` |
| TypeScript | `agent/sdk/typescript` |
| Go | `platform/sdk/go` |
| JVM | `platform/sdk/jvm` |
| .NET | `platform/sdk/dotnet` |
| Swift | `platform/sdk/swift` |

`layerx-agentd` is `agent/crates/layerx-agentd`. The MCP server is `agent/crates/layerx-mcp`. The developer CLI in `platform/cli` installs those transports with `layerx install mcp` and `layerx install a2a`.

## Architecture

Paxeer X Network is one network with two execution domains: the Paxeer X chain node (`paxd`, Go) and the LayerX kernel (`layerxd`, C17), joined by EVM precompiles on one side and hosted platform services on the other. Rounded boxes are running services, cylinders are durable stores, hexagons are contracts and EVM precompiles (labelled with their address), and plain boxes are modules inside a state machine. Arrows follow the direction data moves: solid arrows submit or write, dotted arrows carry reads, events and proofs.

```mermaid
flowchart TB
  subgraph users ["Users and agents"]
    direction TB
    human(["Human"])
    agent(["Autonomous agent"])
    web("Human web app")
    hsvc("layerx-human-service")
    intents("layerx-intents<br/>typed intent compiler")
    kms("layerx-human-kms")
    mcp("layerx-mcp")
    sdk("layerx-sdk<br/>Rust · TypeScript · Python")
    agentd("layerx-agentd")
  end

  subgraph platform ["Platform services"]
    direction TB
    gateway("layerx-gateway<br/>eth_* relay · px_* · px_getCapabilities")
    indexer("layerx-indexer<br/>LayerX and Paxeer ingesters")
    idxdb[("SQLite history")]
    relay("relay/archive node")
  end

  subgraph lx ["LayerX kernel · C17"]
    direction TB
    layerxd("layerxd<br/>LNI server · sequencer")
    kernel["Activity kernel<br/>module dispatch"]
    perps["perps · 6"]
    spot["spot · 10"]
    gov["governance · 7"]
    kbridge["bridge · 8"]
    ledger["402LXP ledger<br/>sole balance writer"]
    alog[("Activity log")]
    guarantor("layerx-guarantor")
  end

  subgraph px ["Paxeer X node · paxd"]
    direction TB
    evm("EVM JSON-RPC<br/>pax-geth")
    cons("Tendermint consensus")
    occ("OCC parallel executor")
    pAnchor{{"layerxAnchor<br/>0x1014"}}
    pCustody{{"layerxCustody<br/>0x1013"}}
    pExch{{"layerxExchange<br/>0x1015"}}
    pBridge{{"layerxBridge<br/>0x1016"}}
    pLaunch{{"launchpad<br/>0x1017"}}
    mCustody["layerxcustody module"]
    mExch["layerxexchange module"]
    mBridge["layerxbridge module"]
    mLaunch["launchpad module"]
    paxdb[("PaxDB state store")]
  end

  subgraph eth ["Bridge to Ethereum"]
    direction TB
    relayer("layerx-bridge-relayer")
    vault{{"PaxeerXVault"}}
  end

  human --> web
  web -->|"HTTPS"| hsvc
  hsvc -->|"typed intent"| intents
  hsvc -->|"custody signing"| kms
  intents -->|"compiled Activity"| agentd
  agent --> mcp
  agent --> sdk
  mcp -->|"tool calls"| agentd
  sdk -->|"daemon socket"| agentd
  agentd -->|"LNI frames"| layerxd

  layerxd --> kernel
  kernel --> perps & spot & gov & kbridge
  kernel -->|"transfer sets"| ledger
  ledger -->|"signed receipt"| alog
  alog -->|"sealed batch"| guarantor
  guarantor -->|"submitCheckpoint"| pAnchor

  layerxd -.->|"batch sync"| relay
  relay -.->|"history batches"| indexer
  evm -.->|"blocks · logs · tx_search"| indexer
  indexer --> idxdb
  indexer -.->|"px_getHistory"| gateway
  layerxd -.->|"lx_* reads"| gateway
  gateway -->|"eth_* relay"| evm
  gateway -.->|"eth_call · eth_getLogs"| web
  gateway -.->|"px_* reads"| hsvc

  evm -->|"txs"| cons
  cons -->|"ordered blocks"| occ
  occ --> pAnchor & pCustody & pExch & pBridge & pLaunch
  pCustody --> mCustody
  pExch -->|"pending intent"| mExch
  pExch -->|"margin deposit"| mCustody
  pBridge --> mBridge
  pLaunch --> mLaunch
  occ -->|"state commits"| paxdb

  pExch -.->|"intent logs decoded"| intents
  mCustody -.->|"custody credit proof"| kbridge

  vault -.->|"Deposit event"| relayer
  relayer -->|"bridgeIn"| pBridge
  pBridge -.->|"BridgeOut event"| relayer
  relayer -->|"release"| vault

  classDef actor fill:#e4dcf2,stroke:#7a68ad,color:#2a2340
  classDef svc fill:#d8e5f3,stroke:#4d77a8,color:#1b2a3c
  classDef store fill:#e1ecd4,stroke:#6a8c48,color:#23321a
  classDef contract fill:#f3e2cc,stroke:#ad7a3e,color:#3b2913
  classDef module fill:#e6e8eb,stroke:#6b7480,color:#22272e

  class human,agent actor
  class web,hsvc,intents,kms,mcp,sdk,agentd,gateway,indexer,relay,layerxd,guarantor,evm,cons,occ,relayer svc
  class idxdb,alog,paxdb store
  class pAnchor,pCustody,pExch,pBridge,pLaunch,vault contract
  class kernel,perps,spot,gov,kbridge,ledger,mCustody,mExch,mBridge,mLaunch module
```

The bridge to Ethereum in detail: each relayer instance holds one attestor key behind a remote signer, journals every observed event and signed transaction before broadcast, and meets a signature threshold above one by exchanging signatures with other instances.

```mermaid
flowchart LR
  subgraph ethb ["Ethereum"]
    bVault{{"PaxeerXVault"}}
  end

  subgraph relb ["Relayer instance"]
    direction TB
    bRelayer("layerx-bridge-relayer")
    bSigner("Remote signer<br/>one attestor key")
    bJournal[("Append-only journal")]
    bCosign[("Cosign directory")]
  end

  subgraph pxb ["Paxeer X node"]
    direction TB
    bPrec{{"layerxBridge<br/>0x1016"}}
    bModule["layerxbridge module<br/>attestors · caps · nullifiers"]
    bTf["tokenfactory<br/>bridged denoms"]
  end

  bVault -.->|"Deposit event"| bRelayer
  bRelayer -->|"digest to sign"| bSigner
  bRelayer -->|"events · signed txs"| bJournal
  bRelayer -->|"threshold signatures"| bCosign
  bRelayer -->|"bridgeIn + signatures"| bPrec
  bPrec --> bModule
  bModule -->|"mint · burn"| bTf
  bPrec -.->|"BridgeOut event"| bRelayer
  bRelayer -->|"release + signatures"| bVault

  classDef svc fill:#d8e5f3,stroke:#4d77a8,color:#1b2a3c
  classDef store fill:#e1ecd4,stroke:#6a8c48,color:#23321a
  classDef contract fill:#f3e2cc,stroke:#ad7a3e,color:#3b2913
  classDef module fill:#e6e8eb,stroke:#6b7480,color:#22272e

  class bRelayer,bSigner svc
  class bJournal,bCosign store
  class bVault,bPrec contract
  class bModule,bTf module
```

## Contributing

Read [`CONTRIBUTING.md`](CONTRIBUTING.md) before opening a pull request. Protocol changes start in `spec/`. Do not disclose a suspected vulnerability in a public issue; follow [`SECURITY.md`](SECURITY.md).

## Security

Report vulnerabilities through GitHub private reporting, as described in [`SECURITY.md`](SECURITY.md).

## License

Licensed under the Apache License, Version 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

Paxeer X Network is developed by Sidiora Labs. Source: [github.com/Sidiora-Labs/Paxeer-X-Network](https://github.com/Sidiora-Labs/Paxeer-X-Network).
