# Human interface

`human/` holds the person-facing side of Paxeer X Network: the Rust services that sign principals in, hold custody keys, plan and execute money movement and index public evidence, plus the web applications and wallet packages people use. The Rust services reach the LayerX kernel through the agent-layer crates (path dependencies into `agent/`, `interop/`, `programs/` and `platform/`) and reach the Paxeer X chain through typed JSON-RPC boundaries.

## Layout

| Path | Contents |
| --- | --- |
| [`Cargo.toml`](Cargo.toml) | Rust workspace for every crate under `crates/` |
| [`crates/layerx-human-service`](crates/layerx-human-service/README.md) | The human service: authentication, onboarding, journeys, approvals, custody and movement orchestration |
| [`crates/layerx-intents`](crates/layerx-intents) | The single typed-intent payload compiler (see below) |
| [`crates/layerx-human-identity-provider`](crates/layerx-human-identity-provider/README.md) | LXIP Unix-socket identity directory |
| [`crates/layerx-human-security-provider`](crates/layerx-human-security-provider/README.md) | LXSP Unix-socket authenticator and recovery-receipt provider |
| [`crates/layerx-human-kms`](crates/layerx-human-kms/README.md) | LXKP mutual-TLS key provider |
| [`crates/layerx-human-movement-provider`](crates/layerx-human-movement-provider/README.md) | Movement planning, execution and settlement-evidence daemon |
| [`crates/layerx-paxeer-client`](crates/layerx-paxeer-client/README.md) | Typed Paxeer X chain custody, finality, withdrawal and exit boundaries |
| [`crates/layerx-network-gateway`](crates/layerx-network-gateway) | Network gateway library used by the service crates and the explorer index |
| [`crates/layerx-explorer-index`](crates/layerx-explorer-index/README.md) | Rebuildable public explorer projections |
| [`crates/layerx-human-test-support`](crates/layerx-human-test-support/README.md) | Shared helpers for the service integration suites |
| [`schema/human-api`](schema/human-api/README.md) | Versioned HTTPS and JSON contract between the human service and the web application |
| [`apps/web`](apps/web) | Human web application (`@layerx/human-web`) and its [`@layerx/ui`](apps/web/packages/layerx-ui/README.md) component library |
| [`apps/wallet`](apps/wallet/README.md) | Wallet web app (`@paxeer/wallet-app`) |
| [`wallet`](wallet/README.md) | Wallet workspace: gateway, SDK, demo app, attestor and ceremony Go modules, deployment configuration |
| [`tools`](tools) | Workspace gates: `api-gen`, `boundary-check`, `copy-lint`, `schema-check`, `ui-gate` and `dependency-policy.sh` |

## Build and test

From the repository root:

```sh
make human-js-install   # npm ci for the web app and the agent TypeScript SDK
make human-build        # API generation check, cargo build of the workspace, web build
make human-test         # cargo test of the workspace, web unit tests
make human-check        # cargo check, payload-authority gate, schema check, web typecheck
make human-lint         # copy lint, clippy -D warnings, payload-authority gate
make human-check-ui     # UI gate, @layerx/ui build, web typecheck and tests
```

`make human-test` builds the native `explorer_fixture` first and passes it as `LAYERX_EXPLORER_CORE_FIXTURE`. A single crate runs with `cargo test --locked --manifest-path human/Cargo.toml -p <crate>`.

## The single payload authority

`layerx-intents` is the only component in `human/` permitted to construct protocol payload bytes. Every other crate and the web application describe protocol effects as typed intents and receive canonical bytes back as opaque evidence; none of them may depend on `layerx-wire` or invoke its encoding entry points. The rule exists so the disclosure a person approves is the bytes that get signed: one compiler produces the payload and no second encoder can drift from it. `make human-check` and `make human-lint` run the `boundary-check` gate over `human/crates`, and `make human-check-bundle` builds the web app and runs the same gate over the built `.next` bundle so the browser ships no payload-encoding code path.
