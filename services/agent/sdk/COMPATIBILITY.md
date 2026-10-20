# LayerX SDK compatibility

This file is generated from `services/agent/schema/agent-api/v1.kvx`. Do not hand-edit.

| SDK version | Contract version | Node interface version | Daemon version | Support |
|---|---|---|---|---|
| `0.1.x` | `2.x` | `1.x` | `0.1.x` | supported |

Within a contract major version, schema changes must be additive. A removed or changed declaration requires a contract major-version increment. Every SDK refuses an unsupported contract major instead of guessing compatibility.

The approval module was introduced additively in contract `1.1`. SDK clients negotiated below that minor must treat approval operations and lifecycle events as unavailable; no fallback may synthesize them.

## Enforcement guarantees

All three SDKs preserve the same contract distinctions:

| Surface | Enforcement | SDK promise |
|---|---|---|
| Canonical activities, receipts, protocol budgets and protocol capability grants | protocol-enforced | The SDK preserves canonical bytes, exact result codes and proof material produced by the LayerX state machine. |
| Verification level and freshness | evidence-enforced | A value is exposed as verified only at the level justified by its attached core evidence. |
| Local policy, daemon rate limits, daemon capabilities and `DaemonLimit` spending controls | daemon-enforced | These restrictions bind only while `layerx-agentd` is in the path. Bypassing the daemon bypasses them; they are not protocol guarantees. |
| Approval holds and decisions | daemon-enforced | The SDK preserves the approval disclosure, state, decision outcome and lifecycle event exactly. Approval confers no protocol authority, and bypassing the daemon bypasses the restriction. |
| Caller idempotency | protocol-and-daemon coordinated | The caller key remains byte-identical across retries; `Unknown` remains explicit until receipt lookup resolves it. |

The generated `guarantees.md` in each dynamic-language SDK contains the schema-derived restriction rows. The authored Rust SDK exposes the same invariants through `layerx_sdk::GUARANTEES`.

## Offline receipt verification

A counterparty needs only the offline export bytes and the standalone `layerx-proof` verifier. No daemon, node, network connection or local LayerX database is consulted:

```text
cargo run --manifest-path services/agent/Cargo.toml --locked -p layerx-proof --example offline_verify -- EXPORT_PATH
```

The export carries the exact receipt, inclusion proofs, batch headers and checkpoint certificates. Verification fails closed if evidence is missing, malformed or does not justify the claimed verification level. The TypeScript and Python `offline-receipt-verification` examples preserve the same proof-bearing read boundary; they do not turn an unverified value into a result.

## Release regeneration gate

`make agent-test-sdk-compat` regenerates and diffs the SDK outputs, validates additive schema compatibility, checks this matrix and the guarantee documents, and requires the same five example scenarios in TypeScript and Python.
