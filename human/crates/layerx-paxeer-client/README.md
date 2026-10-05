# layerx-paxeer-client

Typed Paxeer X chain JSON-RPC custody, finality, withdrawal and emergency-exit boundaries for the human services. Production finality requires agreement from at least two independent HTTPS endpoints. Local disposable-chain configurations are explicit.

Custody is the native custody precompile at `0x…1013` (`CUSTODY_PRECOMPILE`); finalized batch state and receipt roots are read from the anchor precompile at `0x…1014` (`ANCHOR_PRECOMPILE`).

Deposit proofs are verified against finalized custody receipts, canonical Merkle inclusion and the configured checkpoint authority. `WithdrawalBoundary` reads the custody and anchor precompiles, constructs the claim from verified withdrawal material, and verifies the evidence before payout. `state_proof` and `exit` cover the emergency-exit path. Codec admission checks canonical structure only.

## Tests

```sh
cargo test --locked --manifest-path human/Cargo.toml -p layerx-paxeer-client
```

`tests/withdraw.rs` drives the real `WithdrawalBoundary` over HTTP against an in-process JSON-RPC server that answers the custody and anchor precompile views, using a real bound withdrawal fixture. `tests/custody_abi.rs` checks the custody ABI against `tests/vectors/custody_abi.json`. `tests/finality.rs`, `tests/status.rs` and `tests/deposit.rs` start a disposable Anvil process and need `anvil` installed.
