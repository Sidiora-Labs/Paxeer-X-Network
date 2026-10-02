# Migration source verifier operations

`EthereumVerifier` and `SolanaVerifier` are the production source boundaries used by `MigrationAdapter`. Construct them from `EthereumConfig` or `SolanaConfig`; both configuration types deserialize from JSON. The verifier must remain alive for verification and the subsequent history-cursor commit.

Each source and rollback-anchor quorum requires two to eight independent HTTPS JSON-RPC endpoints and a strict majority. Endpoint URLs must use DNS names and HTTPS. Every endpoint declaration requires an `independent_backend` label for the operator-audited backend identity. Both `(host, port)` and normalized backend identities must be unique, so aliases, alternate request paths, and duplicate labels cannot manufacture quorum membership. The DER trust anchor and bearer credential paths must be absolute. Bearer credentials must be regular files with no group or other permissions; invalid paths or permissions refuse construction before any network request.

Protected Ethereum, Solana, and rollback-anchor endpoint declarations use this closed shape; omitting `independent_backend` is a configuration error:

```json
{
  "url": "https://rpc-a.example/source",
  "ca_certificate_der": "${LAYERX_MIGRATION_SECRET_DIR}/rpc-ca.der",
  "bearer_token_file": "${LAYERX_MIGRATION_SECRET_DIR}/rpc-a.token",
  "independent_backend": "operator-a"
}
```

Ethereum configuration pins the chain ID, genesis block, custody contract, immutable runtime or explicit proxy implementation, runtime hashes at the source block, ABI selector, event topic, and the location of every custody field. Solana configuration pins the genesis hash, custody program, immutable ProgramData account and code hash, loader, custody and token authorities, account owners, instruction discriminator, account indices, byte offsets, and integer encoding. No default custody schema or deployment identifier exists.

The journal directory is canonical absolute local durable storage protected by an HMAC key file. The key path must be canonical and absolute with no symlinked component, resolve directly to a regular file, and have no group or other permissions. The namespace directory must likewise be canonical with no symlinked component, have the same owner as the key, and have no group or other permissions; newly created directories and record/seal files use modes `0700` and `0600`. Every existing record and seal must be a direct regular file owned by that identity with no group or other permissions before replay reads it. Unsafe, case-normalized, relative, symlinked, foreign-owned, or accessible storage is refused. Its head is additionally reconciled on every read and append against a strict-majority HTTPS authority. That authority must provide linearizable, durable implementations of:

- `layerx_getMigrationJournalHead([anchor_id])`, returning `{sequence,digest}`.
- `layerx_advanceMigrationJournalHead([{anchor_id,expected_sequence,expected_digest,sequence,digest}])`, performing an authenticated compare-and-swap and returning the committed `{sequence,digest}`.

The authority must never move a head backwards and must retain heads independently of the verifier host. Missing, divergent, rolled-back, or unauthenticated head state makes the verifier fail closed. Journal authentication keys, RPC bearer tokens, and authority credentials must be readable only by the service identity.

Account mapping requires a wallet-signed, bounded ownership claim and a deployment-specific `BindingReceiptPolicy`. Asset migration requires an exact custody claim and a deployment-specific `CustodyReceiptPolicy`. Both policies pin the independently trusted sequencer public key and verify the resulting protocol receipt, including authority, module and operation coordinates, exact balance effects, and an external-claim context commitment. The production plane receives only adapter-created execution requests with canonical idempotency keys and cannot substitute its own batch signer.

History pages are external provenance. They are prepared in the authenticated journal, stored through `ExternalHistorySink`, then committed through the same verifier. A sink must durably deduplicate by chain, native transaction identifier, address, asset, and kind before returning success. It must never translate an imported record into a LayerX activity or receipt.

`history::DurableExternalHistory` implements that sink using the existing authenticated journal and rollback-anchor protocol. Construct it with a dedicated `JournalConfig`; each stored page is one atomic `external-history` update containing canonical records with the `LXP/ExternalHistory/v1` domain. Admission validates the complete page before applying it, treats identical records as replay, and refuses conflicting facts for the same source identity. Principal-scoped reads return only `ExternalHistoryRecord` values, including their closed Ethereum/Solana provenance label, and accept page limits of 1–256. The continuation cursor orders record identities within the current durable state; it is not a frozen snapshot or a source-chain evidence token. Reopen and every read reconcile the existing HMAC/hash-chain journal with its independent rollback anchor. Retain the history journal, its authentication key and anchor identity together; deleting local data is not a reset of the durable anchor.

The live history cases additionally require `LAYERX_ETHEREUM_HISTORY_STORE_CONFIG` and `LAYERX_SOLANA_HISTORY_STORE_CONFIG`, each naming a protected `JournalConfig` JSON file. Each must name a fresh, dedicated qualification namespace and rollback-anchor id, distinct from its source verifier's journal. Supply genuine nonempty source history evidence. The fixture stores and replays those verified pages, reopens the real sink, checks principal isolation and bounded pagination, and corrupts private copies of the resulting journal to exercise integrity, permissions and rollback refusal. It does not modify the original journal for those refusal cases or synthesize history records. The workflow receives these configurations from `ETHEREUM_HISTORY_STORE_CONFIG_B64` and `SOLANA_HISTORY_STORE_CONFIG_B64`; they use the same protected-path substitution as the existing verifier configurations. Source verification and external history storage do not establish that account binding or custody credit executed on LayerX.

The protected `migration-testnets` GitHub environment supplies exact deployed configuration, evidence envelopes, credentials, journal keys, and trust anchors to `.github/workflows/interop-migration-testnets.yml`. The workflow is manually dispatched because it reads live test networks and durable operator state. It exercises the production verifiers through `make interop-test-migration-testnets`.
