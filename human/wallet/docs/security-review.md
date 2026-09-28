# Attestor security review

This review covers the threshold library the attestor daemon depends on and the daemon itself under `human/wallet/attestor`. It was done by reading the code. Every statement cites the file it comes from.

## Scope

- The library: `github.com/getamis/alice`, pinned in `human/wallet/attestor/go.mod` at commit `d8fd6861d3b2`. The last tagged release before that commit is `v1.0.8` (commit `96ba067b74a7`).
- The daemon: `cmd/attestor`, `internal/server`, `internal/transport`, `internal/policy` (with `evm` and `lx`), `internal/auth`, `internal/store`, `internal/tss` and `internal/dealer`.

## Library delta

### Method

Every file that differs between `v1.0.8` and the pinned commit was diffed and read. The CGGMP, FROST, refresh, Birkhoff interpolation and echo broadcast files are listed below, whether they changed or not.

The external audit reports shipped at the library root predate `v1.0.8`. The library's own security notes list fixes made between the audit and `v1.0.8`: the Ring-Pedersen Fiat-Shamir challenge, the no-small-factor proof, and the TSSHOCK class of attacks. Those fixes were not diffed file by file against the audited revision. Finding F-12 records that gap.

### Changed files

- `crypto/birkhoffinterpolation/birkhoffinterpolation.go`
  - What changed: `CheckValid` became the private `checkRecoverable`. A new `ValidateThresholdScheme(threshold, fieldOrder)` runs `checkRecoverable` and then `CheckThresholdSecrecy`. `CheckThresholdSecrecy` rank-tests every subset of size threshold − 1 against the unit vector of the constant term, and returns the new `ErrBelowThresholdRecovery` when any such subset spans it.
  - Before the change, a participant could declare an (x, rank) pair, such as a rank-one share next to a rank-zero share at a related x, that lets fewer than threshold shares recover the secret.
  - Soundness: unchanged. Equivocation defence: unchanged. Share secrecy: strengthened.
- `crypto/tss/dkg/0_peer_handler.go`
  - What changed: the peer handler validates the collected (x, rank) set with `ValidateThresholdScheme`. This handler is also the DKG behind FROST (`crypto/tss/eddsa/frost/dkg/dkg.go`).
  - Soundness: unchanged. Equivocation defence: unchanged. Share secrecy: strengthened.
- `crypto/tss/ecdsa/cggmp/dkg/0_peer_handler.go`
  - What changed: the same switch to `ValidateThresholdScheme` for CGGMP key generation.
  - Soundness: unchanged. Equivocation defence: unchanged. Share secrecy: strengthened.
- `crypto/tss/ecdsa/cggmp/sign/round_1.go` and `crypto/tss/ecdsa/cggmp/signSix/round_1.go`
  - What changed: signing validates the signer set with `ValidateThresholdScheme` before computing coefficients.
  - Soundness: unchanged. Equivocation defence: unchanged. Share secrecy: strengthened, because a signer set that would disclose the secret is refused.
- `crypto/tss/ecdsa/addshare/newpeer/0_peer_handler.go`
  - What changed: a new participant validates the quorum's (x, rank) set with `ValidateThresholdScheme`.
  - Soundness: unchanged. Equivocation defence: unchanged. Share secrecy: strengthened.
- `crypto/tss/recovery/recover_private_key.go` and `crypto/bip32/master/0_initial_handler.go`
  - What changed: both switched to `ValidateThresholdScheme`. The daemon uses neither.
  - Soundness, equivocation defence and share secrecy: no effect on the daemon.
- `crypto/birkhoffinterpolation/threshold_secrecy_test.go`, `crypto/tss/dkg/dkg_bks_test.go` and a new CGGMP DKG peer handler test
  - What changed: tests for the new check. Test-only files, with no effect on the three properties.

### Unchanged files

- `crypto/tss/eddsa/frost/signer/round_1.go` and the rest of the FROST signer: unchanged. The signer uses only `ComputeBkCoefficient` and does not call the new check. The daemon closes that gap for imported shares (finding F-3). Shares from key generation are already checked by the DKG handler above.
- `crypto/tss/ecdsa/cggmp/refresh/*.go`: unchanged. Refresh round three checks only `ValidatePublicKey`.
- `types/message/msg_main_echo.go` and `types/message/msg_main.go`: unchanged.
  - Votes are counted per authenticated sender.
  - A relay must carry either a 32-byte hash or exactly the reduced echo content; anything else is `ErrInvalidRelay`.
  - An unconfirmed hash is corrected by the authenticated origin.
  - A differing hash after confirmation is `ErrDifferentHash`, which aborts the session.
  - Effect: a malicious relayer can only abort a session. It cannot make honest peers accept two different messages.

