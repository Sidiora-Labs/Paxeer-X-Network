# Layerx-protocol — Agent Context

<!-- codify-owned: graph-agent-context v1 -->

_Generated graph context owned by `cg agentmd`. Regenerate with `cg agentmd --write` after significant changes. Workflow instructions remain owned by `cg spec render`._

## Languages

| Language | Files | Lines |
|---|---:|---:|
| go | 4848 | 1296799 |
| typescript | 3891 | 390344 |
| rust | 2084 | 1036002 |
| c | 722 | 271785 |
| solidity | 520 | 83867 |
| python | 465 | 154727 |
| javascript | 207 | 30423 |
| java | 88 | 18990 |
| swift | 35 | 9119 |
| csharp | 18 | 6538 |
| kotlin | 6 | 427 |

12884 source files, 3299021 lines total.

## Directory map

- `admin/` — 7 files, 2620 lines (mostly go)
- `agent/` — 699 files, 309337 lines (mostly rust)
- `bridge/` — 30 files, 14496 lines (mostly rust)
- `cmd/` — 68 files, 39981 lines (mostly c)
- `(root)` — 4 files, 302 lines (mostly go)
- `consensus/` — 731 files, 197686 lines (mostly go)
- `contracts/` — 116 files, 20748 lines (mostly solidity)
- `daemon/` — 14 files, 2961 lines (mostly go)
- `custodyproof/` — 5 files, 2386 lines (mostly go)
- `engine/` — 197 files, 48456 lines (mostly go)
- `explorer/` — 3489 files, 282411 lines (mostly typescript)
- `tests/fuzz/` — 11 files, 685 lines (mostly c)
- `human/` — 1215 files, 344979 lines (mostly typescript)
- `hpx/` — 2 files, 883 lines (mostly go)
- `include/` — 66 files, 11369 lines (mostly c)
- `tests/integration/` — 70 files, 15680 lines (mostly typescript)
- `interchain/` — 445 files, 123556 lines (mostly go)
- `interop/` — 180 files, 116454 lines (mostly rust)
- `layerxproof/` — 22 files, 4516 lines (mostly go)
- `loadtest/` — 346 files, 43210 lines (mostly solidity)
- `modules/` — 690 files, 173519 lines (mostly go)
- `node/` — 124 files, 31465 lines (mostly go)
- `parallelization/` — 9 files, 194 lines (mostly rust)
- `occ_tests/` — 3 files, 898 lines (mostly go)
- `platform/` — 673 files, 316733 lines (mostly rust)
- `paxeer-network/` — 12 files, 1432 lines (mostly typescript)
- `precompiles/` — 318 files, 90380 lines (mostly go)
- `programs/` — 324 files, 219795 lines (mostly rust)
- `rpc/` — 116 files, 36036 lines (mostly go)
- `ratelimiter/` — 3 files, 550 lines (mostly go)
- `sdk/` — 1367 files, 343781 lines (mostly go)
- `src/` — 203 files, 78898 lines (mostly c)
- `signatures/` — 3 files, 1081 lines (mostly c)
- `storage/` — 445 files, 119015 lines (mostly go)
- `tests/` — 398 files, 149644 lines (mostly c)
- `store/` — 7 files, 307 lines (mostly go)
- `sync/` — 1 files, 30 lines (mostly go)
- `tests/testutil/` — 23 files, 1415 lines (mostly go)
- `tools/` — 216 files, 84373 lines (mostly python)
- `types/` — 1 files, 13 lines (mostly go)
- `utils/` — 28 files, 2746 lines (mostly go)
- `wasm/` — 203 files, 64000 lines (mostly go)

## Build & tooling

- `Makefile` — make
- `package.json` — npm/node (scripts: `build`)
- `go.mod` — Go modules
- `foundry.toml` — Foundry (Solidity)

## Entry points

