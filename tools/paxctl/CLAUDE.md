# seictl

Dual-purpose tool: CLI for Sei node operators and HTTP sidecar server for the sei-k8s-controller. Packaged as a single Go binary (`ghcr.io/sei-protocol/seictl`) distributed via GoReleaser (native binaries) and Docker (distroless).

## Architecture

`seictl` carries two distinct surfaces today:

**Node-operator surface** (the original):
- **CLI commands**: `config patch`, `genesis patch`, `patch`, `serve`, `await` (top-level files: `config.go`, `genesis.go`, `patch.go`, `serve.go`, `await.go`)
- **Sidecar server**: `sidecar/server/` — HTTP API on `127.0.0.1:7777`
- **Task engine**: `sidecar/engine/` — concurrent task executor + cron scheduler
- **Task handlers**: `sidecar/tasks/` — snapshot, peers, genesis, config, state-sync, upload
- **Generated client**: `sidecar/client/` — OpenAPI-generated HTTP client for the sidecar API, consumed by sei-k8s-controller
- **OpenAPI spec**: `sidecar/api/openapi.yaml` — source of truth for the sidecar HTTP contract
- **Internal**: `internal/patch/` — TOML/JSON merge-patch logic

**Engineer-harness surface**: two preset-driven command trees over the SeiNetwork + SeiNode CRDs — `seictl network {apply,get,list,delete,watch}` (genesis networks) and `seictl node {…}` (followers/RPC). Shared internals in `internal/cliutil` (output/errors/client/parse/watch) and `internal/seiapi` (GVK plumbing). The legacy `nodedeployment`/`nd` verb and `internal/snd` were removed in the SeiNodeDeployment clean-break (v0.1.0).

## Code Standards

### Go

- Write clear, self-documenting code. Prefer descriptive names over comments that restate what the code does.
- Comments should explain *why*, not *what*. Reserve them for non-obvious intent, trade-offs, constraints, or public API contracts. Do not use comments as section dividers, narration, or decoration.
- No unnecessary abstractions. Three similar lines are better than a premature helper.
- Functions should do one thing. If a function needs a comment explaining what each section does, it should be multiple functions.
- Keep functions short. A function that doesn't fit on one screen is usually doing too much.
- Names are the best documentation: `sidecarImage(node)` needs no comment, `si(n)` needs a rewrite.
- Error messages should provide enough context to diagnose without a debugger.
- Imports grouped: stdlib, external, then `github.com/sei-protocol/seictl`.
- All code must pass `gofmt -s`. Run `make fmt` before committing.

### Testing

- Tests use `testing` from the standard library. No assertion frameworks unless already established.
- Table-driven tests for any function with more than two interesting input variations.
- Test names should describe the scenario, not the function: `TestValidateCron/empty_string` over `TestValidateCronEmpty`.
- Run `make test` before submitting changes.

### API Client (`sidecar/client/`)

- Generated from `sidecar/api/openapi.yaml` using oapi-codegen.
- Run `make generate` to regenerate after spec changes. Never hand-edit `sidecar.gen.go`.
- The high-level `SidecarClient` in `client.go` and typed task builders in `tasks.go` are hand-written wrappers over the generated code.
- Package name is `client`; downstream consumers alias it as `sidecar` by convention.

## Build & Run

```bash
make build       # Build to ./build/seictl
make test        # Run all tests
make lint        # Check formatting
make fmt         # Auto-format
make generate    # Regenerate OpenAPI client
make clean       # Remove build artifacts
```
