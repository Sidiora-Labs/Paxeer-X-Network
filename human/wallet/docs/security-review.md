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
  - It can submit any signing request with a user's live identity token, and every node signs anything the policy allows for that user (finding F-6).
  - It can call the import, refresh and add-share routes, because its client certificate is accepted on the same terms as the operator's (finding F-4).
  - It can withhold or delay requests.
  - It can start refresh sessions with a subset of nodes (finding F-5).
- **What it cannot cause:**
  - It cannot sign without a token that verifies against the configured JWKS.
  - It cannot sign outside the per-kind policy. `eth_sign_digest` now recomputes its digest from the construction (finding F-1, fixed).
  - It cannot record a foreign owner or account on a new participant (finding F-2, fixed).
  - It cannot import shares that fall outside the custody scheme (finding F-3, fixed).
  - It cannot read shares; they are sealed at rest with AES-GCM under the node key, with associated data (`internal/store`).

### Attestor peers

- **How they connect:** over TLS 1.3 with a required client certificate, identified by SPKI pin (`internal/transport/tls.go`).
- **What one peer can cause:**
  - It can abort any session it participates in: protocol mismatch, invalid messages, or equivocation, which the echo broadcast catches.
  - It can stall `Flush` until the protocol timeout by sending a duplicate original, since expected relays grow per original received (finding F-11).
  - It can fill the pending-session buffer (64 sessions, 256 messages each, 30-second TTL). Other senders then get 503 answers and retry (finding F-11).
  - During refresh, it can leave nodes in different share epochs (finding F-5).
- **What one peer cannot cause:**
  - It cannot impersonate another peer. A non-relay message whose id differs from the authenticated sender is `ErrSenderMismatch`.
  - It cannot relay outside secp256k1 key generation, signing and refresh sessions.
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

- **What it can cause:**
  - It can import dealer bundles, when the ceremony flag is enabled.
  - It can run refresh and add-share.
  - It can read health.
  - It can replace the policy file and the JWKS on disk.
- **What it cannot cause:**
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
2. Malformed JSON or unknown fields: `session_bad_request`.
3. Unknown kind: `session_bad_request`.
4. Missing or invalid token, or no JWKS loaded: `token_unavailable` or `token_invalid`.
5. Agent credential presented: no agent verifier is wired in `cmd/attestor/main.go`, so the answer is `token_unavailable`.
6. No policy file loaded: `no_policy`.
7. Unknown key id, or owner differs from the stored share: refused before any session.
8. `eth_sign_digest` without a construction, or with an unknown construction kind: `session_bad_request` (finding F-1, fixed).
9. Transaction bytes that do not decode, a foreign chain id, or an unprotected legacy transaction: refused by `evm.DecodeTransaction`.
10. Policy decision deny: `policy_denied`, carrying the policy code.
11. Allowed-sign audit append fails: refused before the session starts.
12. Session failure, timeout or protocol mismatch: a session error, and nothing is returned.

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

- `lx_activity` and `lx_grant` are registered with an inspector that performs no cap or destination checks. The kernel evaluator in `internal/policy/lx` exists but is not wired in `cmd/attestor/main.go` (finding F-7).
- Denial audit writes discard their error (`internal/server/sign.go`). The request is still refused, but the denial record can be lost (finding F-10).
- Destinations are not limited when a document has no allow list.
- Calls to unknown contracts count only their native value (`evm.DecodeCalldata` returns an unknown call).
- `personal_message` has no content policy.
- EIP-712 permits count no token spend (finding F-9).

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

- **Severity:** high. **Status:** open.
- **Evidence:**
  - `cmd/attestor/main.go` builds the API listener with the operator CA as its only client CA.
  - `post` in `internal/server/server.go` checks only that the chain verified.
  - The gateway certificate can therefore call import (behind the ceremony flag), refresh and add-share.
- **Owner action:**
  - Issue gateway and operator certificates from separate CAs.
  - Gate the operator routes on the operator identity.
  - Keep the ceremony flag off outside ceremonies.

### F-5: refresh is not atomic across nodes

- **Severity:** high. **Status:** open.
- **Evidence:**
  - Refresh in `internal/server/keys.go` overwrites the stored share when the local session completes.
  - A peer or gateway that lets some nodes finish and others fail leaves nodes in different epochs.
  - Fewer than three nodes may then agree, and the key can be lost.
- **Owner action:** design a two-phase refresh: keep the old share until every participant confirms the new epoch, with the backup snapshot taken before each refresh.

### F-6: identity tokens are bearer tokens

- **Severity:** high. **Status:** open.
- **Evidence:**
  - `internal/auth/jwt.go` verifies the token but does not bind it to the request.
  - The gateway relays the token, so a compromised gateway holding a live token can sign anything within policy for that user.
  - This conflicts with the decision that the gateway cannot sign.
- **Owner action:** require a request-bound proof from the user device, or a step-up for signing kinds above a threshold.

### F-7: kernel kinds are not inspected

- **Severity:** high before kernel activation. **Status:** open.
- **Evidence:**
  - `lx_activity`, `lx_bind` and `lx_grant` use an empty inspector in `internal/server/sign.go`.
  - `lx.New` in `internal/policy/lx` is not wired in `cmd/attestor/main.go`.
- **Owner action:** wire the kernel evaluator before the kernel kinds are allowed in any policy document.

### F-8: policy ledger is in memory and per node

- **Severity:** medium. **Status:** open.
- **Evidence:**
  - The ledger in `internal/policy` resets on restart.
  - With three of five signers, each node sees only the spends it co-signs, so the effective daily cap can reach five thirds of the configured cap.
- **Owner action:**
  - Persist the ledger.
  - Set caps with the five-thirds factor, or share spend records among nodes.

### F-9: permits and replay windows

- **Severity:** medium. **Status:** open.
- **Evidence:**
  - EIP-712 permits count no token spend, only the verifying contract as destination.
  - Agent credential expiry in `internal/auth/agent.go` has no upper bound.
  - The agent nonce cache is in memory, so a request can be replayed after a restart. The agent path is not wired, so this is latent.
- **Owner action:**
  - Decode permit amounts into the cap ledger.
  - Bound agent expiry.
  - Persist nonces before wiring agents.

### F-10: denial audit records can be lost

- **Severity:** low. **Status:** open.
- **Evidence:** denial paths in `internal/server/sign.go` discard the audit append error. The request is still refused.
- **Owner action:** surface audit failures in health and alert on them.

### F-11: peer-driven stalls

- **Severity:** low. **Status:** open.
- **Evidence:**
  - A duplicate original makes `Flush` wait for relays that never arrive, until the protocol timeout.
  - One peer can fill the pending-session buffer (`internal/transport/session.go`).
  - Both cause only delay and retries.
- **Owner action:**
  - Count expected relays per distinct original.
  - Cap pending sessions per peer.

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