- function `main` — `agent/crates/layerx-agent-api/build.rs:431`
- function `main` — `agent/crates/layerx-agentd/examples/native_handover_programs.rs:536`
- function `main` — `agent/crates/layerx-agentd/examples/native_handover_reads.rs:388`
- function `main` — `agent/crates/layerx-agentd/examples/review_audit_export.rs:24`
- function `main` — `agent/crates/layerx-agentd/src/main.rs:2202`
- function `main` — `agent/crates/layerx-client/examples/go_verifier_vectors.rs:1565`
- function `main` — `agent/crates/layerx-client/examples/module_state.rs:49`
- function `main` — `agent/crates/layerx-client/examples/native_handover_history.rs:340`
- function `main` — `agent/crates/layerx-client/examples/paxeer_bind_vectors.rs:44`
- function `main` — `agent/crates/layerx-crypto/tests/native_payment_vectors.c:37`
- function `main` — `agent/crates/layerx-crypto/tests/native_receive.c:6`
- function `main` — `agent/crates/layerx-mcp/src/bin/layerx-mcp.rs:89`
- function `main` — `agent/crates/layerx-proof/examples/offline_export.rs:27`
- function `main` — `agent/crates/layerx-proof/examples/offline_verify.rs:68`
- function `main` — `agent/crates/layerx-sdk/src/bin/layerx-wallet-rpc.rs:200`

## HTTP routes

