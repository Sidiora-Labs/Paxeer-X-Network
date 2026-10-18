# leaderboard

General and campaign leaderboard widgets and UI. Uses ranking components and shared LeaderboardTabs / LeaderboardFilter.

## Subdirectories

| Directory | Description | Link |
|-----------|-------------|------|
| [generalLeaderboard](./generalLeaderboard/index.md) | General leaderboard (volume/PnL tabs, date filter, ranking) | — |
| [campaignLeaderboard](./campaignLeaderboard/index.md) | Campaign-specific leaderboard | campaignLeaderboard |
| shared | LeaderboardTabs, LeaderboardFilter | — |

## Files (shared)

| File | Language | Description |
|------|----------|-------------|
| `shared/LeaderboardTabs.tsx` | TSX | Volume / PnL tab switcher |
| `shared/LeaderboardFilter.tsx` | TSX | Date range and search filter UI |

## Main exports (from package)

- `GeneralLeaderboardWidget`, `GeneralLeaderboardWidgetProps`
- `GeneralLeaderboard`, `GeneralLeaderboardProps`
- `useGeneralLeaderboardScript`
- `CampaignLeaderboardWidget`, `CampaignLeaderboardWidgetProps`
- `CampaignLeaderboard`, `CampaignLeaderboardProps`
- `useCampaignLeaderboardScript`
