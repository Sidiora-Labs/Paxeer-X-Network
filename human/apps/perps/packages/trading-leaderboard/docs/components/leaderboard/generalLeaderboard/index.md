# generalLeaderboard

General leaderboard: volume/PnL tabs, date filter, search, and general ranking list.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Re-exports widget, UI, script | — |
| `generalLeaderboard.widget.tsx` | TSX | Widget that wires script + UI | [generalLeaderboard.widget.md](./generalLeaderboard.widget.md) |
| `generalLeaderboard.ui.tsx` | TSX | GeneralLeaderboard UI (tabs, filter, GeneralRankingWidget) | [generalLeaderboard.ui.md](./generalLeaderboard.ui.md) |
| `generalLeaderboard.script.ts` | TypeScript | useGeneralLeaderboardScript, filter/search state, weekly ranges | [generalLeaderboard.script.md](./generalLeaderboard.script.md) |

## Exports

- `GeneralLeaderboardWidget`, `GeneralLeaderboardWidgetProps`
- `GeneralLeaderboard`, `GeneralLeaderboardProps`
- `useGeneralLeaderboardScript`
- `FilterDays`, `GeneralLeaderboardScriptOptions`, `GeneralLeaderboardScriptReturn`
- `useCampaignWeeklyRanges`, `useCurrentWeeklyRange`
