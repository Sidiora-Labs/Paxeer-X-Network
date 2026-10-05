# Local Hardhat mainnet fork

This config starts a JSON-RPC node on `http://localhost:9546` that mirrors
Ethereum mainnet at the latest (or a pinned) block. It is an optional, manual
reference for `integration_test/rpc_tests/`: use it for ad-hoc checks that the
Paxeer X chain's RPC response shapes hold up against real mainnet data. The
automated suite asserts only against the geth `--dev` reference (see
[`../README.md`](../README.md)).

## Quick start

From `integration_test/rpc_tests/`:

```bash
# In a dedicated terminal, leave this running for the duration of your test
# session.
ETH_MAINNET_UPSTREAM=<your mainnet RPC URL> npm run rpc:fork
```

Then in another terminal:

```bash
npm run test:rpc
```

## Environment

| Variable                | Default                  | Purpose                                                      |
| ----------------------- | ------------------------ | ------------------------------------------------------------ |
| `ETH_MAINNET_UPSTREAM`  | (required, no default)   | Mainnet RPC URL the fork pulls state from. Provide your own. |
| `ETH_MAINNET_FORK_BLOCK`| (unset → latest)         | Pin to a specific block for determinism.                     |

## Notes

- `chainId` is `1`, matching mainnet, so `eth_chainId` and `net_version`
  checks against the fork agree with upstream Ethereum semantics.
- Artifacts and cache live under `.artifacts/` and `.cache/` inside this folder so
  they do not collide with the module-level `artifacts/`.
- This fork is only an RPC reference. Test deployments still happen on the local
  chain; see `_start/00_bootstrap.spec.ts`.