| Method | Pattern | Handler | Where |
|---|---|---|---|
| GET | `` | `version` | `storage/tools/bench/benchmark.go:155` |
| * | `/` | — | `explorer/frontend/playwright/fixtures/auth.ts:7` |
| * | `/` | — | `explorer/frontend/playwright/fixtures/rewards.ts:28` |
| * | `/` | — | `explorer/frontend/ui/shared/entities/address/AddressEntity.pw.tsx:292` |
| * | `/` | — | `explorer/frontend/ui/snippets/navigation/vertical/NavigationDesktop.pw.tsx:131` |
| * | `/` | — | `explorer/frontend/ui/snippets/navigation/vertical/NavigationDesktop.pw.tsx:166` |
| * | `/_openzeppelin/contracts/token/ERC20/ERC20.sol` | — | `explorer/frontend/ui/shared/monaco/utils/composeFileTree.spec.ts:27` |
| * | `/_openzeppelin/contracts/token/ERC20/IERC20.sol` | — | `explorer/frontend/ui/shared/monaco/utils/composeFileTree.spec.ts:23` |
| * | `/_openzeppelin/contracts/token/ERC20/extensions/IERC20Metadata.sol` | — | `explorer/frontend/ui/shared/monaco/utils/composeFileTree.spec.ts:19` |
| * | `/_openzeppelin/contracts/utils/Context.sol` | — | `explorer/frontend/ui/shared/monaco/utils/composeFileTree.spec.ts:15` |
| * | `/addresses/:hash` | — | `explorer/frontend/lib/api/services/multichainAggregator.ts:7` |
| * | `/addresses/:hash/domains` | — | `explorer/frontend/lib/api/services/multichainAggregator.ts:17` |
| * | `/addresses/:hash/portfolio` | — | `explorer/frontend/lib/api/services/multichainAggregator.ts:22` |
| * | `/addresses/:hash/tokens` | — | `explorer/frontend/lib/api/services/multichainAggregator.ts:11` |
| * | `/api` | — | `explorer/frontend/lib/api/services/general/v1.ts:9` |
| * | `/api/account/auth/logout` | — | `explorer/frontend/lib/api/services/general/account.ts:59` |
| * | `/api/account/v2/address/link` | — | `explorer/frontend/lib/api/services/general/account.ts:56` |
| * | `/api/account/v2/authenticate_via_dynamic` | — | `explorer/frontend/lib/api/services/general/account.ts:62` |
| * | `/api/account/v2/authenticate_via_wallet` | — | `explorer/frontend/lib/api/services/general/account.ts:50` |
| * | `/api/account/v2/confirm_otp` | — | `explorer/frontend/lib/api/services/general/account.ts:44` |
| * | `/api/account/v2/email/link` | — | `explorer/frontend/lib/api/services/general/account.ts:53` |
| * | `/api/account/v2/get_csrf` | — | `explorer/frontend/lib/api/services/general/account.ts:7` |
| * | `/api/account/v2/send_otp` | — | `explorer/frontend/lib/api/services/general/account.ts:41` |
| * | `/api/account/v2/siwe_message` | — | `explorer/frontend/lib/api/services/general/account.ts:47` |
| * | `/api/account/v2/user/api_keys{/:id}` | — | `explorer/frontend/lib/api/services/general/account.ts:35` |
| * | `/api/account/v2/user/custom_abis{/:id}` | — | `explorer/frontend/lib/api/services/general/account.ts:13` |
| * | `/api/account/v2/user/info` | — | `explorer/frontend/lib/api/services/general/account.ts:10` |
| * | `/api/account/v2/user/tags/address{/:id}` | — | `explorer/frontend/lib/api/services/general/account.ts:23` |
| * | `/api/account/v2/user/tags/transaction{/:id}` | — | `explorer/frontend/lib/api/services/general/account.ts:29` |
| * | `/api/account/v2/user/watchlist{/:id}` | — | `explorer/frontend/lib/api/services/general/account.ts:17` |
| * | `/api/candle/[...path]` | — | `human/apps/wallet/src/app/api/candle/[...path]/route.ts:1` |
| * | `/api/candle/cv/[...path]` | — | `human/apps/wallet/src/app/api/candle/cv/[...path]/route.ts:1` |
| * | `/api/candle/pax/[...path]` | — | `human/apps/wallet/src/app/api/candle/pax/[...path]/route.ts:1` |
| * | `/api/candle/sid/[...path]` | — | `human/apps/wallet/src/app/api/candle/sid/[...path]/route.ts:1` |
| * | `/api/chat` | — | `human/apps/wallet/src/app/api/chat/route.ts:1` |
| * | `/api/config` | — | `explorer/frontend/pages/api/config.ts:1` |
| * | `/api/csrf` | — | `explorer/frontend/pages/api/csrf.ts:1` |
| * | `/api/explorer/verify` | — | `human/apps/web/src/app/api/explorer/verify/route.ts:1` |
| * | `/api/fx/latest/USD` | — | `human/apps/wallet/src/app/api/fx/latest/USD/route.ts:1` |
| * | `/api/health` | — | `human/apps/wallet/src/app/api/health/route.ts:1` |
| * | `/api/healthz` | — | `explorer/frontend/pages/api/healthz.tsx:1` |
| * | `/api/log` | — | `explorer/frontend/pages/api/log.ts:1` |
| * | `/api/media` | — | `human/apps/wallet/src/app/api/media/route.ts:1` |
| * | `/api/metrics` | — | `explorer/frontend/pages/api/metrics.ts:1` |
| * | `/api/monitoring/invalid-api-schema` | — | `explorer/frontend/pages/api/monitoring/invalid-api-schema.ts:1` |
| * | `/api/performance/vitals` | — | `human/apps/web/src/app/api/performance/vitals/route.ts:1` |
| * | `/api/pns/api/v1/addresses-lookup` | — | `human/apps/wallet/src/app/api/pns/api/v1/addresses-lookup/route.ts:1` |
| * | `/api/pns/api/v1/addresses/[address]` | — | `human/apps/wallet/src/app/api/pns/api/v1/addresses/[address]/route.ts:1` |
| * | `/api/pns/api/v1/domains-lookup` | — | `human/apps/wallet/src/app/api/pns/api/v1/domains-lookup/route.ts:1` |
| * | `/api/pns/api/v1/domains/[name]` | — | `human/apps/wallet/src/app/api/pns/api/v1/domains/[name]/route.ts:1` |
| * | `/api/pns/api/v1/domains/[name]/events` | — | `human/apps/wallet/src/app/api/pns/api/v1/domains/[name]/events/route.ts:1` |
| * | `/api/points/balance/[address]` | — | `human/apps/wallet/src/app/api/points/balance/[address]/route.ts:1` |
| * | `/api/proxy` | — | `explorer/frontend/pages/api/proxy.ts:1` |
| * | `/api/push/cron` | — | `human/apps/wallet/src/app/api/push/cron/route.ts:1` |
| * | `/api/push/notify-tx` | — | `human/apps/wallet/src/app/api/push/notify-tx/route.ts:1` |
| * | `/api/push/send` | — | `human/apps/wallet/src/app/api/push/send/route.ts:1` |
| * | `/api/push/subscribe` | — | `human/apps/wallet/src/app/api/push/subscribe/route.ts:1` |
| * | `/api/sdk/[...path]` | — | `human/apps/wallet/src/app/api/sdk/[...path]/route.ts:1` |
| * | `/api/sidiora/logo/[...path]` | — | `human/apps/wallet/src/app/api/sidiora/logo/[...path]/route.ts:1` |
| * | `/api/sidiora/metadata` | — | `human/apps/wallet/src/app/api/sidiora/metadata/route.ts:1` |

