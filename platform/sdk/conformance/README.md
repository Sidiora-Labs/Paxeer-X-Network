# SDK conformance suite

Shared vectors and tests that the Paxeer X Network SDKs for the LayerX kernel
domain must pass: TypeScript, Python and Rust under [`agent/`](../../../agent),
and Go, JVM, Swift and .NET under [`platform/sdk`](..).

## Contents

| Path | Purpose |
| --- | --- |
| `secret-hygiene.test.{ts,py,rs}` | Secret hygiene: `SecretBytes` key material and tokens stay out of logs, errors and serialized output; `IdempotencyKey` validation; integer-only `ProtocolAmount`. |
| `streaming-resumability.test.{ts,py,rs}` | Resumable streaming: bounded opaque cursors and a no-gap, no-duplicate event chain across reconnection. |
| `terminal-v4.test.py` | Python program-terminal and applied-leg verification against the `receipt-programs-*-v4.json` fixtures and `receipt-programs-executed-v3.json`. |
| `operations.json` | The 128 agent-plane and human-plane operations with method, path, request and response types, idempotency and body flags; consumed by the SDK generator in [`../generators`](../generators). |
| `mirror-v2.json` | Mirror archive framing, Ethereum getters, Solana manifest and chunk layout, finality rules and the required accept and refuse cases; consumed by the SDK generator. |
| `run-go.sh`, `run-jvm.sh` | Go and JVM runners: generator drift check, then the SDK tests and conformance main. |
| `fixtures/` | Receipt, refusal, native-program lifecycle, capability, account-derivation and intent-plan vectors, with their generators. |

## Running

The whole suite runs from the repository root:

```bash
make platform-test-sdks
```

`platform-test-sdks` runs `platform-verify-sdks` (see
[`platform/Makefile.inc`](../../Makefile.inc)). It first checks the native
lifecycle, capability and executed-program fixtures for drift, then runs the
Rust `layerx-sdk` tests, the TypeScript and Python conformance tests, the Go and
JVM runners, and the Swift and .NET builds and tests. It needs `pytest`, `swift`
and `dotnet` on the PATH.

Individual pieces, from the repository root:

```bash
PYTHONPATH=agent/sdk/python python3 -m pytest --import-mode=importlib \
  platform/sdk/conformance/secret-hygiene.test.py \
  platform/sdk/conformance/streaming-resumability.test.py
sh platform/sdk/conformance/run-go.sh "$PWD"
sh platform/sdk/conformance/run-jvm.sh "$PWD"
```

## Native program lifecycle fixtures

`native-program-deploy-v3.json`, `native-program-upgrade-v3.json`, and the four
`native-program-wind-down-*-v3.json` fixtures contain payloads and signed
protocol-3 activities produced by
[`tests/programs/test_call_activity.c`](../../../tests/programs/test_call_activity.c).
SDK tests decode and re-encode the C bytes and bind the activity identifiers and
idempotency keys. These are wire-layout vectors, not execution evidence.

Regenerate with `make programs-native-lifecycle-fixtures`; check byte-for-byte
drift with `make programs-check-native-lifecycle-fixtures`.

## Frozen ABI 2 capability fixture

`fixtures/native-program-capabilities-v2.json` comes from the Rust runtime's
`capability_fixture` example, not from an SDK encoder. Generate it with
`make programs-generate-capability-fixture`; check reproducibility with
`make programs-check-capability-fixture` (also required by `platform-verify-sdks`).

Each SDK constructs the logical grants from the fixture and compares its encoded
bytes with the runtime output. The fixture covers all ten tags, equal and
amount-decreasing narrowing, amount escalation refusal, and refusal to substitute
a BalanceView receipt digest. Canonical order is `1,2,3,4,5,9,6,10,7,8`, not
numeric tag order. ProgramSpend encodes its bounded seed length as `u16be`; the
separate derived-account hash uses `u32be`. Neither layout changes with freezing.

## Program receipt fixtures

`receipt-programs-positive-v3.json` is a receipt-codec vector re-enveloped from
v2. It is not runtime execution evidence. `receipt-programs-executed-v3.json`
comes from the native CALL transition and the Rust Wasm runtime. Run
`make programs-executed-fixture` to generate it and
`make programs-check-executed-fixture` to check drift. Its Python packager
requires `cryptography`, verifies the original signed evidence, and does not
re-sign or alter receipt fields. The fixture is deterministic local transition
evidence, not external finality or checkpoint-inclusion evidence. The v4
executed fixture has the matching `make programs-executed-v4-fixture` and
`make programs-check-executed-v4-fixture` targets.

## Conformance requirements

Every published SDK must:

1. **Secret hygiene**: pass the `secret-hygiene.test.*` checks.
2. **Resumable streaming**: pass the `streaming-resumability.test.*` checks.
3. **Integer-only money**: reject floating-point `ProtocolAmount` values.
4. **Idempotency keys**: require an idempotency key on mutations.
5. **Local verification**: ship receipt, batch-inclusion and checkpoint
   verification that needs no trust in hosted surfaces.

When adding a requirement, write the test for TypeScript, Python and Rust
(`.test.ts`, `.test.py`, `.test.rs`) with identical semantics, wire it into
`platform-verify-sdks`, and update this README.

## Language notes

- **TypeScript**: `SecretBytes` is a class with a private byte field that is
  zeroed with `Uint8Array.fill(0)`; batch-inclusion, Merkle and checkpoint
  verification functions are async.
- **Python**: `SecretBytes` redacts its representation, destroys its value in
  `__del__`, and `__reduce__` raises `TypeError` so it cannot be pickled.
- **Rust**: `SecretBytes` does not implement `Clone` and zeroizes on `Drop` via
  the `zeroize` crate.
- **JVM**: see [`../jvm/README.md`](../jvm/README.md).
