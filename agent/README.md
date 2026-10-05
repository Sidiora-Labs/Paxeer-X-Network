# LayerX agent interface

The agent workspace is the interaction layer between autonomous agents and the
LayerX kernel (`layerxd`, C17) of Paxeer X Network. It has no protocol
authority. Every state-changing operation is a canonical LayerX activity signed
by protocol-recognised authority and submitted as the exact signed bytes; this
workspace never invents, applies, or asserts protocol state.

The LayerX Node Interface (LNI, [`schema/lni/`](schema/lni/README.md)) is the
sole boundary to the C17 core. Agent crates never open node storage, read
append-only logs, bind private C layouts, or link the C core.

## Crates

This is a Rust 2021 Cargo workspace (`agent/Cargo.toml`, toolchain pinned in
`agent/rust-toolchain.toml`).

| Crate | Role |
| --- | --- |
| `layerx-types` | Canonical domain types shared by the interaction layer |
| `layerx-wire` | Byte-exact canonical wire encoding |
| `layerx-crypto` | Key custody, disclosure-bound signing, payment payload codecs |
| `layerx-proof` | Offline verification of core-produced evidence |
| `layerx-paxeer-verifier` | Verification of checkpoint publications on the Paxeer X chain |
| `layerx-client` | Versioned LNI client |
| `layerx-identity-binding` | Unix-socket client for the identity binding service, shared with `human/` |
| `layerx-agent-api` | Stable contract shared by agent-facing servers and SDKs |
| `layerx-agentd` | The non-authoritative agent daemon library ([README](crates/layerx-agentd/README.md)) |
| `layerx-agentd-host` | Builds the `layerx-agentd` binary |
| `layerx-mcp` | Tenant- and scope-bound MCP server ([README](crates/layerx-mcp/README.md)) |
| `layerx-sdk` | Rust SDK for direct-node and daemon deployments |

The Python and TypeScript SDKs live under `agent/sdk/`. The supported daemon,
node interface, contract and SDK versions are listed in
[`COMPATIBILITY.md`](COMPATIBILITY.md).

## Build and check

All commands run from the repository root.

```sh
make agent-build
make agent-lint
make agent-check
```

`make agent-check` runs `agent-check-boundary`, `agent-check-secrets` and
`agent-test-boundary`. `make agent-check-boundary` enforces the node-interface
boundary by rejecting forbidden storage dependencies, node-private paths,
C-core linkage, generated bindings, and C-layout declarations that are not in
[`stable-abi-allowlist.toml`](stable-abi-allowlist.toml). An exception is a
protocol design change made through that allowlist; it cannot be suppressed
with a source comment.

## Tests

The full workspace tests and sanitizers require the real native daemon and a
disposable Paxeer X chain. Install the pinned Rust and Go toolchains, Foundry
(`forge`, `cast` and `anvil`), and the Python packages in
`tests/bridge/requirements.txt` on `PATH`, then download the Go module
dependencies with `go mod download` at the repository root. Run

```sh
sudo env "PATH=$PATH" "CARGO_HOME=$HOME/.cargo" "RUSTUP_HOME=$HOME/.rustup" \
  "GOMODCACHE=$(go env GOMODCACHE)" "GOPATH=$(go env GOPATH)" \
  sh agent/tools/run-real-node-tests.sh test
```

from the repository root, or pass `sanitizers` instead of `test` for both
sanitizer variants. The script refuses to run without root, because it launches
the daemon under a distinct test UID. Before the tests it builds the native
fixtures, the credit signer, the custody proof tool and `paxd`. Chain state and
keys live in temporary test directories. `make agent-test` and
`make agent-test-sanitize` require the same environment.

Focused targets exist per area, for example `make agent-test-mcp-scope`,
`make agent-test-lni-schema` and `make agent-test-sdk-rust`; the full list is
the `agent-test-*` targets in the root `Makefile`.

## MCP

The MCP server is [`crates/layerx-mcp`](crates/layerx-mcp/README.md): one
tenant, one scope set, daemon-only routing. The interop MCP/A2A transports live
in [`interop/`](../interop/README.md), and the `layerx install mcp`,
`layerx install a2a`, `layerx mcp serve` and `layerx a2a serve` commands live in
[`platform/cli/`](../platform/cli/README.md).

Wallet, token and grant tools use the same daemon prepare, disclose, sign,
submit and track path as other writes. Developer path:
[`docs/wiki/PaymentsQuickstart.md`](../docs/wiki/PaymentsQuickstart.md).