## Load-bearing symbols (most referenced)

- `len` (function, 33297 refs) — `agent/crates/layerx-agentd/src/capability/mod.rs:440`
- `Err` (method, 24694 refs) — `consensus/abci/types/types.go:24`
- `new` (function, 21362 refs) — `agent/crates/layerx-agent-api/src/error.rs:20`
- `Some` (function, 16634 refs) — `consensus/libs/utils/option.go:15`
- `Equal` (function, 15486 refs) — `consensus/internal/protoutils/msg.go:45`
- `uint64` (function, 12822 refs) — `human/crates/layerx-intents/src/precompile.rs:387`
- `get` (function, 10553 refs) — `agent/crates/layerx-agent-api/src/generated.rs:86`
- `expect` (function, 9743 refs) — `agent/crates/layerx-types/src/json.rs:97`
- `byte` (function, 9135 refs) — `agent/crates/layerx-agentd/src/audit/record.rs:554`
- `clone` (function, 8780 refs) — `agent/crates/layerx-agentd/src/human_runtime.rs:2072`
- `and` (method, 8631 refs) — `consensus/libs/bits/bit_array.go:165`
- `require` (function, 8099 refs) — `agent/crates/layerx-agentd/src/budget/budget_proof.rs:190`
- `from` (function, 7636 refs) — `agent/crates/layerx-agentd/src/budget/hold.rs:75`
- `append` (function, 7618 refs) — `agent/crates/layerx-sdk/src/native_capabilities.rs:117`
- `CHECK` (macro, 7118 refs) — `sdk/crypto/keys/secp256k1/internal/secp256k1/libsecp256k1/src/util.h:46`

## Querying this codebase

This project is indexed by Codify (SQLite + FTS5, 100% local). Prefer these over grep/file-walking — one call returns definitions, snippets, and call edges:

```bash
cg context <query>      # symbols + snippets + callers/callees + routes
cg search <text>        # instant name/full-text search
cg grep <pattern> [-i]  # line search, each hit in its symbol
cg files <glob|words>   # find files; --dirs maps directories
cg symbol <name>        # definition + snippet + reference count
cg impact <name> -d 3   # who breaks if this changes
cg routes [filter]      # URL pattern -> handler
cg changes              # impact radius of uncommitted edits
```

All of the above accept `--json`. The graph auto-syncs via `cg watch`, or connect over MCP with `cg mcp-install`.

Over MCP, use `grep_code`, `find_files`, `search_code`, and `get_context` before any shell search: `cg grep` and `cg files` answer what grep, rg, and find would, ranked and capped to a token budget.

When another process (an editor's indexer) holds the database, `cg remember`, `cg handoff`, and the bookkeeping behind `cg spec done` say **queued**: the write is kept in `.codegraph/journal/` and the next `cg` command applies it — do not re-run it. Exit 75 (a claim) means nothing changed: re-run the same command. `cg journal` lists what is queued.
