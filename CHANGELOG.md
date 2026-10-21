# Changelog

All notable changes to Paxeer X Network are recorded in this file. Versions follow the node release
identity in [`tools/release/version.json`](tools/release/version.json); a release is published under
the git tag `paxeer-network/<version>`. The release process is described in [RELEASE.md](RELEASE.md).

## Unreleased

### Repository layout

The repository is being regrouped into `core`, `sdk`, `services`, `apps` and `tests`, with `contracts`,
`docs`, `deploy`, `docker`, `scripts`, `spec`, `tools` and `api` staying at the root. Build, CI, Docker,
deploy and spec references follow every move in the same commit, and `scripts/ci/layout-check.sh`
fails on any reference to an old root path. The Go module path `github.com/Sidiora-Labs/Paxeer-X-Network`
does not change; import paths of moved packages are rewritten.

- Upstream Paxeer-Network repositories the node builds from are vendored as pinned directory snapshots,
  each with a `VENDOR.kvx` pin record that `scripts/ci/vendor-verify.sh` checks against a fresh fetch:
  `core/deps/geth`, `core/deps/tm-db`, `core/deps/db-backend`, `core/deps/log`, `core/deps/utils`,
  `core/deps/cosmwasm`, `sdk/js`, `tools/loadtest`, `tools/paxctl`, `tools/uci` and `tools/wasm-tools`.
- Test trees are consolidated under `tests`: `tests/fuzz`, `tests/fixtures/example`,
  `tests/integration`, `tests/testutil` and `tests/occ`.
- The agent workspace moves to `services/agent` and interop to `services/interop`.
- The platform workspace is split: the CLI, emulator and release tooling move to `tools/platform`, the
  platform SDK to `sdk/platform`, integrations to `sdk/integrations`, and the hosted services to
  `services/platform`.
- The human Rust workspace moves to `services/human`; the perps, wallet and web frontends move to
  `apps/perps`, `apps/wallet` and `apps/web`; the wallet SDK moves to `sdk/wallet` and the wallet
  service to `services/wallet`.
- Chainflow moves to `sdk/chainflow`.
- The explorer is split into `apps/explorer` (frontend) and `services/explorer` (backend, services and
  deploy).
- The launchpad is flattened: services to `services/launchpad`, libraries and client packages to
  `sdk/launchpad`, the client app to `apps/launchpad` and the contracts to `contracts/launchpad`.
- The bridge is split into `contracts/bridge` (on-chain programs) and `services/bridge` (deploy and
  relayer configuration).
- The HPX tooling moves to `tools/hpx`.

The chain trees stay at their current root paths in this release: `modules`, `precompiles`, `engine`,
`consensus`, `node`, `rpc`, `sdk` (the Cosmos SDK fork, beside the new SDK directories), `src`,
`include`, `cmd`, `migrations`, `storage`, `wasm`, `wasm-runtime`, `daemon`, `admin`, `custodyproof`,
`layerxproof`, `utils`, `interchain` and `programs`. They move under `core/` in a later release, which
also wires the vendored Go dependencies into `go.mod` and points the integration RPC tests at `sdk/js`.

## v6.12.0

One coordinated network upgrade, applied under the on-chain plan `v6.12.0`. Every validator and full
node halts at the plan height and restarts on the v6.12.0 `paxd`, and this is the first release
published as a tagged, reproducible build.

### Launchpad

- The launchpad precompile at `0x1017` now serves the exact interface of the deployed launchpad
  contracts: every function, event and custom error of the pool, factory, router, quoter, fees router,
  fee accumulator, pool registry, protocol config and treasury roles answers with the same selectors,
  encodings and revert data. The contract addresses stay valid and forward to the precompile.
- Holders keep everything they held. The upgrade moves every pool of both deployment generations into
  the precompile with its reserves, pool-token supply and holders, fee-right holders, accrued fees and
  hook configuration; a read through the precompile returns the same values the contract returned at
  the halt height. Pool tokens stay the deployed ERC20 contracts at their addresses, so wallet balances
  do not change, and the USDL held by the old pools and fee accumulator moves into launchpad custody.
- USDL remains the existing six-decimal ERC20; it is never minted and gets no bank denom. Pool
  creation, swaps and fees move USDL through the user's ERC20 allowance to the precompile.
- Fee rights are ERC721 tokens and pool tokens ERC20 tokens whose transfer, approval and balance
  surfaces are served by the chain and visible in the explorer. Optical hooks are part of the release,
  and launchpad administration moves to governance messages.
