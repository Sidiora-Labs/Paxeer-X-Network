# testdata - .io and .iox RPC fixtures

**What it is:** request/response fixtures for Ethereum JSON-RPC methods. The `rpc_io_test` package runs them against a Paxeer X chain EVM RPC node.

- **`.io` files** - plain request (`>>`) / expected response (`<<`) pairs, curated from [ethereum/execution-apis](https://github.com/ethereum/execution-apis) plus Paxeer X additions. **97 files.** Data-dependent `.io` fixtures that required Ethereum fixture hashes were removed; equivalent coverage lives in `.iox`.
- **`.iox` files** - extended format with `@ bind` and optional `@ ref_pair N`, where data comes from a first request. **64 files.** All are specific to this repo.

**Total: 161 fixtures** (97 `.io` + 64 `.iox`) in **69** top-level method folders. See [`../RPC_IO_README.md`](../RPC_IO_README.md) for how to run them and what the outcomes mean.

**Important:** this directory is **not** a direct copy of execution-apis. Do **not** replace it by copying from execution-apis (that would remove every `.iox` and restore removed `.io`). To add or update individual tests from execution-apis, copy only the files you need and avoid overwriting existing `.iox` or curated `.io`. The suite collects `.io` and `.iox` from `testdata/` and its subdirectories; if none are found, the integration test skips.
