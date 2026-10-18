# locale — Directory Index

## Directory responsibility

The `locale` directory holds the default English translation bundle and per-domain locale modules. It defines the shape of `LocaleMessages` (via `en`) and is the single source for the default namespace used by the i18n instance.

## Key entities

| Entity | File | Responsibility |
|--------|------|----------------|
| `en` | en.ts | Merged English bundle; re-exported from package index |
| Module objects | module/*.ts | Domain-specific key-value objects (common, trading, orders, etc.) |

## Files

| File | Language | Summary | Entry |
|------|----------|---------|--------|
| [en.ts](en.md) | TS | Aggregates all module objects into default English bundle | en |
| [module/](module/index.md) | — | Subdirectory of locale modules | common, ui, trading, … |

## Subdirectory

- [module/](module/index.md) — Locale modules (common, markets, portfolio, trading, orders, positions, orderEntry, tpsl, share, leverage, scaffold, tradingRewards, tradingView, connector, transfer, affiliate, ui, tradingLeaderboard, tradingPoints, widget, vaults, notification, chart).
