# Local gateway lifecycle tests

This standalone Cargo package (`layerx-gateway-local-qualification`) runs the
gateway against real local processes: gateway, identity, receipt authority, TLS
Redis, core boundary, sequencer and authority replica. The `lifecycle` test
target drives [`../lifecycle-boundary.sh`](../lifecycle-boundary.sh) unchanged.
It provisions a local identity through the identity API and obtains a
signer-bound API key through `POST /v1/keys`; it never inserts gateway key
records directly. All keys, tokens and certificates are generated locally.

[`build.rs`](build.rs) includes the core `Cluster` fixture from the binary named
by `LAYERX_TEST_CORE_BIN`; no native build is performed by this package.

Prerequisites: Linux root (the real daemon runs under its separate UID),
OpenSSL, TLS-enabled `/usr/bin/redis-server`, curl, jq, xxd and Python 3. Build
`build/bin/layerxd`, `build/bin/layerx-genesis-build` and the platform binaries
`layerx-core-boundary`, `layerx-identity`, `layerx-receipt-authority` and
`layerx-gateway` first, then from the repository root:

```sh
export LAYERX_TEST_SERVICE_BIN_DIR=/absolute/path/to/platform/debug
export LAYERX_TEST_CORE_BIN="$LAYERX_TEST_SERVICE_BIN_DIR/layerx-core-boundary"
cargo test --manifest-path platform/hosted/gateway/tests/local/Cargo.toml \
  --test lifecycle local_gateway_lifecycle -- --exact --nocapture
```

The first Cargo invocation resolves this package's lockfile; later runs can use
`--locked`. Each real-node fixture removes its generated state root when
dropped; set `LAYERX_TEST_RETAIN_STATE` to keep it for inspection.

Other tests in the `lifecycle` target include `local_gateway_rpc` (`POST /rpc`),
`local_gateway_websocket_receipt_wake` (authenticated `GET /rpc/ws`),
`local_gateway_account_sequence_matches_authenticated_account`,
`local_gateway_committed_payment_reads`,
`local_gateway_successful_send_latency`,
`local_gateway_grant_draw_and_subscription_renewal_latency`,
`local_gateway_program_events_read_the_web_request`,
`local_gateway_paid_withdrawal_preserves_fees_across_restart` and
`local_gateway_legacy_genesis_refuses_withdrawal_without_mutation`.

The receipt verifier is a separate binary,
`gateway-lifecycle-receipt-verify` ([`verifier.rs`](verifier.rs)), built on
`layerx-proof` with a locally pinned sequencer key and the manifest of locally
signed canonical activities. It requires network 7332, fetches the authority's
batch-header signature and receipt inclusion proof over authenticated TLS,
verifies them, and resolves batch identity, asset and state roots through
`layerx_platform_authority::authorized_batch_by_activity`.
`gateway-lifecycle-sdk-verify` ([`lifecycle_sdk.rs`](lifecycle_sdk.rs)) uses
the `layerx-sdk` program lifecycle types.
