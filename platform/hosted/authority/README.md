# Receipt authority

`layerx-platform-authority` builds the `layerx-receipt-authority` binary. It
serves verified authorised-batch facts for the LayerX kernel over TLS: receipt
bytes come from the kernel's LNI unix socket, header and receipt signatures are
checked against a pinned sequencer key, and inclusion evidence comes from an
independent receipt-authority replica. The gateway, registry and webhooks
services call it with their own bearer tokens. An optional, separately
configured group of `/v1/agent/*` routes answers the human agent contract used
by `RemoteHumanAuthority` in
[`agent/crates/layerx-agentd/src/human_runtime.rs`](../../../agent/crates/layerx-agentd/src/human_runtime.rs).

Sources: [`src/main.rs`](src/main.rs) (service, routes, environment),
[`src/human.rs`](src/human.rs) (human agent routes and principal policy),
[`src/human/dynamic.rs`](src/human/dynamic.rs) (subject-scoped requests),
[`src/human/budget_state.rs`](src/human/budget_state.rs) (budget state and
proof export), [`src/protected.rs`](src/protected.rs) (protected file reads).

## Build and test

The crate is a member of the [`platform`](../../Cargo.toml) Cargo workspace.

```sh
cargo build --locked --manifest-path platform/Cargo.toml -p layerx-platform-authority
cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-authority --test human_tls
```

Integration tests live in [`tests/`](tests): `human_tls.rs` starts the real
binary and drives the real `RemoteHumanAuthority` client against it with a
private CA; `real_node.rs`, `budget_proof_exports.rs` and
`maintenance_identity.rs` exercise the service against recorded fixtures in
[`tests/fixtures`](tests/fixtures).

## Service routes and configuration

All routes are GET. `/livez` and `/readyz` are unauthenticated; `/readyz`
reports `ready`, `network_id`, `protocol_network_id` and `wire_version`. The
bearer-protected routes are:

| Route | Purpose |
| --- | --- |
| `/v1/authorized-batches/by-activity/{activity_id}` | Verified authorised-batch facts for an activity |
| `/v1/authorized-batches/wait-by-activity/{activity_id}` | Same, waiting for a publication notification |
| `/internal/v1/activities/{activity_id}/authority` | Internal authority lookup |
| `/v1/batches/{batch_id}/receipt-authority?receipt_digest={digest}` | Relayed to the replica unchanged |

Incoming requests are limited to 16 KiB. The service takes no arguments and
reads its configuration from the environment; `--help` prints the same list:

| Variable | Meaning |
| --- | --- |
| `LAYERX_AUTHORITY_LISTEN` | Listen address (defaults to all interfaces) |
| `LAYERX_AUTHORITY_TLS_CERT_DER`, `LAYERX_AUTHORITY_TLS_KEY_DER` | Server certificate and PKCS#8 private key, DER |
| `LAYERX_AUTHORITY_CLIENT_CA_DER` | Optional; a presented client certificate must chain to it |
| `LAYERX_AUTHORITY_TOKEN_FILES` | Colon-separated files, one service bearer token each |
| `LAYERX_AUTHORITY_REPLICA_URL`, `LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE`, `LAYERX_AUTHORITY_REPLICA_ID` | Loopback receipt-authority replica, its bearer token and its 64-hex identity |
| `LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE` | Optional distinct bearer allowed only on the receipt-authority relay route |
| `LAYERX_AUTHORITY_LNI_SOCKET` | LNI unix socket used as the receipt source |
| `LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID` | Protocol network id expected in the LNI handshake |
| `LAYERX_AUTHORITY_NETWORK_ID`, `LAYERX_AUTHORITY_WIRE_VERSION` | Values echoed in every answer; the wire version must equal the built protocol version |
| `LAYERX_AUTHORITY_SEQUENCER_ID`, `LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY` | Pinned sequencer identity and key |
| `LAYERX_AUTHORITY_FIRST_BATCH`, `LAYERX_AUTHORITY_LAST_BATCH` | Inclusive authorised batch range |
| `LAYERX_AUTHORITY_GENESIS_TRUST`, `LAYERX_AUTHORITY_HANDOVER_FINALITY` | Protected native genesis trust artifact and the independent Paxeer verification policy it requires |

## Human agent contract

