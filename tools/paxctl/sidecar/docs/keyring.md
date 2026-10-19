# Sidecar keyring backend

The sidecar opens a Cosmos SDK keyring at startup when configured to do so.
The keyring is the entry point for all transaction-signing tasks (governance
votes, software-upgrade proposals, deposits — Component C of the in-pod
governance signing design).

This document covers the operator-facing contract. The full design — including
the controller-side Secret projection that materializes the keyring directory
in the pod — lives in the [in-pod-governance-signing design](https://github.com/sei-protocol/bdchatham-designs/blob/main/designs/governance-signing/in-pod-governance-signing.md) (relocated to the bdchatham-designs repo per Design 05 / PLT-497).

## Environment contract

| Env | Values | Default | Required when |
|---|---|---|---|
| `SEI_KEYRING_BACKEND` | `test` \| `file` \| `os` | unset (governance signing disabled) | sign-tx tasks in use |
| `SEI_KEYRING_DIR` | absolute path | `<home>/keyring-file` where `<home>` is the `--home` flag (defaults to `$SEI_HOME`) | required for `file` |
| `SEI_KEYRING_PASSPHRASE` | string | unset | `backend == file` |

For the `file` backend, a trailing `/keyring-file` path segment is stripped before handoff to the SDK — the Cosmos SDK keyring re-appends `keyring-file/` internally, so callers passing `/sei/keyring-file` and `/sei` both end up at `/sei/keyring-file/*`. This matches both operator mental models.

Unset `SEI_KEYRING_BACKEND` is the default: the sidecar starts normally
and rejects sign-tx submissions with `keyring not configured`. The node's
genesis-ceremony tasks (e.g. `generate-gentx`) continue to function — those
use a separate, in-process test backend that is intentionally isolated from
production keys (see "Genesis isolation" below).

Unknown values for `SEI_KEYRING_BACKEND` cause the sidecar to refuse to start.
KMS / HSM / Vault / remote-signer backends are not supported yet.

## Fail-fast semantics

When `SEI_KEYRING_BACKEND` is set, the sidecar:

1. Reads `SEI_KEYRING_BACKEND`, `SEI_KEYRING_DIR`, `SEI_KEYRING_PASSPHRASE`.
2. Validates the backend value and (for `file`) the presence of a passphrase.
   A missing passphrase on the file backend is a startup error — operators
   see a clear `SEI_KEYRING_PASSPHRASE required when SEI_KEYRING_BACKEND=file`
   message and the pod CrashLoopBackOffs.
3. Opens the keyring through the Cosmos SDK.
4. Runs a structural liveness check (`kr.List()`) with a bounded retry of 3
   attempts, 2 seconds between attempts. The retry absorbs the rare kubelet
   Secret-mount race where the projected file is briefly absent.
5. Wipes `SEI_KEYRING_PASSPHRASE` from the process environment so the secret
   no longer appears in `/proc/<pid>/environ` for the lifetime of the
   container.

An empty keyring is a permitted outcome of the smoke test — the sidecar
trusts that callers will supply key names that exist when they submit
sign-tx tasks, and surfaces missing-key errors at that point.

## Trust model

- The passphrase lives in the process env between `os.Getenv` and
  `os.Unsetenv` — a window of a few milliseconds at startup.
- The passphrase is never logged. The sidecar logs `keyring opened` with the
  backend and directory only.
- Errors returned from the keyring-open path are scrubbed of any verbatim
  occurrence of the passphrase before they leave the function.
- The keyring directory is mounted read-only; the sidecar never writes to it.
- Operators must rotate the passphrase by re-projecting the Secret and
  restarting the sidecar pod (see the operator runbook in the design doc).

## Genesis isolation

`sidecar/tasks/generate_gentx.go` continues to use `keyring.BackendTest`
unconditionally. The gentx validator key is throwaway by design and must not
share state with the operator's production keyring. The two code paths share
no keyring object and live in different packages.

## Operator runbook

See the "Operator runbook: creating the Secrets" section of the
[in-pod-governance-signing design](https://github.com/sei-protocol/bdchatham-designs/blob/main/designs/governance-signing/in-pod-governance-signing.md) for the end-to-end flow:
generating the keyring locally, building the projected Kubernetes Secret,
and wiring it to the validator CRD.
