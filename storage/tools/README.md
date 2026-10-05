# PaxDB tools

Operator and benchmarking tools for the PaxDB storage layer.

## paxdb

`cmd/paxdb` is a CLI for inspecting, repairing, and benchmarking node databases. Install it from this directory:

```bash
make install        # go install ./cmd/paxdb
make install-all    # same, built with the RocksDB backend tag (needs the RocksDB shared library and headers)
```

Commands:

| Command | Purpose |
| ------- | ------- |
| `dump-db` | Dump every key/value of one store from a State Store DB to a file |
| `dump-iavl` | Dump memiavl data |
| `dump-flatkv` | Dump physical FlatKV key/value pairs into per-bucket files |
| `state-size` | Print state size analysis |
| `prune` | Prune a DB at a given height |
| `replay-changelog` | Scan the changelog to replay and recover PebbleDB data |
| `memiavl-latest-version` | Print the latest memiavl version of a stopped node |
| `import-flatkv-from-memiavl` | Import selected memiavl modules into FlatKV |
| `migrate-evm-status` | Report the on-disk FlatKV EVM migration status as JSON |
| `trace-profile-report` | Run `debug_traceTransactionProfile` across a block range and generate a report |
| `benchmark-write`, `benchmark-read`, `benchmark-iteration`, `benchmark-reverse-iteration` | Measure write, read, and iteration performance of the DB backends |
| `generate` | Deprecated; no longer generates data |

Run `paxdb <command> --help` for flags.

## rpc_bench

`rpc_bench` benchmarks EVM JSON-RPC methods (for example `debug_traceBlockByNumber` and `eth_getLogs`) against a node:

```bash
go run ./rpc_bench -endpoint <rpc-url> [-concurrency 16] [-blocks 20] [-start-block N -end-block M] \
  [-requests 100] [-methods m1,m2] [-trace-discover 5] [-plot-dir DIR] [-output-file FILE]
```

`-endpoint` is required; the other flags show their defaults.

## Other packages

- `bench` — shared benchmark helpers
- `utils` — shared helpers, including a DynamoDB client
