# ranking

General and campaign ranking widgets and shared column definitions.

## Subdirectories

| Directory | Description | Link |
|-----------|-------------|------|
| generalRanking | General ranking widget and script | [generalRanking/index.md](./generalRanking/index.md) |
| campaignRanking | Campaign ranking widget | [campaignRanking/index.md](./campaignRanking/index.md) |
| shared | Column definitions, ranking UI, volume/pnl column titles | — |

## Files (shared)

| File | Language | Description |
|------|----------|-------------|
| `shared/column.tsx` | TSX | Column field definitions (RankingColumnFields) |
| `shared/ranking.ui.tsx` | TSX | Ranking table/list UI |
| `shared/volumeColumnTitle.tsx` | TSX | Volume column header |
| `shared/pnLColumnTitle.tsx` | TSX | PnL column header |
| `shared/util.ts` | TypeScript | Ranking helpers |

## Main exports

- `GeneralRankingWidget`, `GeneralRankingWidgetProps`, `useGeneralRankingScript`
- `CampaignRankingWidget`, `CampaignRankingWidgetProps`, `useCampaignRankingScript` (if any)
