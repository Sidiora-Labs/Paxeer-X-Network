# layerx-migrate

Ethereum and Solana source-chain verifiers for the `migration` interop adapter. This crate imports provenance against pinned source deployments. It does not write LayerX balances and it is not the C17 genesis cutover.

Operator contract: [`OPERATIONS.md`](OPERATIONS.md). Workspace index: [`../../README.md`](../../README.md).

Wiki page: [`docs/wiki/Migration.md`](../../../docs/wiki/Migration.md). LayerX kernel genesis import sections (different surface): [`migrations/`](../../../migrations/README.md).

The crate also builds the `layerx-migrate` binary.

From the monorepo root: `make interop-test-migration`. The ignored live-testnet suite is `make interop-test-migration-testnets`; it needs an operator environment and runs in CI only through the manually dispatched `.github/workflows/interop-migration-testnets.yml`.
