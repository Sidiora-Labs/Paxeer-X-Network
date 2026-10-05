# layerx-mirror

Publishes LayerX batch archives to Ethereum and Solana, and verifies those archives. Pure archives: commitments plus retrievable data. No vault, no portal, no custody.

Settlement stays on the Paxeer X chain. See the workspace index: [`../../README.md`](../../README.md).

| Binary | Role |
| --- | --- |
| `layerx-mirror-publisher` | `cargo run --locked --manifest-path interop/Cargo.toml -p layerx-mirror --bin layerx-mirror-publisher -- <config.json>` |
| `layerx-mirror-verify` | `cargo run --locked --manifest-path interop/Cargo.toml -p layerx-mirror --bin layerx-mirror-verify -- <config.json>` |

Example configurations: [`config.example.json`](../../deploy/mirror/config.example.json) for the publisher, [`verify-config.example.json`](../../deploy/mirror/verify-config.example.json) for the verifier.

On-chain programs: `interop/contracts/ethereum-mirror/`, `interop/contracts/solana-mirror/`. Remote signer framing: [`../../deploy/mirror/signer-protocol.md`](../../deploy/mirror/signer-protocol.md).

From the monorepo root: `make interop-build`, `make interop-test`, `make interop-test-mirrors` (this crate plus both mirror contracts). Operator live targets: `make mirror-live`, `make mirror-verify-live`.
