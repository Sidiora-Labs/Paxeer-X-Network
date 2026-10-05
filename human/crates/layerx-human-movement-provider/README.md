# Movement provider

This Unix-socket daemon implements movement protocol 2, including typed planning, durable quote and exact-transaction preparation, custody-backed EIP-1559 submission, external claim signature verification, transaction recovery, deposit proof re-verification and checkpoint registration verification. The human service owns receipt-gated journey advancement and principal authorization. The provider never treats an admission or transaction hash as payment completion.

Custody is the Paxeer X chain custody precompile at `0x…1013`; finalized batch roots are read from the anchor precompile at `0x…1014`. Neither needs a configured address.

## Commands

| Command | Effect |
| --- | --- |
| none | Opens the journal, binds the socket and serves |
| `probe` | Asks the running provider for readiness; only its `Ready` answer succeeds |
| `validate-config` | Parses and validates the configuration, then exits |
| `--publish-deposit-proof <transaction> <checkpoint> <account>` | Builds a deposit proof for a custody deposit from the configured endpoints and writes `deposit-<transaction>.bin` into the evidence root once at least the minimum number of endpoints agree on it |

The provider answers `Ready` only while the journal is readable, the evidence root is a protected directory, a movement execution authority is configured and passes the authenticated executor probe within the deadline, and at least `PAXEER_MINIMUM_AGREEMENT` Paxeer origins answer on the configured chain. It logs the first failing condition.

## Planning and execution

The human service resolves aliases, currency and principal ownership before calling this provider. The versioned contract carries typed accounts/assets, amount and route, actor/authority, custody key and opaque provider reference, principal/tenant, the receipt-backed wallet, network/protocol, sequence, relationship, fee bounds, validity window and authority evidence. The provider constructs scoped stable plan/quote identifiers and typed plans using the existing route resolver. Raw caller aliases never supply signing authority.

Successful plan and unsigned-transaction responses are journaled before reply and replay exactly. Quote loading remains principal-scoped in the human service. Preparing a transaction obtains a quorum pending nonce and accounts for the provider's durable reservations. The human service checks every returned economic and gas field, then registers the exact transaction in KMS. The executor can only sign or recover an existing authorization through its separately pinned TLS identity. Raw private keys remain inside KMS. An acknowledgement gap reuses the original signed bytes and hash; it never creates a new nonce or economic action. Withdrawal lookup is read-only.

External claim signatures are verified by KMS against the exact authorized EIP-1559 preimage and bound wallet, with low-s enforcement, before raw transaction bytes are recorded. Calldata and raw signed transactions occupy separate contract fields. Deposit, withdrawal and exit submissions retain principal/tenant/account/wallet/originating-plan identity.

## Configuration

Every variable below has prefix `LAYERX_HUMAN_MOVEMENT_PROVIDER_`.

| Suffix | Meaning |
| --- | --- |
| `MODE` | `movement` enables custody-backed paths; `evidence-only` serves evidence reads and refuses planning/execution |
| `SOCKET` | Absolute Unix socket path, normally `/run/layerx/human/movement.sock` |
| `ALLOWED_UID`, `ALLOWED_GID` | Exact kernel identity of human-service client |
| `MAX_FRAME_BYTES` | 2–1048576 |
| `DEADLINE_SECONDS` | 1–60, enforced by absolute socket watchdog |
| `STATE_ROOT` | Private provider-owned durable journal directory |
| `EVIDENCE_ROOT` | Existing private provider-owned candidate directory |
| `PAXEER_RPC_URLS` | JSON array of 2–8 independent HTTPS endpoints |
| `PAXEER_CA_DER` | Absolute private DER CA file |
| `PAXEER_CHAIN_ID` | Explicit nonzero EVM chain identity |
| `PAXEER_MINIMUM_AGREEMENT` | At least 2, at most endpoint count |
| `PAXEER_CONFIRMATIONS` | Nonzero finality depth |
| `PAXEER_CHECKPOINT_REGISTRY` | 0x-prefixed 20-byte address of the deposit-root checkpoint registry |
| `PAXEER_CHECKPOINT_AUTHORITY` | Public Ed25519 deposit registration authority, 0x-prefixed 64 hex digits |
| `CUSTODY_REFERENCE` | Nonzero custody domain, 0x-prefixed 64 hex digits |
| `CUSTODY_PROFILE`, `CUSTODY_PROFILE_SHA256` | Protocol 3 only: private native custody profile file, validated for the network, and its SHA-256; protocol 3 requires chain ID 125 |
| `NETWORK_ID`, `PROTOCOL_VERSION` | LayerX kernel network and exact protocol 2 or 3 |
| `POLL_SECONDS`, `DELAYED_AFTER_POLLS` | Poll cadence 1–60 and nonzero stall threshold |
| `CHECKPOINT_INTERVAL_SECONDS`, `PAXEER_BLOCK_SECONDS`, `REMINDER_INTERVAL_SECONDS` | Explicit nonzero settlement estimate and reminder inputs |
| `KMS_ENDPOINT`, `KMS_SERVER_NAME`, `KMS_PROVIDER_REFERENCE` | Required in movement mode; socket address, TLS server name, existing provider identity |
| `KMS_CA_DER`, `KMS_CLIENT_CERT_DER`, `KMS_CLIENT_KEY_DER` | Required in movement mode; private absolute DER paths for executor mTLS |

The executor certificate must match KMS `LAYERX_HUMAN_KMS_EVM_CLIENT_CERT_DER`, distinct from its human-service certificate. The client uses `LAYERX_HUMAN_MOVEMENT_SOCKET`, `_PEER_UID`, `_PEER_GID`, `_MAX_FRAME_BYTES` and `_DEADLINE_SECONDS`. Socket parents must be daemon-owned 0700/0750 with the daemon GID and no symlinks; socket mode is 0660. Cross-UID clients need shared-group traversal/access. HTTPS production finality requires two independent endpoint votes, so at least two Paxeer RPC endpoints must be configured.

## Evidence and persistence

Private regular singly-linked candidate files are bounded to 1 MiB and encoded as native movement responses:

- `deposit-<transaction hex>.bin`: `DepositProof(Ok(candidate))`, normally written by `--publish-deposit-proof`. Fresh quorum observations and the deposit verifier check the custody precompile deposit, the Ed25519 registration authority and Merkle inclusion.
- `withdrawal-<withdrawal ID hex>.bin`: `WithdrawalMaterial(Some(material))`, served only for a debit recorded in the journal and only after the anchor precompile confirms, with the configured endpoint agreement, that the batch named by the material's sequencer-signed header is finalized with exactly that header's state and receipt roots.
- `exit-<account hex>-<asset hex>.bin`: `ExitPlan(candidate)`. The evidence must match the authorized account, asset, wallet and finalized balance and pass `EmergencyExit::construct_claim` before a newly scoped plan is returned.

Missing or invalid material never produces a successful proof or readiness response. A codec round-trip proves structure only; cryptographic signature rejection is tested at the real settlement boundary.

The journal uses private fsynced atomic snapshots, canonical records, explicit protocol binding, capacity bounds and an exclusive process lock. It refuses corruption, conflicting action identities and uncertain writes. Fresh finality/proof reads are reobserved after restart. Only immutable successful planning and transaction-preparation responses are replayed as stored.

## Tests

Unit tests are in `src/tests.rs`; `tests/startup.rs` and `tests/probe.rs` exercise the binary and the probe command, using the KMS helpers in `tests/kms`. Run them with `cargo test --locked --manifest-path human/Cargo.toml -p layerx-human-movement-provider`.
