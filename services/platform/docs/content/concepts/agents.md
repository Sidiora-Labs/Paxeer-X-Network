# Agents and budgets

An agent is software you allow to spend money. The interesting question is not how to let it spend - that is one call - but what stops it when it goes wrong.

## The ceiling that survives a compromise

A budget is a protocol object in the budget module. You create it, you fund it, and spending past what is funded is refused by the transition function. That refusal does not depend on your agent behaving, on the agent runtime being intact, or on `layerx-agentd` still being in the request path. It is the same kind of guarantee as conservation of supply.

Anything narrower than the funded budget - a per-call limit, a counterparty allow-list, a rate - is enforced by the agent layer or by your own code. It is real, and it is worth having, and it is not a protocol guarantee. This documentation labels it accordingly, and so should your product copy.

## Capabilities

A capability is what `layerx-agentd` will let a holder do. You create one, attenuate it into something narrower, and revoke it when you are done.

| Operation | Effect |
|---|---|
| `capability.create` | Mints a capability for an agent |
| `capability.attenuate` | Produces a strictly narrower capability |
| `capability.revoke` | Ends it |
| `capability.list` | Shows what is outstanding |

Attenuation only ever narrows. There is no widening operation, so a capability handed down a chain cannot regain authority on the way.

## Approval holds

Some activity should not happen without a person. The approval module lets the agent layer hold an activity and surface it for a decision, carrying the held activity's structured disclosure, the digest of its canonical bytes, the reason it was held, and a deterministic expiry.

| Operation | Effect |
|---|---|
| `approval.list` | Pending holds |
| `approval.get` | One hold with its disclosure |
| `approval.approve` | Releases exactly the held activity |
| `approval.reject` | Ends it |

The digest matters: approving a hold approves the exact bytes that were disclosed, not a re-derived intent that might differ.

## The write path

Agent writes are always prepare, sign, submit, then track or wait. Preparing gives you the canonical bytes and a disclosure describing them; signing binds the disclosure; submitting hands it to the network; tracking resolves the outcome. If a submission cannot be classified, the answer is `Unknown` - see [Retries and unknown outcomes](concepts-idempotency.html).

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Protocol budget ceilings | `protocol` | The funded budget holds even against a fully compromised agent runtime. |
| Programs never write balances | `protocol` | An agent calling a program cannot use it to exceed its own authority. |
| Capability attenuation | `agent-layer` | Binds callers that go through `layerx-agentd`. A principal reaching the protocol another way is bound by protocol budgets, not by this. |
| Approval holds | `agent-layer` | The hold exists while the daemon is in the request path. |
| Agent tenancy isolation | `agent-layer` | Sessions are scoped to an agent's tenancy. This is an agent-layer boundary, not a protocol one. |
| Unknown is a real outcome | `agent-layer` | An unclassifiable submission is reported as `Unknown`, never guessed. |