- The launchpad backend binds the precompile through its deployment manifest role, and its indexer,
  stats, chart, PnL and ranking services produce the same rows for a swap against the precompile as
  for one against the contracts. Service configuration accepts runtime documents, NATS credentials and
  TLS material inline as well as from files.

### Perpetuals and spot venue

- A perpetuals venue runs natively in the chain as the precompile at `0x1015`, replacing the previous
  exchange precompile from the upgrade height; earlier heights still replay against the previous
  implementation.
- Market, limit, stop-market, stop-limit, take-profit and stop-loss orders with good-till-cancel,
  immediate-or-cancel, fill-or-kill and post-only time in force, reduce-only, partial close and
  position-attached take-profit and stop-loss, matched deterministically on one price-time book per
  market.
- Cross margin in USDL across every market with tiered maintenance margin and leverage up to 1000x;
  maker and taker fees by volume tier with maker rebates, hourly funding from the premium against the
  oracle price, no borrowing fee, liquidation in slices, an insurance-fund backstop and
  auto-deleveraging. Eighteen markets open at activation: PAX, SID, NAS100, ETH, SPX500, BTC, BNB,
  SOL, HYPE, TSLA, GOOGL, XAU, NVDA, ASTER, LINK, XRP, TRUMP and AVAX. A market trades only while its
  oracle price is fresh.
- A spot venue runs as the precompile set at `0x1021` to `0x1025`: market buys and sells with slippage
  and deadline, limit and stop orders, quotes, a liquidity vault with share tokens and TWAP. Spot fills
  settle atomically; USDL moves as the ERC20 and PAX and SID as native balances, which gain ERC20
  pointers.
- Every state change of both venues emits an EVM log, so positions, orders, fills, funding payments
  and liquidations can be rebuilt from logs alone.

### Prices

- A new price feed module and precompile at `0x1020` take prices from validator-run price sidecars that
  read the Chainflow feed. Each validator operator registers one feed key with `registerFeedKey`; the
  chain aggregates the active set's signed observations into one index and one mark price per market
  per block, with a freshness flag. Price submissions pay no fee and are bounded in size and rate.
- The price sidecar ships with the release as its own binary, with a service unit and configuration
  template under `tools/price-sidecar`.

### Hyperpax perps frontend

- The Hyperpax perps frontend trades against the precompiles: deposit, order placement, cancel, fill
  display, position management, funding display and withdrawal all resolve to precompile addresses.
- ERC20 approvals request exactly the amount of the call they precede, every market-priced write
  carries a price bound and a deadline, time-in-force and take-profit and stop-loss options reach the
  chain, and the order confirmation shows the quoted fee and liquidation price.
- Charts, prices, candles and reference order books come from the Chainflow feed; the venue's own
  fills, order book and account state come from the perps data gateway over REST, WebSocket and
  GraphQL. Funding rates are shown from the exact on-chain value, so a small negative rate reads as
  negative.
- The embedded wallet keeps signing as before, with a callback-only PKCE session.

### Perps data services

- New services in `services/platform/perps-data`: an indexer that follows the chain head, decodes
  every venue, price feed and USDL log into Postgres and publishes every row change on NATS JetStream
  with a monotonic sequence; a gateway serving markets, positions, orders, fills, funding,
  liquidations, leaderboards and account history over REST, one shared WebSocket with snapshot and
  sequence resume, and GraphQL; an explorer service with per-account and per-market trading history;
  and a portal for markets, accounts and venue statistics.
- Orders and positions that predate the indexer's start height are seeded from chain state. Account
  routes act only for the address signed in through the gateway's wallet sign-in. A replay tool
  rewrites a height range without duplicates, and every service exposes liveness, readiness and
  Prometheus metrics.

### Bridge

- The SID mint authority moves to the bridge, and SID holder balances are migrated, as part of the
  `v6.12.0` upgrade. The earlier unapplied `v6.11` plan name is retired; its work ships in `v6.12.0`.

### Release and deploy tooling

- `paxd` links the kernel library on every build path, and a node build workflow gates every change to
  the node.
- `tools/release` builds a reproducible release (`paxd`, the price sidecar, the libwasmvm runtime
  libraries, `SHA256SUMS` and the upgrade-info document), checks a published release against a local
  build, rehearses the upgrade on a local network from real chain state, and stages, swaps, snapshots,
  rolls back and verifies a fleet. See [RELEASE.md](RELEASE.md).
- Off-chain service images build and push to GHCR from `main` (`tools/deploy/offchain-images.sh`), the
  rollout tool can point a Railway service at an image pinned by digest, the launchpad services carry
  Railway config-as-code, and the launchpad and perps data services have rollout manifests.
