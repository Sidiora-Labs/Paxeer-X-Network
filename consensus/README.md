# Consensus engine

This directory is the Byzantine Fault Tolerant consensus engine of the Paxeer X chain, the Go execution domain of Paxeer X Network that `paxd` runs (EVM chain ID 125). It is a fork of Tendermint Core: [`version/version.go`](version/version.go) still reports Tendermint `0.35.0-unreleased` and ABCI `0.17.0`.

The engine is not a separate Go module. Its packages are imported as `github.com/sidiora-labs/paxeer-network/consensus/...` from the repository's root [`go.mod`](../go.mod), and it is embedded in `paxd` rather than shipped as its own binary.

For protocol details, refer to the [specification](./spec/README.md). For detailed analysis of the consensus protocol, including safety and liveness proofs, read the paper "[The latest gossip on BFT consensus](https://arxiv.org/abs/1807.04938)".

## How it fits the network

- `paxd start` constructs the node with [`node`](node/) from [`sdk/server/start.go`](../sdk/server/start.go).
- `paxd tendermint` exposes the engine's operator subcommands, wired in [`sdk/server/util.go`](../sdk/server/util.go): `show-node-id`, `show-validator`, `show-address`, `version`, `gen-autobahn-config`, `gen-validator`, `reindex-event`, `light`, `reset`, `unsafe-reset-all`, `gen-node-key`, `inspect`, `key-migrate`, `debug` and `completion`. Their implementations live in [`cmd/tendermint/commands`](cmd/tendermint/commands/) and [`sdk/server`](../sdk/server/).
- The application is called in-process through the Go `Application` interface in [`abci/types/application.go`](abci/types/application.go), wrapped by [`internal/proxy`](internal/proxy/). This tree has no socket or gRPC ABCI transport.
- Autobahn is optional. `autobahn-config-file` in the node configuration ([`config/config.go`](config/config.go)) points at its JSON configuration; leaving it empty disables it. `paxd tendermint gen-autobahn-config` generates that file.

The LayerX kernel (`layerxd`, C17) is the other execution domain of the network; it is described in the [root README](../README.md).

## Layout

| Path | Contents |
| --- | --- |
| [`abci/`](abci/) | Application interface types and the example key-value application |
| [`autobahn/`](autobahn/), [`internal/autobahn/`](internal/autobahn/) | Autobahn types and implementation |
| [`cmd/tendermint/commands/`](cmd/tendermint/commands/) | Cobra commands mounted under `paxd tendermint` |
| [`cmd/priv_val_server/`](cmd/priv_val_server/) | gRPC remote signer server built on [`privval/`](privval/) |
| [`cmd/contract_tests/`](cmd/contract_tests/) | Dredd hooks for the RPC contract tests against [`rpc/openapi/openapi.yaml`](rpc/openapi/openapi.yaml) |
| [`config/`](config/) | Node configuration |
| [`crypto/`](crypto/) | Ed25519 keys, hashing and Merkle trees |
| [`internal/`](internal/) | Consensus state machine, mempool, evidence, p2p, block sync, state sync, block and state stores, RPC handlers |
| [`light/`](light/) | Light client |
| [`node/`](node/) | Node assembly |
| [`proto/`](proto/) | Protocol buffer definitions and generated Go code |
| [`rpc/`](rpc/) | RPC client, JSON-RPC server library, core types and the OpenAPI description |
| [`types/`](types/) | Blocks, votes, validators, evidence, genesis and consensus parameters |
| [`spec/`](spec/) | Upstream protocol specifications, TLA+ and Ivy models |
| [`test/`](test/) | Fuzz targets and end-to-end harness |
| [`networks/`](networks/), [`scripts/`](scripts/) | Upstream deployment and maintenance tooling |

## Build and test

From the repository root:

```sh
make paxeer-build        # builds ./build/paxd, which embeds this engine
go test ./consensus/...  # runs the engine's Go tests
```

[`Makefile`](Makefile) in this directory is inherited from upstream. Its `build` and `install` targets compile `./cmd/tendermint`, which in this tree holds only the `commands` package and no `main` package, so they do not produce a binary. Build the engine through `paxd`. The Docker-based local network in [`networks/local`](networks/local/README.md) and the end-to-end Docker image in [`test/e2e`](test/e2e/README.md) depend on a standalone binary as well.

The fuzz targets are described in [`test/fuzz/README.md`](test/fuzz/README.md).

## Security

To report a vulnerability, follow the repository [security policy](../SECURITY.md).

## Documentation

Hosted documentation for Paxeer X Network is at [docs.paxeer.app](https://docs.paxeer.app/). The source repository is [Sidiora-Labs/Paxeer-X-Network](https://github.com/Sidiora-Labs/Paxeer-X-Network).