### Client transport and encoding

`RemoteHumanAuthority` sends every request as GET over HTTPS with
`Authorization: Bearer <token>` and no body. Every route has required `tenant`
and `principal` query parameters. The constructor requires an `https://`
endpoint, a token of at least 32 bytes, a nonzero deadline, a response limit in
`1..=1_048_576` bytes and a private CA certificate in DER; it removes trailing
slashes from the endpoint.

In the tables, `H32` means a JSON string of 64 hexadecimal characters encoding
32 bytes, with no `0x` prefix. `HEX` means a nonempty, even-length hex string of
at most 2,097,152 characters, also bounded by the response limit. `U64`, `U16`,
`U8` and `U32` are unsigned JSON integers. `DEC128` is a JSON string holding an
unsigned base-10 `u128`.

The client treats every non-success HTTP status, including 503, as
`HumanOperationError::Refused`; it does not parse the error body or
`Retry-After`. Transport failure or failure reading the body produces
`HumanOperationError::Unavailable`. Invalid JSON also produces `Refused`.

### Routes

All paths have the prefix `/v1/agent/`.

| Route | Additional query parameters | Response fields |
| --- | --- | --- |
| `registry` | None | `modules`: array of `{module_id: U16, activity_types: [U32]}` |
| `authorized-batch` | `activity_id`: H32 | `batch_id`, `asset`, `previous_state_root`, `resulting_state_root`, `sequencer_public_key`: H32 |
| `balance-context` | None | `account_id`, `asset_id`, `sequencer_id`, `sequencer_public_key`: H32; `currency`, `observed_at`: strings; `age_seconds`, `maximum_age_seconds`, `first_batch_number`, `last_batch_number`: U64 |
| `identity` | `did` | `authorities`: array of `{kind, id: H32}`; `canonical_core_bytes`: HEX; `head_sequence`, `revocation_sequence`: U64; `verification_level`: string; `frozen`: boolean |
| `core-clock` | None | `lower_unix_ms`, `lower_sequence`, `upper_unix_ms`, `upper_sequence`, `observed_head_sequence`: U64; `canonical_attestation`: HEX |
| `capability-scope` | `did`; `authority`, `action_key`, `capability_id`: H32 | `activity_types`: [U16]; `counterparties`, `assets`: [H32]; `amount_ceiling`: DEC128; `expiry_sequence`, `observed_sequence`: U64; `enforceable_dimensions`: [string]; `verification`: U8; `evidence_digest`: H32 |
| `budget-state` | `budget_id`: H32 | `revocation_sequence`, `observed_head_sequence`, `age_sequences`, `maximum_age_sequences`: U64; `verification`: U8; `evidence_digest`, `receipt_digest`, `checkpoint_digest`, `asset`: H32; `remaining`: DEC128 |
| `budget-proof` | `budget_id`: H32 | Budget state proof export |
| `key-policy` | `did`; `recovery`: `true` or `false` | `policy_revision`, `required_delay_seconds`, `maximum_delay_seconds`, `effective_sequence`, `observed_head_sequence`, `age_sequences`, `maximum_age_sequences`: U64; `verification`: U8; `evidence_digest`, `checkpoint_digest`: H32 |

Registry module IDs are the closed set in
[`agent/crates/layerx-types/src/payload.rs`](../../../agent/crates/layerx-types/src/payload.rs)
(`Asset` = 1 through `Web` = 11); each module lists at most 64 activity
types (`MAX_MODULE_ACTIVITY_TYPES` in
[`limits.rs`](../../../agent/crates/layerx-types/src/limits.rs)).

Capability installation in the client checks `evidence_digest` against SHA-256
of six length-prefixed parts (four-byte big-endian length each): ASCII
`layerx-human/agent-create/agent-evidence/v1`, byte `05`, `action_key`,
`capability_id`, `observed_sequence` (8 bytes big-endian) and `verification`
(one byte).

### Subject-scoped requests

When a request carries any of `subject_principal`, `owner_did`,
`owner_account`, `asset_id`, `registration` or `signed_activity`, the server
handles it in subject-scoped mode ([`src/human/dynamic.rs`](src/human/dynamic.rs)).
That mode requires all of `tenant`, `principal`, `subject_principal`,
`owner_did`, `owner_account` and `asset_id`, resolves the subject through the
identity binding service, and adds a `subject-context` route. In this mode
`authorized-batch` also requires `signed_activity`, `budget-proof` also requires
`did`, and `registration` is accepted only on `identity`, `key-policy`,
`capability-scope`, `authorized-batch` and `budget-proof`.

