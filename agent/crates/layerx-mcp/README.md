# layerx-mcp

Tenant- and scope-bound Model Context Protocol tools for the LayerX domain of
Paxeer X Network. A model gets the tools its bound scope allows. It does not get
protocol authority.

Every call routes through `layerx-agentd`. There is no MCP-only write path and no
tool-owned connection to the C17 core. Authority is fixed at server startup from
an ordinary daemon session and capability.

This crate lives in the agent workspace (`agent/`). Related surfaces:

| Surface | Location |
| --- | --- |
| This server | `agent/crates/layerx-mcp` |
| Daemon | `agent/crates/layerx-agentd` |
| MCP / A2A as interop transports | [`interop/`](../../../interop/README.md) |
| `layerx install mcp` / `layerx mcp serve` | [`platform/cli/`](../../../platform/cli/README.md) |

## Tools in this crate

A tool is listed only when the bound session's scopes include its required
scope, and a `read-only` deployment lists read tools only. Operator walkthrough:
[`docs/wiki/RunningAnAgent.md`](../../../docs/wiki/RunningAnAgent.md).

The core catalogue (`TOOL_CATALOGUE` in `src/server.rs`):

| Tool | Kind | Required scope | Daemon operation |
| --- | --- | --- | --- |
| `tenant.readiness` | read | `read` | `TenantReadiness` |
| `balance.get` | read | `read:balance` | `ReadBalance` |
| `wallet.balance` | read | `read:wallet:balance` | `ReadBalance` |
| `wallet.accounts` | read | `read:wallet:accounts` | `ReadAccount` |
| `history.list` | read | `read:history` | `ReadHistory` |
| `receipt.get` | read | `read:receipt` | `ProgramReceipt` |
| `checkpoint.get` | read | `read:checkpoint` | `ReadCheckpoint` |
| `proof.get` | read | `read:proof` | `ReadProofBundle` |
| `availability.get` | read | `read:availability` | `AvailabilityFetch` |
| `activity.prepare` | write | `write:prepare` | `Prepare` |
| `activity.disclose` | write | `write:disclose` | `Prepare` |
| `activity.sign` | write | `write:sign` | `Sign` |
| `activity.submit` | write | `write:submit` | `Submit` |
| `wallet.send` | write | `write:wallet:send` | `Submit` |
| `token.create` | write | `write:token:create` | `Submit` |
| `token.mint` | write | `write:token:mint` | `Submit` |
| `token.transfer` | write | `write:token:transfer` | `Submit` |
| `grant.issue` | write | `write:grant:issue` | `Submit` |
| `grant.draw` | write | `write:grant:draw` | `Submit` |
| `activity.track` | write | `write:track` | `Track` |
| `activity.wait` | write | `write:activity:wait` | `Wait` |
| `faucet.request` | write | `write:faucet:claim` | `FaucetClaim` |

The paid web tools (`WEB_TOOLS` in `src/catalogue.rs`) each make one 402LXP
payment to the configured x-websearch sidecar and return a sequencer-signed
settlement receipt with the result. Their output is marked untrusted
(`layerx/output: untrusted` in the listing).

| Tool | Required scope |
| --- | --- |
| `web.search` | `write:web:search` |
| `web.fetch` | `write:web:fetch` |
| `web.content` | `write:web:content` |

The session tools (`SESSION_TOOLS` in `src/catalogue.rs`) manage daemon-owned
state only: `subscription.create`, `subscription.list`, `subscription.pause`,
`subscription.resume`, `subscription.delete`, `subscription.health` and
`subscription.acknowledge` require the `subscribe` scope; `approval.list`,
`approval.get`, `approval.approve` and `approval.reject` require the `approve`
scope.

Write tools follow the ordinary daemon path: prepare, disclose, sign, submit,
track. `activity.wait` uses the `Wait` operation and the existing receipt
tracking stages. A write result carries a verified receipt or an honest
non-terminal state, and each invocation is recorded as `Completed`, `Refused`,
`Unknown` or `Failed` (`InvocationOutcome` in `src/server.rs`). This catalogue is what `layerx mcp serve` and the `layerx-mcp`
binary serve: both bind one daemon session through `src/binding.rs` and route
every call through `src/stdio.rs`, so no signing seed and no gateway credential
is read on the served path. The CLI's gateway-bound `layerx a2a serve` surface
is a different catalogue (`receipt.get`, `activity.submit` and
`faucet.request` against the hosted gateway, `SERVED` in
`platform/cli/src/toolset.rs`); it shares only the `faucet.request` definition,
the `FAUCET_REQUEST` constant in `src/server.rs`, so the two surfaces cannot
drift apart on that tool's name, kind, scope, mutation or evidence.

The wallet and token tools are implemented in `src/tools/wallet.rs` and
`src/tools/write.rs`, the web tools in `src/tools/web.rs`. Payment walkthrough:
[`docs/wiki/PaymentsQuickstart.md`](../../../docs/wiki/PaymentsQuickstart.md).

Untrusted tool arguments cannot change tenant, scope, or counterparty. See
`src/untrusted.rs` and `src/validate.rs`. Payment payload and disclosure codecs
live in `layerx-crypto` (`payments`, `disclosure`). The signer binds payment
payload and disclosure bytes before signing; it does not accept an unstructured
approval.

## Test

From the repository root:

```sh
make agent-test
```

Crate tests are `approval`, `daemon_bound`, `faucet`, `read`, `readonly`,
`scope`, `web`, `write`, and `injection` (`agent/tests/mcp/injection.rs`).
Focused targets: `make agent-test-mcp-scope`, `agent-test-mcp-read`,
`agent-test-mcp-write`, `agent-test-mcp-approval`, `agent-test-mcp-injection`
and `agent-test-mcp-readonly`.

## Serving

The crate ships one binary. `layerx-mcp <absolute path to a binding document>`
binds the daemon session the document names and serves the catalogue on a
peer-credential admitted Unix socket. The developer CLI serves the same session
on standard input and output with `layerx mcp serve --daemon-binding <path>`.
The document is a closed JSON object:

```json
{
  "mode": "full",
  "tenant": "beta",
  "store": "/var/lib/layerx/agentd/store",
  "audit_root": "/var/lib/layerx/agentd/audit",
  "session_id": "<32 hex bytes>",
  "session_token_file": "/etc/layerx/mcp-session-token",
  "session_generation": 1,
  "capability_id": "<32 hex bytes>",
  "core_sequence": 120,
  "deadline_ms": 10000,
  "agent": {
    "endpoint": "<IPv4 loopback address>:<port>",
    "bearer_file": "/etc/layerx/agentd-bearer",
    "probe_program": "<32 hex bytes>"
  },
  "limit": {
    "id": "<16 hex bytes>",
    "name": "mcp",
    "scope": "tenant",
    "scope_id": "<32 hex bytes>",
    "ceiling": "1000",
    "consumed": "0"
  },
  "listener": {
    "socket": "/run/layerx/mcp.sock",
    "owner_uid": 0,
    "owner_gid": 0,
    "mode": "660",
    "admitted_uids": [0]
  }
}
```

`mode` is `full` or `read-only`. `agent.endpoint` must be on the IPv4 loopback
address. `listener` is required by the binary and ignored by the CLI transport.
Two optional sections are accepted: `agent.readiness` (`endpoint`,
`gateway_key_file`, optional `trust_anchors`) and `web` (`endpoint`, `network`,
`sequencer_public_key`, `timeout_ms`, `pending_attempts`,
`approval_threshold`), which a session carrying a web scope requires. Secret
files are read through the daemon's protected-source boundary: absolute,
owner-only, and never copied into the document.
