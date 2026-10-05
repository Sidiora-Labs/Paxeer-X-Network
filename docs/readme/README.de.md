<p align="center"><img src="https://supabase.paxeer.app/storage/v1/object/public/json/Tiny%20worker%20on%20a%20floating%20moss%20island.png" alt="Paxeer X Network" width="1540"></p>

<h1 align="center">Paxeer X Network</h1>

[English](../../README.md) · [Español](README.es.md) · [日本語](README.ja.md) · [Русский](README.ru.md) · [简体中文](README.zh-CN.md) · [Português](README.pt-BR.md) · Deutsch · [Français](README.fr.md)

*Weicht diese Fassung von der englischen README ab, gilt die englische Fassung.*

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

## Was Paxeer X Network ist

Paxeer X Network ist ein Netzwerk mit zwei Ausführungsdomänen: der Paxeer-X-Chain (`paxd`, Go, EVM-Chain-ID 125) und dem LayerX-Kernel (`layerxd`, C17), der deterministischen Ausführungs- und Buchungsdomäne für autonome Agenten. Offizielle Website: [paxeer.network](https://paxeer.network/). Dokumentation: [docs.paxeer.app](https://docs.paxeer.app/).

Im LayerX-Kernel geht jede zustandsändernde Operation als signierte, kanonisch kodierte `Activity` ein. Der Kernel prüft den Akteur und seine Berechtigung, verbraucht die Kontosequenz, ordnet die Activity in eine globale Sequenz ein, wendet einen deterministischen Zustandsübergang an und gibt eine signierte Quittung zurück, die an die resultierende State Root gebunden ist.

Das nur anfügende Aktivitätsprotokoll ist die maßgebliche Quelle. Datenbankindizes sind verwerfbare Projektionen und lassen sich durch erneutes Abspielen dieses Protokolls wieder aufbauen. Konsenskritische Ausführung schließt Gleitkommaarithmetik, Entscheidungen nach der lokalen Uhr, die Iterationsreihenfolge von Datenbanken und andere Quellen von Nichtdeterminismus aus. `402LXP` ist die einzige Komponente, die Salden schreiben darf. Protokollmodule geben validierte Transfersätze aus, statt Guthaben selbst zu verändern.

Gewöhnliche Agentenaktivität wird im LayerX-Kernel ausgeführt und geordnet. Periodische Checkpoints werden auf der Paxeer-X-Chain abgewickelt, die Verwahrung, Checkpoint-Registrierung, Garantenbonds, Challenges, Auszahlungen, Streitfälle und Notausstiege hält. Eine gewöhnliche Kernel-Aktion erfordert keine Transaktion auf der Paxeer-X-Chain.

Dieses Repository ist das Monorepo von Sidiora Labs für Paxeer X Network: die Paxeer-X-Chain und der LayerX-Kernel in einem Repository. Die gemeinsame Ablage hält Kernel, Chain, Contracts und Entwickleroberflächen an einem Ort prüfbar. Jedes Subsystem behält seine eigene Build-, Release-, Deployment- und Vertrauensgrenze. Die maßgebliche Spezifikation ist [`spec/paxeer-x/spec.kvx`](../../spec/paxeer-x/spec.kvx), gerendert als [`spec/paxeer-x/design.md`](../../spec/paxeer-x/design.md); Release Notes stehen in [`CHANGELOG.md`](../../CHANGELOG.md).

## Das Netzwerk ausprobieren

Die limitierte Beta ist noch nicht geöffnet. Die Gateway-API wird verfügbar, sobald sie öffnet. Dies ist eine Mainnet-Beta mit echten Werten, daher gibt es keinen Faucet für die allgemeine Nutzung; zugelassene Entwickler erhalten Testzuteilungen vom Team.

Die öffentlichen EVM-JSON-RPC-Namen für Chain-ID 125 stehen in [`docs/site/docs/reference/public-rpc.md`](../../docs/site/docs/reference/public-rpc.md). Die Checkliste für den öffentlichen Endpunkt ist [`docs/wiki/Getting-Started-Beta.md`](../../docs/wiki/Getting-Started-Beta.md). Der Pfad für Wallet, Finanzierung per Custody-Credit, Asset, Programs und HTTP 402 ist [`docs/wiki/PaymentsQuickstart.md`](../../docs/wiki/PaymentsQuickstart.md). Die Befehle `layerx wallet` und `layerx token` liegen in `platform/cli`, das LXT-20-Token-Interface für Programme in `programs/crates/layerx-programs-registry/src/lxt20.rs`. Encodings: [`docs/wiki/Assets.md`](../../docs/wiki/Assets.md). RPC-Methoden: [`docs/wiki/PublicRpc.md`](../../docs/wiki/PublicRpc.md). Nachweisstufen: [`docs/wiki/CommitmentLevels.md`](../../docs/wiki/CommitmentLevels.md). Die öffentliche Payment-API: [`docs/wiki/PublicAPI.md`](../../docs/wiki/PublicAPI.md).

Um alles lokal zu betreiben, folgen Sie [`docs/wiki/Quickstart.md`](../../docs/wiki/Quickstart.md): die `layerx` CLI aus `platform/cli` installieren, mit `make platform-beta-cluster-up` einen verwerfbaren Beta-Cluster starten, `build/beta-cluster/env` sourcen, dann einen Schlüssel anlegen, beim privaten Faucet dieses Clusters beanspruchen, eine Zahlung einreichen, die Quittung prüfen und ein Programm deployen.

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

## Aus dem Quellcode bauen

Die Kern-Runtime ist C17 (`-std=c17` im Root-`Makefile`). Die Agent-, Human- und Platform-Workspaces verwenden Rust 1.91.1 (`rust-toolchain.toml`). Die Settlement-Contracts des Kernels in `contracts/` verwenden Solidity 0.8.27 (`foundry.toml`). Die Replay-Qualifikation braucht GCC 13, Clang 18, Docker, einen amd64-musl-Runner sowie einen AArch64-Cross-Compiler mit QEMU; siehe [`docs/QUALIFICATION.md`](../../docs/QUALIFICATION.md).

```sh
make build
make test
make test-contracts
make ci
```

Targets der Paxeer-X-Chain (`paxd`, definiert in `chain.mk`), ohne das Verzeichnis zu wechseln:

```sh
make paxeer-build
make paxeer-lint
make paxeer-test
make paxeer-ci
```

`make ci` führt `public-audit`, die Kernel-Tests, einen Vergleich zweier Builds des Archivs, Prüfungen der Konsens-Symbole und Sanitizer-Suiten aus. `make monorepo-ci` ist ein separates subsystemübergreifendes Gate. Ein lokaler Erfolg ist keine Berechtigung, Contracts zu deployen, Verwahrung zu bewegen oder echte Werte zu handhaben.

## Repository-Aufbau

| Pfad | Zweck |
| --- | --- |
| `src/`, `include/` | C17-Runtime des LayerX-Kernels: Zustandsmaschine, Speicher, Sequenzierung, Replay und Settlement-Anbindung |
| `cmd/` | Kernel-Daemons und Werkzeuge (`layerxd`, `layerxctl`, `layerx-guarantor`, Genesis, Verifikation) |
| `agent/` | Rust-Agentenschnittstelle, SDK, Daemon, MCP-Server, Kodierung, Kryptografie und Beweisprüfung |
| `human/` | Human-Steuerungsebene, typisierter Intent-Compiler, KMS, Explorer-Index sowie Web- und Wallet-Anwendungen |
| `platform/` | Entwicklerplattform, gehostete Dienste, Middleware, SDKs, Emulator, CLI und Release-Werkzeuge |
| `programs/` | Programs-Runtime, Registry, Interpreter, Sandbox, Markt und Programm-SDKs für den LayerX-Kernel |
| `interop/` | Oberflächen für Agenten-Commerce und netzübergreifende Interoperabilität, einschließlich des Bridge-Relayers |
| `contracts/` | Solidity-Contracts auf der Paxeer-X-Chain für Verwahrung, Checkpoints, Garantenbonds, Ansprüche, Streitfälle und Ausstiege |
| `bridge/` | Bridge-Vault-Contracts und Deployment-Runbook für EVM-Chains und Solana |
| `explorer/` | Block-Explorer, ein Blockscout-Fork, getrennt vom Rest des Monorepos gehalten |
| `go.mod`, `chain.mk`, `daemon/`, `node/`, `modules/`, `consensus/`, `sdk/`, `rpc/`, `precompiles/`, `storage/`, `wasm/`, `docker/` | Knoten der Paxeer-X-Chain (`paxd`), EVM/RPC-Kompatibilität, Speicher-Engines, Module, Precompiles und subsystemeigene Builds |
| `spec/` | Normative KVX-Spezifikation, generiertes Design, Anforderungen und Aufgabengraph |
| `tests/`, `fuzz/` | Native, Contract-, Replay-, Invarianten-, Fehler- und Fuzz-Suiten |
| `migrations/` | SQL für die Genesis-Importabschnitte des Kernels, wiederaufbaubare Projektionen und den Verlaufsindex |
| `docs/` | Wiki, Quellen der Dokumentationsseite, Monorepo-Notizen und Qualifikationsdokumentation |

## Dokumentation

- Gehostete Dokumentation: [docs.paxeer.app](https://docs.paxeer.app/)
- Wiki-Index: [`docs/wiki/Home.md`](../../docs/wiki/Home.md)
- Erste Schritte: [`docs/wiki/Getting-Started-Beta.md`](../../docs/wiki/Getting-Started-Beta.md)
- Pfad für Payment-Entwickler: [`docs/wiki/PaymentsQuickstart.md`](../../docs/wiki/PaymentsQuickstart.md)
- Öffentliches JSON-RPC: [`docs/wiki/PublicRpc.md`](../../docs/wiki/PublicRpc.md)
- Assets und Tokens: [`docs/wiki/Assets.md`](../../docs/wiki/Assets.md)
- Commitment-Stufen: [`docs/wiki/CommitmentLevels.md`](../../docs/wiki/CommitmentLevels.md)
- Monorepo-Aufbau und Release-Tags: [`docs/MONOREPO.md`](../../docs/MONOREPO.md)
- Qualifikations-Gates: [`docs/QUALIFICATION.md`](../../docs/QUALIFICATION.md)
- Spezifikation: [`spec/paxeer-x/spec.kvx`](../../spec/paxeer-x/spec.kvx)
- Release Notes: [`CHANGELOG.md`](../../CHANGELOG.md)

## SDKs und Integrationen

| Sprache | Pfad |
| --- | --- |
| Rust | `agent/crates/layerx-sdk` |
| Python | `agent/sdk/python` |
| TypeScript | `agent/sdk/typescript` |
| Go | `platform/sdk/go` |
| JVM | `platform/sdk/jvm` |
| .NET | `platform/sdk/dotnet` |
| Swift | `platform/sdk/swift` |

`layerx-agentd` liegt in `agent/crates/layerx-agentd`. Der MCP-Server liegt in `agent/crates/layerx-mcp`. Die Entwickler-CLI in `platform/cli` installiert diese Transporte mit `layerx install mcp` und `layerx install a2a`.

## Architektur

Paxeer X Network ist ein Netzwerk mit zwei Ausführungsdomänen: dem Knoten der Paxeer-X-Chain (`paxd`, Go) und dem LayerX-Kernel (`layerxd`, C17), verbunden auf der einen Seite durch EVM-Precompiles und auf der anderen durch gehostete Plattformdienste. Abgerundete Kästen sind laufende Dienste, Zylinder sind dauerhafte Speicher, Sechsecke sind Contracts und EVM-Precompiles (mit ihrer Adresse beschriftet), und einfache Kästen sind Module innerhalb einer Zustandsmaschine. Pfeile folgen der Richtung, in der Daten fließen: durchgezogene Pfeile reichen ein oder schreiben, gepunktete Pfeile tragen Lesezugriffe, Events und Beweise.

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

Die Bridge zu Ethereum im Detail: Jede Relayer-Instanz hält einen Attestor-Schlüssel hinter einem Remote-Signer, journalisiert jedes beobachtete Event und jede signierte Transaktion vor dem Senden und erreicht eine Signaturschwelle über eins, indem sie Signaturen mit anderen Instanzen austauscht.

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

## Mitwirken

Lesen Sie [`CONTRIBUTING.md`](../../CONTRIBUTING.md), bevor Sie einen Pull Request öffnen. Protokolländerungen beginnen in `spec/`. Melden Sie eine vermutete Schwachstelle nicht in einem öffentlichen Issue; folgen Sie [`SECURITY.md`](../../SECURITY.md).

## Sicherheit

Melden Sie Schwachstellen über das private Reporting von GitHub, wie in [`SECURITY.md`](../../SECURITY.md) beschrieben.

## Lizenz

Lizenziert unter der Apache License, Version 2.0. Siehe [`LICENSE`](../../LICENSE) und [`NOTICE`](../../NOTICE).

Paxeer X Network wird von Sidiora Labs entwickelt. Quellcode: [github.com/Sidiora-Labs/Paxeer-X-Network](https://github.com/Sidiora-Labs/Paxeer-X-Network).