### Pin decision

The daemon keeps the pinned commit `d8fd6861d3b2`, and `go.mod` is unchanged.

- The pinned commit is `v1.0.8` plus the threshold-secrecy check. Every change strictly adds a refusal, and no previously checked input becomes accepted.
- No library defect found in this review requires a different pin.
- Moving back to `v1.0.8` would remove the check that the import path now relies on (`ValidateThresholdScheme` in `internal/server/keys.go`).

## Daemon trust boundaries

### Gateway

- **How it connects:** over mutual TLS to the API listener.
- **What it can cause:**
  - It can submit one signing request per node with each live identity token it holds, and every node signs what the policy allows for that user (finding F-6, partially fixed).
  - It can withhold or delay requests.
  - It can start refresh sessions with a subset of nodes. A refresh commits only when every participant has staged the new share, so a partial start leaves every node at the old epoch (finding F-5, fixed).
- **What it cannot cause:**
  - It cannot call import, refresh or add-share. Those routes accept only a client certificate chaining to the operator CA (finding F-4, fixed).
  - It cannot sign without a token that verifies against the configured JWKS, names the configured issuer and audience, is unexpired and younger than the maximum age.
  - It cannot reuse a token: each node records the token's hash in its share store and refuses a second request with it, across restarts (finding F-6, partially fixed).
  - It cannot sign a kernel kind outside the kernel policy. `lx_activity`, `lx_bind` and `lx_grant` pass through the kernel evaluator, and without a kernel policy they are refused (finding F-7, fixed).
  - It cannot sign a send authorization outside the kernel policy. `lx_send_authorization` signs only the owner authorization digest of an unsigned asset send, under the `lx_activity` policy and authority rules, and its amount counts once when the completed send is signed as `lx_activity`.
  - It cannot sign outside the per-kind policy. `eth_sign_digest` now recomputes its digest from the construction (finding F-1, fixed).
  - It cannot record a foreign owner or account on a new participant (finding F-2, fixed).
  - It cannot import shares that fall outside the custody scheme (finding F-3, fixed).
  - It cannot read shares; they are sealed at rest with AES-GCM under the node key, with associated data (`internal/store`).

### Attestor peers

- **How they connect:** over TLS 1.3 with a required client certificate, identified by SPKI pin (`internal/transport/tls.go`).
- **What one peer can cause:**
  - It can abort any session it participates in: protocol mismatch, invalid messages, or equivocation, which the echo broadcast catches.
  - It can delay a session by going silent, until the round deadline aborts it with `session_timeout` and a `session.stalled` audit entry naming the silent peer (finding F-11, fixed).
  - It can fill only its own share of the pending-session buffer: pending sessions it opens and messages it queues are capped per peer, and excess answers 503 (finding F-11, fixed).
  - During refresh, it can withhold its staged acknowledgement and abort the refresh, but it cannot leave nodes in different share epochs short of a crash between acknowledgement and commit (finding F-5, fixed).
  - It can inflate an account's recorded spend by announcing requests, which only tightens the caps (finding F-8, fixed).
- **What one peer cannot cause:**
  - It cannot impersonate another peer. A non-relay message whose id differs from the authenticated sender is `ErrSenderMismatch`.
  - It cannot relay outside secp256k1 key generation, signing and refresh sessions.
  - It cannot stall `Flush` with a duplicate original: expected relays are counted once per sender and round.
  - It cannot sign with a share from another epoch: the committed epoch is bound into the session protocol name, so signers at different epochs fail closed.
  - It cannot relay for itself, for the receiver, or for a non-participant.
  - It cannot learn a share: every protocol message is a zero-knowledge-backed library message.
  - Below the threshold of three signers, it cannot produce a signature.

### Identity provider

- **What it can cause:** whoever controls the JWKS the daemon loads can mint tokens for any subject, within the policy for that subject.
- **What it cannot cause:**
  - It cannot exceed the policy.
  - It cannot sign for an account whose share is recorded under a different owner. The owner is checked against the stored share (`internal/server/sign.go`).
  - A missing JWKS answers `token_unavailable` and nothing is signed.

### Operator

