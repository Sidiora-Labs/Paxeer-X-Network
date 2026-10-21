<!-- Generated from services/human/schema/human-api/errors.kvx and services/agent/schema/agent-api/errors.kvx by services/platform/docs/build/build_site.py. Do not hand-edit. -->

# Error reference

Both contracts refuse with a typed shape. No operation on either plane returns an unstructured error, and no SDK converts a refusal into a success.

## Human API

Every failure response carries one typed shape: a stable machine code, the copy-catalog key naming the human message, the trace identifier on the envelope, and a retriability classification. No operation returns an unstructured error.

| Machine code |
|---|
| `unauthenticated` |
| `session-expired` |
| `step-up-required` |
| `forbidden` |
| `not-found` |
| `invalid-request` |
| `conflict` |
| `rate-limited` |
| `cursor-expired` |
| `unavailable` |
| `upstream-degraded` |
| `challenge-expired` |
| `refused-by-policy` |
| `refused-by-budget` |
| `refused-by-capability` |
| `refused-by-protocol` |
| `refused-by-limit` |
| `quote-expired` |
| `wallet-not-bound` |
| `exit-unavailable` |
| `already-decided` |
| `hold-expired` |
| `hold-defective` |
| `archive-needs-disposition` |
| `confirmation-mismatch` |
| `not-suppressible` |
| `support-unavailable` |
| `support-conversation-unknown` |
| `support-message-unknown` |

### Retriability

| Class | Meaning |
|---|---|
| `retriable` | The same request may be retried as-is. Surfaces offer Retry. |
| `retriable-after` | The same request may be retried once the carried retry_after_ms has elapsed. Rate-limit refusals always classify here and always carry the timing. |
| `structural` | Retrying the same call cannot help until surrounding state changes, such as re-authentication or a reload. Surfaces offer Reload. |
| `final` | The outcome is settled. The plane never retries into a different outcome and surfaces state the result honestly. |

## Agent API

| Error class |
|---|
| `TransportFailure` |
| `Deadline` |
| `ProtocolIncompatibility` |
| `UnavailableCapability` |
| `CoreRejection` |
| `VerificationFailure` |
| `PolicyRefusal` |
| `CapabilityRefusal` |
| `BudgetRefusal` |
| `RateLimit` |
| `IdempotencyConflict` |
| `InternalFault` |

### Verification levels

| Level |
|---|
| `Unverified` |
| `SequencerSigned` |
| `BatchIncluded` |
| `StateProven` |
| `CheckpointFinalised` |
| `SettlementAnchored` |

The agent contract orders verification levels by declaration order: a later level implies every earlier one, and no layer reports a level its evidence does not justify.
