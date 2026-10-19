# Generated SDK guarantees

This file is generated from `services/agent/schema/agent-api`. Do not hand-edit.

| Restriction | Enforcement | Exact statement |
|---|---|---|
{{GUARANTEES}}

Every authoritative read carries the full verification-level lattice and freshness coordinates. `Unknown` remains a first-class submission state. Mutations retain caller-supplied idempotency keys, and protocol result codes retain their exact signed integer value.

The approval surface is available from contract `{{APPROVAL_INTRODUCED}}` and remains a daemon-enforced restriction with no protocol authority.