- **How it connects:** over mutual TLS to the API listener, with a client certificate chaining to the operator CA.
- **What it can cause:**
  - It can import dealer bundles, when the ceremony flag is enabled.
  - It can run refresh and add-share.
  - It can read health.
  - It can replace the policy file and the JWKS on disk.
- **What it cannot cause:**
  - It cannot call keys.generate or sign. Those routes accept only a client certificate chaining to the gateway CA (finding F-4, fixed).
  - It cannot extract a share through the API; no route returns share material.
  - It cannot import a bundle whose threshold, ranks, participants or interpolation scheme break the custody scheme (finding F-3, fixed).

### Dealer

- **What it can cause:**
  - The dealer sees the whole secret while splitting and is trusted to wipe it (`internal/dealer`).
  - It is also trusted for share consistency. `ValidatePublicKey` checks only that the pseudoinverse combination of partial keys equals the public key, not that every partial key lies on one polynomial of the threshold's degree (finding F-13).
- **What it cannot cause:**
  - It cannot place shares outside the custody scheme. The import checks threshold three, rank zero, configured participant ids, and `ValidateThresholdScheme` (finding F-3, fixed).

## Policy fail-closed paths

Each path below refuses the request, and no signature is produced. The order is the order of evaluation.

### `internal/server`

1. Client certificate chain does not verify: refused at the TLS layer.
2. Chain root is not the CA the route needs (gateway CA for keys.generate and sign, operator CA for keys.import, keys.refresh and keys.addshare): `operator_required`.
3. Malformed JSON or unknown fields: `session_bad_request`.
4. Unknown kind: `session_bad_request`.
5. Missing or invalid token, a token older than `ATTESTOR_JWT_MAX_AGE`, a token already used on this node, or no JWKS loaded: `token_unavailable` or `token_invalid`.
6. Agent credential presented without `ATTESTOR_AGENTS_FILE`: `token_unavailable`. With it, an unregistered or frozen principal, a bad signature, an expired request, an expiry beyond `ATTESTOR_AGENT_MAX_EXPIRY` or a nonce already recorded in the store: `agent_invalid`.
7. No policy file loaded: `no_policy`. No kernel policy loaded, for a kernel kind: `no_policy`.
8. Unknown key id, or owner differs from the stored share: refused before any session.
9. `eth_sign_digest` without a construction, or with an unknown construction kind: `session_bad_request` (finding F-1, fixed).
10. Transaction bytes that do not decode, a foreign chain id, or an unprotected legacy transaction: refused by `evm.DecodeTransaction`.
11. Policy or kernel policy decision deny: `policy_denied`, carrying the policy code.
12. Any denial or failure whose audit append fails: `store_audit_failed`. Every refusal is written to the fsynced audit log before the response is sent (finding F-10, fixed).
13. Allowed-sign audit append fails: refused before the session starts.
14. Fewer than three participants record the announced request in their spend ledgers: `quorum_too_few_signers`, before any session.
15. Session failure, timeout, round deadline or protocol mismatch, including an epoch mismatch between signers: a session error, and nothing is returned.

### `internal/policy`

1. Kind not in the document's allowed kinds: denied.
2. Per-transaction cap, or rolling 24-hour cap per asset, exceeded: denied.
3. Request rate exceeded: denied.
4. Destination on the deny list, or absent from a present allow list: denied.
5. Precompile selector not allowed: denied.
6. Sponsored batch or EIP-7702 authorisation whose recomputed digest differs from the claimed digest: `digest_mismatch`.
7. Chain id that differs from the configured chain: denied.
8. EIP-712 data that fails strict parsing: denied.
9. Calldata that does not round-trip through the embedded ABIs: denied.
10. Unknown inspector for a kind: denied.

### Paths that do not fully fail closed

- The kernel activity disclosure is derived by the node from the envelope, because the sign request carries no disclosure field. The match against the human service's disclosure therefore happens at the human service, not at the node.
- Destinations are not limited when a document has no allow list.
- Calls to unknown contracts count only their native value (`evm.DecodeCalldata` returns an unknown call).
- `personal_message` has no content policy.

## Findings

### F-1: bare digest signing bypassed policy

