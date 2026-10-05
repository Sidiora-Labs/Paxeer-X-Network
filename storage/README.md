# PaxDB

PaxDB is the storage layer of the Paxeer X chain (`paxd`). It replaces the single [IAVL](https://github.com/cosmos/iavl) database of a stock Cosmos SDK chain with separate layers for active state, historical state, and ledger data. The node wires it in `node/app.go`.

## Architecture

The design follows the [Cosmos store v2 ADR](https://github.com/cosmos/cosmos-sdk/blob/main/docs/architecture/adr-065-store-v2.md): instead of one large database that stores both latest and historical data, state is split into two layers:

- **State Commitment (SC)**: the active chain state in a Merkle tree, providing transaction state access and the app hash
- **State Store (SS)**: versioned raw key/values for full nodes and archive nodes to serve historical queries

### Advantages
- SC and SS backends are swappable
- The SS store only keeps raw key/values, which saves disk space and reduces write amplification

### Trade-offs
- Historical proofs are not available for every historical block
- Historical data has no integrity or correctness validation of its own

## Layout

| Path | Contents |
| ---- | -------- |
| [`state_db/sc`](state_db/sc) | State commitment: [`memiavl`](state_db/sc/memiavl) (memory-mapped IAVL), [`flatkv`](state_db/sc/flatkv) (EVM state with LtHash integrity), [`composite`](state_db/sc/composite) (routes Cosmos modules to memiavl and EVM to flatkv), [`migration`](state_db/sc/migration) (memiavl to flatkv migration), [`hashvault`](state_db/sc/hashvault) (refuses a different hash for an already committed block) |
| [`state_db/ss`](state_db/ss) | State store: Cosmos and EVM stores, composite routing, pruning, history offload, and the database backend selection |
| [`state_db/bench`](state_db/bench) | SC and SS benchmarks, including the [`cryptosim`](state_db/bench/cryptosim) workload |
| [`db_engine`](db_engine) | Key/value engines: PebbleDB, RocksDB, the [`litt`](db_engine/litt) store, and a sharded cache (`dbcache`) |
| [`ledger_db`](ledger_db) | Block, transaction, receipt, and event storage |
| [`wal`](wal) | Write-ahead log and changelog used by the SC layer |
| [`config`](config) | SC, SS, receipt, and write-mode configuration |
| [`tools`](tools) | The `paxdb` operator CLI and benchmarks; see [`tools/README.md`](tools/README.md) |
| `common`, `proto` | Shared helpers and protobuf definitions |

## State Commitment (SC) Layer
Responsibility of the SC layer:
- Provide the root app hash for each new block
- Provide the data access layer for transaction execution
- Provide an API to import/export chain state for state sync
- Provide proofs for heights not pruned yet

The memiavl implementation is a fork of MemIAVL from [Cronos](https://github.com/crypto-org-chain/cronos). To stay compatible with Cosmos SDK chains, it uses the same data structure (a Merkleized AVL tree), but represents the tree as memory-mapped flat files instead of persisting it as key/values in a database engine. See [`state_db/sc/memiavl/README.md`](state_db/sc/memiavl/README.md).

EVM state can live in flatkv instead; the `write_mode` setting (see [`config/write_mode.go`](config/write_mode.go)) selects memiavl only, flatkv only, or one of the migration stages between them.

## State Store (SS) Layer
The SS layer provides a modular storage backend for versioned raw key/value pairs in an embedded database:
- Queries for versioned raw key/value pairs
- Versioned CRUD operations
- Versioned batching
- Versioned iteration
- Pruning

### DB Backend
PebbleDB is the default SS backend (`pebbledb`). RocksDB (`rocksdb`) is available when the binary is built with `-tags=rocksdbBackend`.
