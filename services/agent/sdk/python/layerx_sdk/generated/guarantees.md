# Generated SDK guarantees

This file is generated from `services/agent/schema/agent-api`. Do not hand-edit.

| Restriction | Enforcement | Exact statement |
|---|---|---|
| `ProtocolBudget` | `protocol_enforced` | Enforced by the LayerX protocol state machine. |
| `DaemonLimit` | `daemon_enforced` | Bypassing the daemon bypasses this limit. It is not equivalent to a protocol budget. |
| `ApprovalHold` | `daemon_enforced` | An approval hold is a daemon-enforced restriction. It confers no protocol authority, and bypassing the daemon bypasses the restriction. |

Every authoritative read carries the full verification-level lattice and freshness coordinates. `Unknown` remains a first-class submission state. Mutations retain caller-supplied idempotency keys, and protocol result codes retain their exact signed integer value.

The approval surface is available from contract `1.1` and remains a daemon-enforced restriction with no protocol authority.