- **Severity:** blocker. **Status:** fixed.
- **Evidence:** before this change, `eth_sign_digest` in `internal/server/sign.go` accepted any 32-byte digest and inspected nothing. Anyone holding a user token could therefore get a signature over the digest of any transaction, which bypassed every cap and destination rule.
- **Fix:**
  - The request must now carry the sponsored batch or EIP-7702 authorisation construction.
  - The daemon recomputes the digest from it through `internal/policy/evm`.
  - The request is evaluated under the sponsored batch or authorisation policy kind.
  - A mismatch is `digest_mismatch`.
- **Test:** `TestFiveNodeEndToEnd` in `internal/server/e2e_test.go`.
  - It signs a real authorisation and recovers the address.
  - It proves that a bare digest is refused.
  - It proves that a transaction digest presented as an authorisation or as a sponsored batch is refused.
- **Owner action:** none in the daemon. The gateway client must send the construction (observation recorded).

### F-2: add-share did not bind owner and account across the quorum

- **Severity:** high. **Status:** fixed.
- **Evidence:**
  - `doAddShare` in `internal/server/keys.go` recorded the owner and account sent to the new participant, without the quorum agreeing on them.
  - A caller could therefore give the new node a foreign owner and have it sign for that owner.
  - Quorum nodes also did not compare the requested owner with their stored share.
- **Fix:**
  - Each quorum node refuses an owner that differs from its stored share.
  - Every node derives a binding hash over key id, curve, public key, owner, account, new participant and quorum, and places it in the session protocol name (`runBoundSession` in `internal/server/session.go`).
  - Nodes told different values see a protocol mismatch, and the session fails.
- **Test:** `TestAddShareBindsOwnerAcrossParticipants`. A foreign owner given to the new participant fails the session and stores nothing, and a single quorum node refuses a foreign owner. The honest add-share signs with the new participant.
- **Owner action:** none.

### F-3: import accepted shares outside the custody scheme

- **Severity:** high. **Status:** fixed.
- **Evidence:**
  - `doImport` checked only share-to-partial-key consistency.
  - Bundles with a lower threshold, a non-zero rank, a foreign participant id, or an (x, rank) layout that discloses the secret below the threshold were accepted.
  - The FROST signer never runs the library's secrecy check.
- **Fix:** `checkImportedScheme` in `internal/server/keys.go` requires all of the following, and refuses otherwise with `key_invalid_share`:
  - the dealer threshold
  - configured participant ids
  - rank zero
  - `ValidateThresholdScheme` from the pinned library
- **Test:** `TestImportRefusesSharesOutsideTheCustodyScheme`. A two-of-five bundle and a rank-one disclosure layout are refused with nothing stored, and a three-of-five bundle is accepted.
- **Owner action:** none.

### F-4: gateway and operator share one client CA

- **Severity:** high. **Status:** fixed.
- **Evidence:** before this change, `cmd/attestor/main.go` built the API listener with the operator CA as its only client CA, and `post` in `internal/server/server.go` checked only that the chain verified.
- **Fix:**
  - `LoadClientAuthorities` in `internal/server/server.go` loads the gateway CA from `ATTESTOR_TLS_CA_FILE` and the operator CA from `ATTESTOR_OPERATOR_CA_FILE`.
  - `operatorOnly` admits keys.import, keys.refresh and keys.addshare only when the verified chain ends at the operator CA. `gatewayOnly` admits keys.generate and sign only when it ends at the gateway CA.
  - A refusal answers `operator_required` with status 403 and is audited.
- **Test:** `TestHandlerRefusals` and `TestFiveNodeEndToEnd` in `internal/server`, and `TestRunSeparatesOperatorAndGatewayAuthority` in `cmd/attestor`, refuse each identity on the other's routes.
- **Owner action:**
  - Issue gateway and operator certificates from separate CAs. A bundle placed in both variables grants both authorities.
  - Keep the ceremony flag off outside ceremonies.

### F-5: refresh is not atomic across nodes

- **Severity:** high. **Status:** fixed.
- **Evidence:** before this change, refresh in `internal/server/keys.go` overwrote the stored share when the local session completed, so a peer or gateway that let some nodes finish and others fail left nodes in different epochs.
- **Fix:**
  - Each participant stages the new share beside the committed one (`PutStaged` in `internal/store`).
  - The first participant in sorted order coordinates. It commits only after every participant acknowledges its stage over `/v1/peer/refresh`, and otherwise aborts.
  - Participants swap epochs in one transaction on commit (`CommitStaged`) and discard the stage on abort or on a missing decision (`DiscardStaged`). A node that restarts with an uncommitted stage discards it and audits the discard.
  - Refresh and signing sessions bind the committed epoch into the protocol name, so signers at different epochs fail closed.
