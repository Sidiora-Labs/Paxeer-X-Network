# provider

Trading leaderboard React context and provider. Supplies campaigns, current campaign, user data, statistics, date range, and optional data adapter.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.tsx` | TSX | TradingLeaderboardContext, TradingLeaderboardProvider, useTradingLeaderboardContext | (this doc) |

## Exports

### `TradingLeaderboardContext`

React context holding `TradingLeaderboardState`.

### `TradingLeaderboardProvider`

Provider component. Fetches referral info, campaign stats (when not "general"), and computes filtered campaigns, current campaign with tiered prize pool, and campaign date range.

#### Props (`TradingLeaderboardProviderProps`)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `campaigns` | `CampaignConfig[]` | No | Campaign list; if omitted, campaigns section is hidden |
| `href` | `{ trading?: string }` | No | e.g. default "trading now" URL |
| `backgroundSrc` | `string` | No | Image or video URL for background |
| `dataAdapter` | `(info: { page; pageSize }) => { loading; dataSource?; dataList?; userData?; updatedTime?; meta? }` | No | Custom data source; overrides default fetching |
| `campaignId` | `string \| number` | No | Current campaign ID (default "general") |
| `onCampaignChange` | `(id: string \| number) => void` | No | Called when campaign changes |
| `children` | `ReactNode` | Yes | Child tree |

### `useTradingLeaderboardContext()`

Returns current `TradingLeaderboardState`.

### Type: `TradingLeaderboardState`

| Field | Type | Description |
|-------|------|-------------|
| `campaigns` | `CampaignConfig[]?` | Sorted campaign list |
| `backgroundSrc` | `string?` | Background asset URL |
| `href` | `{ trading? }` | Link config |
| `currentCampaignId` | `string \| number` | Active campaign ID |
| `currentCampaign` | `CampaignConfig?` | Resolved campaign (with tiered prize_pools applied) |
| `onCampaignChange` | `(id) => void` | Change handler |
| `userData` | `UserData?` | Leaderboard user data for rewards |
| `setUserData` | `(userdata) => void`? | Set user data |
| `updatedTime` | `number?` | Snapshot/update time |
| `setUpdatedTime` | `(t?) => void`? | Set updated time |
| `dataAdapter` | function? | Custom data adapter |
| `campaignDateRange` | `{ start_time; end_time }?` | Current campaign date range |
| `statistics` | `CampaignStatistics` | total_participants, total_volume |
| `setStatistics` | `(statistics) => void`? | Set statistics |

## Usage example

```tsx
import {
  TradingLeaderboardProvider,
  useTradingLeaderboardContext,
} from "@paxeer-network/trading-leaderboard";

<TradingLeaderboardProvider
  campaigns={campaigns}
  campaignId="general"
  backgroundSrc="/bg.mp4"
  href={{ trading: "/trade" }}
>
  <YourLeaderboardContent />
</TradingLeaderboardProvider>
```
