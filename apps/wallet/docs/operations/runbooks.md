# Operational recovery runbooks

## Upstream outage

Confirm readiness dependency status without exposing origin details, identify the failing fixed-origin proxy, and compare bounded error codes and latency. Keep signing and submission actions disabled when required truth is unavailable. Restore the dependency or route through an approved fixed origin, then verify a real response with content type, size, freshness, and chain context before clearing the incident.

## Corrupt client storage

Capture the public storage failure code and correlation reference. Optional application records are removed automatically and reported in the in-app status. Security-relevant custody choice and dApp permission corruption fails closed. Use the exact registry lifecycle reset for recovery; never clear wallet-core IndexedDB or managed authentication state as a blanket repair.

## Push-store failure

Check readiness and the durable store path, lock-file access, bounded file size, current envelope version, backup, and any quarantined corrupt file. Restore from the atomic backup only after validating every subscription or campaign through its parser. Do not print endpoints, keys, wallet addresses, or payloads. Re-run a consented subscription and revocation flow after recovery.

## Bad release

Stop promotion, preserve the failing artifact and correlation evidence, and activate the last known compatible release. Confirm CSP nonces, service-worker activation, storage schema compatibility, route availability, wallet lock behavior, and one real read-only portfolio flow before reopening actions. Do not force a service-worker skip-waiting transition during an approval or signing operation.

## Transaction reconciliation

Use the submitted hash, selected chain, account, nonce, and durable operation state; do not infer success from a local send response. Query an approved RPC and indexer until the bounded reconciliation deadline, distinguish pending, replaced, dropped, reverted, and confirmed states, and show stale or unavailable data explicitly. Never resubmit without a new user approval.

## Chain reorganization

Mark affected confirmations stale, retain the original hash and block observation, and re-query the canonical chain until the configured finality threshold is re-established. Roll back derived portfolio or activity state without rewriting the signed intent. Surface replacement, revert, and balance effects separately.

## Compromised dApp origin

Remove the origin from the approved catalog, revoke its registered permissions and tabs, block navigation and native callbacks, and invalidate any pending approvals from that origin. Review telemetry only after recursive redaction. Require a fresh explicit connection and approval after a reviewed release restores the origin.

## Forced permission revocation

Identify the exact origin, account, custody mode, and permission record. Apply the dApp lifecycle reset or remove only that origin; revoke native callbacks and close active browser tabs. Preserve wallet state, preferences, contacts, and unrelated origins. Verify that a subsequent request returns a denial and that reconnection requires explicit consent.