- **Residual:** a node that crashes after acknowledging but before receiving the commit stays at the old epoch. Signing with it fails closed, and a new refresh brings it back.
- **Test:** `TestRefreshKeepsTheOldEpochWhenAParticipantStopsBeforeCommit` in `internal/server/durability_test.go` stops one node between stage and commit, shows every node at the old epoch with no stage, and signs at the old epoch after the node returns. It then shows a signer at a different epoch cannot sign. `TestStagedShareCommitsOrDiscardsWhole` covers the store.
- **Owner action:** take the backup snapshot before each refresh.

### F-6: identity tokens are bearer tokens

- **Severity:** high. **Status:** partially fixed.
- **Evidence:**
  - `internal/auth/jwt` verifies the token but does not bind it to the request.
  - The gateway relays the token, so a compromised gateway holding a live token can sign one request per node within policy for that user.
  - This conflicts with the decision that the gateway cannot sign.
- **Fix:**
  - Tokens older than `ATTESTOR_JWT_MAX_AGE` (default one hour) are refused.
  - Each node records the SHA-256 of the token together with a digest of the canonical signing request (method, key id and the request body with session id, kind, signers and payload) in its share store (`TokenReplayStore`, `internal/store/replay.go`) and refuses the identical request under the same token, across restarts, with `token_invalid` and an audit entry that names the replay. Distinct requests under one token are accepted until the token expires or passes the maximum age.
- **Test:** `TestTokenAuthorisesDistinctRequestsAndRefusesARepeatAcrossRestart` and `TestTokenOlderThanMaximumAgeRefused` in `internal/auth/jwt`, `TestTokenRequestRecordsAreScopedToTheRequest` in `internal/store`, `TestOneTokenAuthorisesDistinctRequestsAndRefusesARepeat` in `internal/server`, and the repeated request in `TestFiveNodeEndToEnd`.
- **Reviewed:** tokens are now request-scoped against replay but still not proof-of-possession bound; a party holding a live token can issue new distinct requests with it.
- **Owner decisions:**
  - Binding a token to a device key with a request-bound proof is not implemented. Until it is, a gateway holding a fresh token can still spend it on a request of its choosing.
  - One token authorises every distinct signing request of a session, such as the binding message and the binding transaction of one provisioning, while an identical request under the same token is refused.
  - Whether to require a step-up for signing kinds above a threshold.

### F-7: kernel kinds are not inspected

- **Severity:** high before kernel activation. **Status:** fixed.
- **Evidence:** before this change, `lx_activity`, `lx_bind` and `lx_grant` used an empty inspector in `internal/server/sign.go`, and `lx.New` was not wired in `cmd/attestor/main.go`.
- **Fix:**
  - The empty inspectors are removed. Every kernel kind is evaluated by the `internal/policy/lx` evaluator: modules, operations, caps, destinations, actor and authority, validity window, bind address and the chain's current bind nonce, and grant caps.
  - `cmd/attestor/main.go` builds the evaluator from `ATTESTOR_KERNEL_POLICY_FILE` and `ATTESTOR_RPC_URL` on the same engine and ledger. Without a kernel policy, every kernel kind is refused with `no_policy`.
- **Test:** `TestFiveNodeEndToEnd` signs a kernel activity and a binding under the evaluator, and refuses an activity for another identity and a stale bind nonce.
- **Owner action:** write the kernel policy before the kernel kinds are allowed in any policy document.

### F-8: policy ledger is in memory and per node

- **Severity:** medium. **Status:** fixed.
- **Evidence:** before this change, the ledger reset on restart and each node saw only the spends it co-signed, so the effective daily cap could reach five thirds of the configured cap.
- **Fix:**
  - `SpendLedger` in `internal/policy` keeps each account's spends and requests, with their rolling windows, sealed in the store.
  - A signer announces every allowed request to every participant of the key over `/v1/peer/announce`. The others record it, and the session starts only when at least three participants hold the record. Entries carry the request id, so a request announced by several signers counts once.
- **Test:** `TestSpendLedgerHoldsAcrossSignerSetsAndRestarts` spreads spends over three signer sets and shows a fourth set refused at the daily cap, before and after a restart. `TestSpendLedgerCapsHoldAcrossRestart` covers the ledger alone.
- **Owner action:** `cmd/attestor/main.go` still passes an in-memory ledger; the server replaces it with the store ledger on the same clock (observation recorded).

