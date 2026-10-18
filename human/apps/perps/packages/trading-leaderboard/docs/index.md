# trading-leaderboard (src)

Overview of the `@paxeer-network/trading-leaderboard` package source. This module provides leaderboard UI, campaign widgets, rankings, and rewards for the trading leaderboard feature.

## Package entry (index.ts)

Re-exports:

- `./components/provider` — TradingLeaderboardProvider, useTradingLeaderboardContext
- `./components/ranking/generalRanking`, `./components/ranking/campaignRanking`
- `./components/leaderboard/generalLeaderboard`, `./components/leaderboard/campaignLeaderboard`
- `./pages/leaderboard/page` — LeaderboardPage
- `./components/campaigns/type`
- `CampaignsHeaderWidget` from `./components/campaigns/header`
- `LeaderboardBackground` from `./components/background`

## Directory structure

| Directory | Description |
|-----------|-------------|
| [hooks](./hooks/index.md) | Hooks for trade permission and scroll end detection |
| [pages](./pages/leaderboard/index.md) | Leaderboard page and section components |
| [components](./components/index.md) | Provider, campaigns, leaderboard, ranking, rewards, rule, background |
| [deprecated](./deprecated/index.md) | Deprecated components (mirror of legacy structure) |

## Root-level files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Package entry; re-exports above | (this file) |
| `version.ts` | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` | [version.md](./version.md) |
| `utils.ts` | TypeScript | Date range, campaign date formatting, weekly split | [utils.md](./utils.md) |
| `type.ts` | TypeScript | `DateRange`, `LeaderboardTab` | [type.md](./type.md) |