### Server configuration

| Variable | Required configuration |
| --- | --- |
| `LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE` | Protected file holding 32..4096 bytes in `0x21..0x7e`; must differ from every service token |
| `LAYERX_AUTHORITY_HUMAN_AGENT_TENANT`, `LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL` | Nonempty tenant and principal bound to the token |
| `LAYERX_AUTHORITY_PRINCIPAL_POLICY_FILE` | Protected principal policy, schema below |
| `LAYERX_AUTHORITY_MODULE_REGISTRY_FILE` | Protected module registry file, also read by the gateway |
| `LAYERX_AUTHORITY_CORE_CLOCK_HORIZON` | Positive u64 sequence horizon |
| `LAYERX_AUTHORITY_STATE_ROOT` | Existing absolute canonical directory owned by the process UID with no group or other permissions, holding retained receipt records |
| `LAYERX_AUTHORITY_IDENTITY_BINDING_SOCKET`, `LAYERX_AUTHORITY_IDENTITY_BINDING_UID`, `LAYERX_AUTHORITY_IDENTITY_BINDING_GID` | Optional as a set; identity binding socket and its expected peer UID and GID, used by subject-scoped requests |

When the token file variable is absent and none of the others is set, the
service runs without the human routes and they return 503
`human_authority_unconfigured`. Setting any of the others without the token file
prevents startup. The bearer is compared in constant time; a wrong or missing
bearer gets 401 `identity_required`, a mismatched `tenant` or `principal` gets
403 `principal_mismatch`, an unknown route gets 404, and unexpected or missing
query keys get 400 `invalid_query`.

Protected files are read through [`src/protected.rs`](src/protected.rs): the
path must be absolute and canonical, and the file must be a regular file owned
by the process UID with no group or other permissions. The policy is parsed and
pinned by SHA-256 at startup; every authenticated request re-reads it and
compares the digest, and a missing, invalid or changed policy returns 503
`policy_unavailable`, so changing the policy requires a restart. Verified
activity lookups are retained as JSON records under the state root.

Server-side 503 refusal codes include
`budget_revocation_and_checkpoint_evidence_unavailable` (budget state with no
LNI-backed proof), `identity_state_proof_unavailable`,
`capability_state_proof_unavailable`,
`key_policy_checkpoint_evidence_unavailable`, `budget_state_proof_unavailable`
and `budget_proof_export_unavailable`.

### Principal policy schema

All fields are required and unknown fields are rejected. H32 fields are 64-digit
hexadecimal identifiers. The file is an object with `principals`, an array of:

```text
{
  tenant: string,
  principal: string,
  account_id: H32,
  asset_id: H32,
  activities: [H32],
  budgets: [H32],
  maximum_age_seconds: U64,
  maximum_age_sequences: U64,
  identities: [{
    did: string,
    authorities: [{kind: primary_key|session_key|capability_grant, id: H32}],
    revocation_sequence: U64,
    frozen: boolean,
    evidence: {activity_id: H32, receipt_digest: H32},
    capabilities: [{
      authority: H32,
      action_key: H32,
      capability_id: H32,
      activity_types: [U16],
      counterparties: [H32],
      assets: [H32],
      amount_ceiling: unsigned decimal string,
      expiry_sequence: U64,
      enforceable_dimensions: [activity_type|counterparty|asset|amount|rate|purpose|expiry],
      evidence: {activity_id: H32, receipt_digest: H32}
    }],
    rotation: KeyPolicy,
    recovery: KeyPolicy
  }]
}
KeyPolicy = {
  policy_revision: U64,
  required_delay_seconds: U64,
  maximum_delay_seconds: U64,
  effective_sequence: U64,
  evidence: {activity_id: H32, receipt_digest: H32}
}
```

`authorized-batch` refuses an activity that is not in the principal's
`activities` list (403 `activity_not_bound`), and the budget routes refuse a
budget that is not in `budgets` (404 `budget_not_bound`).
