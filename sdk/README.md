# sdk

`sdk/` is the Cosmos SDK fork that the Paxeer X chain node, `paxd`, is built on. Paxeer X Network is one network with two execution domains: the Paxeer X chain (`paxd`, Go, EVM chain ID 125) and the LayerX kernel (`layerxd`, C17). This directory belongs to the chain side. The [root README](../README.md) describes the whole network.

The code here is part of the root Go module, `github.com/sidiora-labs/paxeer-network` (see [`go.mod`](../go.mod)). Other packages import it as `github.com/sidiora-labs/paxeer-network/sdk/...`. Only [`cosmovisor/`](cosmovisor/README.md) and `ics23/` have their own `go.mod`.

## Layout

| Path | What it holds |
| ---- | ------------- |
| `baseapp/` | The ABCI application base that `paxd` embeds: transaction execution, commit, halt handling and state sync snapshots |
| `tasks/` | The concurrent transaction scheduler (`tasks.NewScheduler`) that `node/` uses for parallel `DeliverTx` |
| `store/` | KV store wrappers and the original `rootmulti` multistore ([README](store/README.md)) |
| `storev2/` | The `rootmulti` multistore that `paxd` mounts, built on the state stores in [`storage/`](../storage/README.md) |
| `snapshots/` | State sync snapshot manager and on-disk snapshot store ([README](snapshots/README.md)) |
| `server/` | The `start`, `export`, `rollback` and `tendermint` commands and `app.toml` handling ([README](server/README.md)) |
| `x/` | Base modules: auth, authz, bank, capability, distribution, evidence, feegrant, genutil, gov, params, slashing, staking, upgrade ([README](x/README.md)) |
| `client/`, `codec/`, `crypto/`, `types/`, `std/`, `version/`, `telemetry/`, `utils/` | CLI client context, encoding, keys and keyring, core types, version command, metrics, helpers |
| `proto/`, `third_party/` | Protobuf definitions for the SDK types and the vendored proto dependencies |
| `ics23/` | A copy of the ICS-23 proof library (module `github.com/confio/ics23/go`). The root `go.mod` resolves that path through its `replace` directive, not through this directory |
| `cosmovisor/` | Process manager that swaps binaries at upgrade heights ([README](cosmovisor/README.md)) |
| `contrib/`, `scripts/` | Developer tooling inherited from upstream ([scripts README](scripts/README.md)) |
| `testutil/`, `tests/` | Test helpers, fixtures and mocks |

The chain application that wires these modules together is [`node/app.go`](../node/app.go). Chain-specific modules (for example `evm`, `mint`, `oracle`, `tokenfactory` and the `layerx*` modules) live in [`modules/`](../modules/README.md). IBC lives in [`interchain/`](../interchain/README.md). The consensus engine is in [`consensus/`](../consensus/README.md).

## Build and test

`paxd` is built from the repository root with the chain makefile:

```bash
make -f chain.mk build      # writes ./build/paxd
make -f chain.mk install    # installs paxd into $GOPATH/bin
```

Run the SDK unit tests with the build tags the SDK makefile uses:

```bash
make -C sdk test-unit
```

or with plain Go from the repository root:

```bash
go test ./sdk/...
```

## Documentation

- Hosted documentation: [docs.paxeer.app](https://docs.paxeer.app/)
- Repository layout and build ownership: [`docs/MONOREPO.md`](../docs/MONOREPO.md)
- Module specifications: the `spec/` directory under each module in [`x/`](x/README.md)
