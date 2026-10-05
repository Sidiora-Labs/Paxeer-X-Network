# Paxeer X Network Explorer backend

This is the backend of the Paxeer X Network block explorer: a fork of the
Blockscout Elixir umbrella application, imported from `blockscout/blockscout`
at tag `v10.2.6` (see [`../UPSTREAM.md`](../UPSTREAM.md)). It indexes the
Paxeer X chain over its EVM JSON-RPC, stores what it indexes in PostgreSQL, and
serves the REST and GraphQL APIs the explorer frontend in
[`../frontend`](../frontend) reads.

## Umbrella applications

| Application | What it does |
| --- | --- |
| [`apps/ethereum_jsonrpc`](apps/ethereum_jsonrpc) | JSON-RPC client and the per-node variants, including `paxeer_x` |
| [`apps/explorer`](apps/explorer) | Ecto schemas, the database migrations and the chain queries |
| [`apps/indexer`](apps/indexer) | Fetchers that pull blocks, transactions, receipts and tokens from the node into the database |
| [`apps/block_scout_web`](apps/block_scout_web) | Phoenix endpoint serving the `/api` REST and GraphQL interfaces |
| [`apps/nft_media_handler`](apps/nft_media_handler) | NFT media fetching, resizing and upload, off unless enabled |
| [`apps/utils`](apps/utils) | Helpers shared by the other applications |

## Paxeer X additions

The fork runs with `CHAIN_TYPE=paxeer_x` and
`ETHEREUM_JSONRPC_VARIANT=paxeer_x`. On top of the upstream code it adds:

- `EthereumJSONRPC.PaxeerX`
  ([`apps/ethereum_jsonrpc/lib/ethereum_jsonrpc/paxeer_x.ex`](apps/ethereum_jsonrpc/lib/ethereum_jsonrpc/paxeer_x.ex)),
  which documents and handles the places where the Paxeer X EVM RPC answers
  differ from Ethereum, such as the `0xffffffff` transaction type of
  Cosmos-originated messages.
- The `Explorer.Chain.PaxeerX` schemas under
  `apps/explorer/lib/explorer/chain/paxeer_x/`, their import runners under
  `apps/explorer/lib/explorer/chain/import/runner/paxeer_x/`, and the
  `*_paxeer_x_*` migrations under `apps/explorer/priv/repo/migrations/`.
- `Indexer.Fetcher.PaxeerXKernelReceipts`
  ([`apps/indexer/lib/indexer/fetcher/paxeer_x_kernel_receipts.ex`](apps/indexer/lib/indexer/fetcher/paxeer_x_kernel_receipts.ex)),
  which projects LayerX kernel receipts from a relay or archive into the
  database. It starts only when `INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL` is
  set.
- API routes under `/api/v2`: `/paxeer-x/capabilities`, `/paxeer-x/anchors`,
  `/paxeer-x/receipts`, `/paxeer-x/receipts/:id`,
  `/transactions/:hash/status` and `/addresses/:hash/unified`.

The remaining `PAXEER_X_*` settings are read in
[`config/runtime.exs`](config/runtime.exs).

## Building and testing

The Elixir and Erlang versions are pinned in
[`.tool-versions`](.tool-versions).

```
mix deps.get
mix compile
```

From the repository root, `explorer/deploy/tools/mix-in-builder.sh` runs any
mix command inside the pinned builder image with a disposable PostgreSQL
sidecar, and `scripts/explorer/gate-test.sh` runs every Paxeer X test suite;
[`../README.md`](../README.md) describes both, the lint scripts and the local
Docker Compose stack.

## Contributing

Upstream's contribution guide and code of conduct are kept in
[CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

GNU General Public License v3.0. See [LICENSE](LICENSE).
