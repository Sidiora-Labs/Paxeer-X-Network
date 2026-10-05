# Kernel SQLite migrations

SQLite schema files for the LayerX kernel (`layerxd`, C17). The append-only activity log is the authority; the tables these files create are projections and import bookkeeping that can be rebuilt from it. This directory is not source-chain migration tooling and not the EVM module store migrations of the Paxeer X chain.

| File | Creates | Loaded by |
| --- | --- | --- |
| `0001_genesis_sections.sql` | Genesis import sections, per-asset totals and historical commitments | No source file or build target loads it today |
| `0001_projection.sql` | The rebuildable projection: `projection_meta` (watermark), `balances`, `receipts`, `module_index` and `agent_queries` | `lxp_projection_open` in [`src/storage/lxp_projection.c`](../src/storage/lxp_projection.c), which takes the migration path as an argument |
| `0007_history_index.sql` | The history index over the append-only log (`history_index_meta`, `history_records`) | `lxp_history_open` in [`src/replica/lxp_history.c`](../src/replica/lxp_history.c), which takes the migration path as an argument |

The tests open these files by their repository-relative path, so run them from the repository root:

```bash
make test-projection
make test-rebuild
make test-history
```

`402LXP` is the only component allowed to write balances; the projection mirrors what the log records.

## Related code

| Kind | Location |
| --- | --- |
| Genesis builder and import sources | `cmd/layerx-genesis/` |
| Ethereum and Solana source-chain migration | [`interop/crates/layerx-migrate`](../interop/crates/layerx-migrate/OPERATIONS.md) |
| Paxeer X chain EVM module store migrations | `modules/evm/migrations/` |
