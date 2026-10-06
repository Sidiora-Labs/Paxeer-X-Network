<!-- id: beta_contract -->
<!-- readiness_claim: false -->

# Paxeer X Network mainnet-beta contract

This is the canonical beta contract of Paxeer X Network. The beta is
mainnet-beta: it runs on the Paxeer network, chain 125, and every product
capability is reached through the unified router at
`https://api-mainnet-beta.paxeer.network`. LayerX is the kernel domain inside
Paxeer X Network; it is not a separate network and has no separate beta.

The contract states the surfaces the beta supports, the rung each must reach,
the endpoints, the evidence ledger, the accepted beta configuration and the
differences from production. `tools/ci/beta-report.sh` reads the evidence
ledger and renders the go/no-go report from its gate records alone.

**This beta is not ready.** The readiness claim below is `false` and stays
`false` until every surface has reached its required rung through an executed
gate recorded in `spec/layerx-beta/qualification.kvx` and the owner go
decision is recorded there.

## Identity

| Key | Value |
| --- | --- |
| id | beta_contract |
| product | Paxeer X Network |
| profile | mainnet-beta |
| readiness_claim | false |
| readiness_statement | The Paxeer X Network mainnet-beta is NOT ready: no gate record exists and every surface is at rung source_present. |
| chain_id | 125 |
| router_url | https://api-mainnet-beta.paxeer.network |
| beta_domain | paxeer.network |
| go_module | github.com/Sidiora-Labs/Paxeer-X-Network |
| required_rung_functional | runtime_proven |
| required_rung_hosted | deployment_proven |
| rung_order | source_present < statically_coherent < built < tested < runtime_proven < deployment_proven < owner_certified |
| evidence_ledger | spec/layerx-beta/qualification.kvx |
| report_generator | tools/ci/beta-report.sh |
| report_status | no-go |

## Evidence ledger

`spec/layerx-beta/qualification.kvx` is the single beta ledger. It holds two
append-only record shapes: `[gate.<task>.<n>]`, an executed gate with its
revision, command, environment, start time, outcome (`pass`, `fail` or
`blocked`) and evidence location, and `[observation.<task>.<n>]`, an
assumption, contradiction or external blocker with its severity.

- Only a gate record raises a surface. A task status, an observation, a
  document or a plan never raises a rung.
- A gate that cannot run because an owner input is missing is recorded
  `blocked` with the input named; it is never recorded `pass`.
- The owner go decision is a gate record with outcome `pass` whose note begins
  `owner go decision:`. Its revision is the release candidate.
- The decision is `go` only when that record exists and no gate on the same
  revision failed or was blocked.
- The ledger is public. Evidence locations name private evidence stores, never
  host addresses, keys or tokens.

`tools/ci/beta-report.sh --check` validates the ledger shape without CI inputs
and prints the decision summary; `tools/ci/beta-report.sh` renders the full
report to stdout.

## Network and endpoints

| Key | Value |
| --- | --- |
| chain | Paxeer network, EVM chain id 125 |
| router | https://api-mainnet-beta.paxeer.network, the canonical entry and discovery surface for every product capability |
| rpc_records | the existing mainnet-beta RPC records and the chain125 record stay unchanged; product names resolve through the same router catalogue |
| developer_host | dev.paxeer.network |
| ramp_host | ramp.paxeer.network |
| emulator_endpoint | http://127.0.0.1:9402, local only |
| private_services | identity, policy, KMS, signer shares, event sources, provisioners, CI controllers and state stores stay behind authenticated internal boundaries and are not routed publicly |
| explorer | the Paxeer explorer fork |

Accounts are funded through custody credit submitted to the router. There is
no public faucet or control surface on the mainnet-beta.

## Surfaces

