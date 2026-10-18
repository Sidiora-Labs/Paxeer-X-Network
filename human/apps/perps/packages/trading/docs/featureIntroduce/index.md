---
name: feature-introduce-index
description: Index for trading feature introduction docs—parameter matrices and step-by-step UI flows aligned with hooks/types and `packages/ui-order-entry` selectors for automation and QA.
---

# Feature introduction (`featureIntroduce`)

This folder holds **feature-level** documentation: dimensions such as margin mode × order type × side × TP/SL, plus **numbered UI procedures** with `data-testid`, stable `id`s, or classes where testids are missing.

Authoritative logic and types: `@paxeer-network/hooks` (`useOrderEntry`, `*OrderCreator`) and `@paxeer-network/types`. Authoritative UI selectors: `packages/ui-order-entry`.

**Coverage:** [scenario-matrix.md](./scenario-matrix.md) lists **96** `TASK-*` IDs: **CROSS/ISOLATED × six order types × BUY/SELL × (NONE | TP_ONLY | SL_ONLY | TP_AND_SL)**. Opening bracket TP/SL is **implemented** for **LIMIT** and **MARKET** only; the same TP/SL column on **STOP_*, SCALED, TRAILING_STOP** is **matrix parity** — hooks block bracket fields there ([`canSetTPSLPrice`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts)); see each doc **step 9**. Regenerate: `python3 packages/trading/docs/featureIntroduce/_generate_task_docs.py` from repo root. For **new or heavily revised** tasks, prefer a **Cursor Task subAgent per `TASK-*`** to diff against code, then merge or re-run the generator.

## Documents

| Document | Notes |
| -------- | ----- |
| [scenario-matrix.md](./scenario-matrix.md) | Full **96** `TASK-*` inventory and links |
| [margin-modes.md](./margin-modes.md) | `CROSS` / `ISOLATED`, `margin_mode`, UI hooks |
| [direction-and-side.md](./direction-and-side.md) | `BUY` / `SELL`, long/short, `reduce_only` |
| [tpsl-bracket.md](./tpsl-bracket.md) | Bracket TP/SL wired on LIMIT/MARKET only (hooks) |
| [order-limit.md](./order-limit.md) | `LIMIT` — 16 |
| [order-market.md](./order-market.md) | `MARKET` — 16 |
| [order-stop-limit.md](./order-stop-limit.md) | `STOP_LIMIT` — 16 (TP/SL column = parity / negative expectations) |
| [order-stop-market.md](./order-stop-market.md) | `STOP_MARKET` — 16 |
| [order-scaled.md](./order-scaled.md) | `SCALED` — 16 |
| [order-trailing-stop.md](./order-trailing-stop.md) | `TRAILING_STOP` — 16 |

## Hooks deep dive

- [useOrderEntry](../../../hooks/docs/next/useOrderEntry/useOrderEntry.md)