### F-9: permits and replay windows

- **Severity:** medium. **Status:** fixed; one owner decision remains.
- **Evidence:**
  - EIP-712 permits counted no token spend, only the verifying contract as destination. They now count their value, or the maximum amount for an allowed-style permit, against the cap of the token they permit, and the spender is checked as a destination (`TestPermitCountsAgainstTheTokenCap`).
  - Agent expiry had no upper bound and agent nonces lived only in memory.
- **Fix:**
  - Agent expiry beyond `ATTESTOR_AGENT_MAX_EXPIRY` (default five minutes) is refused.
  - Agent nonces are recorded in the share store (`AgentNonceStore`) and survive restarts.
  - `cmd/attestor/main.go` wires the agent verifier from `ATTESTOR_AGENTS_FILE`.
- **Test:** `TestAgentNonceRefusedAcrossRestart` and `TestAgentExpiryBeyondMaximumRefused` in `internal/auth/agent`.
- **Owner action:**
  - Decide how the agent principal file is replicated to each node.

### F-10: denial audit records can be lost

- **Severity:** low. **Status:** fixed.
- **Evidence:** before this change, denial paths in `internal/server/sign.go` and failure paths in `internal/server/keys.go` discarded the audit append error.
- **Fix:**
  - Every refusal on every route, including authority refusals, is appended to the audit log before the response is sent. The log fsyncs each record.
  - When the append fails, the response is `store_audit_failed` instead of the original refusal.
- **Test:** `TestHandlerRefusals` checks that refusals advance the audit head and that a closed log turns refusals into `store_audit_failed`.
- **Owner action:** alert on `store_audit_failed` answers and on readiness failures.

### F-11: peer-driven stalls

- **Severity:** low. **Status:** fixed.
- **Evidence:** before this change, a duplicate original made `Flush` wait for relays that never arrived, and one peer could fill the pending-session buffer.
- **Fix:**
  - Expected relays are counted once per sender and round.
  - Each peer may open at most its share of the pending sessions and queue at most its share of messages per pending session; excess is `ErrPeerQuota` and answers 503.
  - A round deadline aborts a session in which no message arrives in time, with `session_timeout` and a `session.stalled` audit entry naming the silent peers.
- **Test:** `TestPendingBufferBoundedPerPeer` and `TestStalledSessionNamesTheSilentPeer` in `internal/transport`, and `TestStalledPeerAbortsTheSessionWithAnAuditEntry` in `internal/server`.
- **Owner action:** none.

### F-12: audit-to-tag gap in the library

- **Severity:** medium. **Status:** open.
- **Evidence:** the shipped audit reports predate `v1.0.8`. The security fixes between them are described in the library notes but were not diffed in this review.
- **Owner action:** identify the audited revision and review the diff to `v1.0.8`, or commission a review of the pinned commit.

### F-13: dealer consistency is trusted

- **Severity:** note. **Status:** open.
- **Evidence:** `ValidatePublicKey` does not prove that the partial keys lie on one polynomial of the threshold's degree. The import scheme check (F-3) limits the layout but not the polynomial.
- **Owner action:** run the dealer only in the documented ceremony, and follow each import with a refresh.

### F-14: add-share through the daemon could not complete

- **Severity:** medium. **Status:** fixed.
- **Evidence:**
  - `doAddShare` gave each contributing node a network that listed the new participant as a peer.
  - `refresh.AddShare` requires every contributor peer to hold a share in the bundle, so every daemon add-share failed with a participant mismatch.
  - No route could grow the participant set.
- **Fix:** contributors run add-share over `contributorNet` in `internal/server/session.go`. It keeps the new participant in the transport session but leaves it out of the peer list the protocol checks against the bundle.
- **Test:** `TestAddShareBindsOwnerAcrossParticipants` completes an honest add-share through five daemons and a sixth, and signs with the new participant.
- **Owner action:** none.

## Areas without findings

- **Echo broadcast and relays:** no finding. Relays are admitted only for the three secp256k1 session kinds with a verified origin, and equivocation aborts the session.
- **Share storage and backup:** no finding. Shares are sealed with AES-GCM under the node key, with associated data. Backups are sealed under a separate backup key (`internal/store`).
- **Transaction decoding:** no finding. Foreign chain ids and unprotected legacy transactions are refused, and the signing digest comes from the chain's latest signer (`internal/policy/evm`).
