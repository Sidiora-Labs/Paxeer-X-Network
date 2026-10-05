# Tools

Developer and operator tools for the repository. Selected subdirectories:

| Directory | What it is |
| --- | --- |
| `chain/` | The `paxd tools` command group, wired into `paxd` by `daemon/paxd/cmd/root.go`; `chain/upgrade-replay` replays an upgrade plan against a copy of a chain's data directory |
| `tx-scanner/` | The `paxd tools scan-tx` command described below |
| [`codebase-map/`](codebase-map/README.md) | An offline, browsable map of the checkout |
| `specgen/` | Compiles the kvx specs under `spec/` into generated rule files (`go run ./tools/specgen -root .`) |
| `bringup/` | Operator scripts for serving public names (`edge.sh`) and checking live systems (`check-live.sh`) |
| `flyci/` | The controller and the runner image of the CI runners hosted on Fly.io (each has its own `fly.toml`) |
| `build/` | Shared build pieces such as `sanitizers.mk`, included by the root `Makefile` |

The rest of this page covers the built-in `paxd tools` command.

## TX-Scanner 
TX-Scanner is a tool that helps to scan transactions that are missing or failed 
to be indexed. This is usually used on archive nodes where all historical transactions
need to be persisted and queryable. 

In the Cosmos SDK that `paxd` vendors there is a known issue: during shutdown, transactions for the 
current block might not be correctly indexed. The consequence of not indexing transactions properly 
is that those transactions can't be queried, even though they exist in the block data.

This tool helps to scan archive nodes to find out all the missing transactions so that
later on you can reindex all the missing transactions to make them queryable again.

### Usage
The scanner reads blocks through the node's gRPC server (`--endpoint`, default the local host, and `--port`, default 9090). `--start-height` sets the first height to scan, `--state-dir` the directory that holds the state file, and `--batch-size` (default 100) and `--bps-limit` (default 400 blocks per second) bound the query rate. It is recommended to run this tool as a background daemon process:
```
# Run in the background
paxd tools scan-tx --start-height 1 --state-dir ./ > scan.log &
```
The tool will keep scanning from the start height, if there's already
a state file exist in `state-dir`, it will instead start from the previous height.

The tool won't stop until you manually stop it, once it hit latest
block height, it will keep running and waiting for new blocks to come,
it keep scanning all the newly produced blocks once they are committed.

### State Format
A typical state file (`tx-scanner-state.json`) would look like this:
```
{
    "last_processed_height": 44394319,
    "blocks_missing_txs": [123400, 2124542]
}
```
last_processed_height: int64, represent last processed block height
blocks_missing_txs: []int64, represent all the block heights that is missing transactions

### ReIndex Transactions
Once you finish scanning and found some missing transactions, you can
use the `paxd tendermint reindex-event` command to reindex these blocks. You need to stop
the paxd process before running the below command:
```
paxd tendermint reindex-event --start-height 2124542 --end-height 2124543
```
