# generalLeaderboard.ui.tsx

## Overview

`GeneralLeaderboard` UI: responsive layout with LeaderboardFilter, LeaderboardTabs (volume/pnl), and GeneralRankingWidget. Uses `GeneralLeaderboardScriptReturn` for filter/search/tab state.

## Exports

### `GeneralLeaderboard`

#### Props (`GeneralLeaderboardProps`)

Extends `GeneralLeaderboardScriptReturn` and:

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `style` | `React.CSSProperties` | No | Inline styles |
| `className` | `string` | No | CSS class |
| `campaignDateRange` | `{ start_time; end_time }?` | No | Campaign date range (for script) |

Script return props include: `filterItems`, `onFilter`, `dateRange`, `filterDay`, `updateFilterDay`, `setDateRange`, `searchValue`, `onSearchValueChange`, `clearSearchValue`, `activeTab`, `onTabChange`, `useCampaignDateRange`, `weeklyRanges`, `currentOrAllTimeRange`.

On mobile, columns are reduced to rank, address, and one metric (volume or pnl by tab). On desktop, rank, address, volume, pnl are shown.