| Surface | Scope | Class | Required rung | Reached rung | Source |
| --- | --- | --- | --- | --- | --- |
| native-core | ledger transition, checkpoints, settlement, guarantor bonds | functional | runtime_proven | source_present | src/, include/ |
| native-daemon | layerxd, layerxctl, layerx-verify, layerx-genesis | functional | runtime_proven | source_present | cmd/ |
| paxeer-chain | paxd on chain 125 with the LayerX custody, anchor and kernel precompiles | hosted | deployment_proven | source_present | daemon/paxd, node/, modules/, precompiles/ |
| router | unified router and its route catalogue | hosted | deployment_proven | source_present | platform/hosted/gateway |
| kernel | LayerX kernel domain behind the router: core, receipt authority and agent boundaries | hosted | deployment_proven | source_present | platform/hosted/node, platform/hosted/core, platform/hosted/authority, platform/hosted/agent-boundary |
| identity | principals, sessions, introspection and service tokens | hosted | deployment_proven | source_present | platform/hosted/identity |
| registry | hosted program registry | hosted | deployment_proven | source_present | platform/hosted/registry |
| webhooks | signed webhook deliveries | hosted | deployment_proven | source_present | platform/hosted/webhooks |
| dashboard | developer dashboard API and web | hosted | deployment_proven | source_present | platform/hosted/dashboard |
| indexer | decoded history of the kernel domain and Paxeer blocks | hosted | deployment_proven | source_present | platform/hosted/indexer |
| human | Human API, custody, intents, approvals and the wallet | hosted | deployment_proven | source_present | human/, platform/hosted/human |
| interop | x402, AP2, UCP, Visa TAP, bridge relayer, gas station and web search | functional | runtime_proven | source_present | interop/crates |
| agent | layerx-agentd, MCP surface | functional | runtime_proven | source_present | agent/crates |
| sdks | TypeScript, Python, Rust, Go, JVM, Swift and .NET SDKs | functional | runtime_proven | source_present | agent/sdk, agent/crates/layerx-sdk, platform/sdk |
| platform-cli | CLI and local emulator | functional | runtime_proven | source_present | platform/cli, platform/emulator |
| programs | Programs runtime: deploy, paid call, restart | functional | runtime_proven | source_present | programs/crates |
| docs-site | documentation site and executable samples | functional | runtime_proven | source_present | platform/docs |

## Beta configuration

The following chain configuration is accepted for the mainnet-beta as it runs
today. It is recorded here as beta configuration, not as a defect, and no
governance proposal changes it during the beta.

| Key | Accepted beta value |
| --- | --- |
| slashing | zero slashing: no validator stake is slashed during the beta |
| consensus_overrides | the validators run with the `unsafe-*` consensus overrides enabled; they are operator settings, not genesis parameters |
| validator_layout | four validators on two hosts, two per host; losing one host halts block production, and that halt risk is accepted for the beta |
| full_nodes | the existing synced full nodes serve RPC; no validator replacement is scheduled during the beta |

## Unknown-state behaviour

- An outcome a client did not observe stays unknown under its idempotency key
  and is resolved only by looking up the canonical receipt; it is never
  resubmitted blindly and never reported as success.
- Paxeer degradation is never presented as kernel finality.
- A surface without an executed gate stays at rung `source_present`.

## Beta-versus-production differences

| Key | Difference |
| --- | --- |
| ui_polish | UI polish, visual regression and automated accessibility scans are not beta gates. |
| usability_and_performance | Usability studies, performance budgets and soak runs are not beta gates. |
| external_security_audit | No external security audit is required for the beta. |
| consensus | Zero slashing, the unsafe consensus overrides and the two-host validator layout above are beta configuration. |
| production_certification | The beta requires runtime_proven and deployment_proven rungs, not owner_certified. |

## Contradictions

| Key | Canonical value | Divergent source | Divergent value |
| --- | --- | --- | --- |
| go_sdk_module | github.com/Sidiora-Labs/Paxeer-X-Network/platform/sdk/go | platform/sdk/go/go.mod | github.com/Sidiora-Labs/LayerX-Network/platform/sdk/go |

The readiness claim cannot become `true` while any row remains.
