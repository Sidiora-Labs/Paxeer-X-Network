# page.tsx (Leaderboard)

## Overview

Leaderboard page that composes campaigns banner, rewards, leaderboard section (general or campaign), and rules. Wraps content in `TradingLeaderboardProvider` and uses `useTradingLeaderboardContext` for current campaign and background.

## Exports

### `LeaderboardPage`

Main page component.

#### Props (`LeaderboardPageProps`)

Extends `GeneralLeaderboardWidgetProps` and `TradingLeaderboardProviderProps`:

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `style` | `React.CSSProperties` | No | Inline styles |
| `className` | `string` | No | CSS class |
| `hideCampaignsBanner` | `boolean` | No | Hide campaigns banner |
| (from Provider) | `campaigns`, `href`, `backgroundSrc`, `dataAdapter`, `campaignId`, `onCampaignChange` | — | See [TradingLeaderboardProvider](../components/provider/index.md) |
| (from GeneralLeaderboard) | `campaignDateRange`, etc. | — | Pass-through to general leaderboard when applicable |

### `LeaderboardSection`

Renders either general leaderboard or campaign leaderboard based on `currentCampaignId` from context.

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `style` | `React.CSSProperties` | No | Inline styles |
| `className` | `string` | No | CSS class |

### `LeaderboardTitle`

Title with gradient underline.

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `title` | `ReactNode` | Yes | Title content |
| `isMobile` | `boolean` | No | Adjusts typography for mobile |

## Usage example

```tsx
import {
  LeaderboardPage,
  LeaderboardSection,
  LeaderboardTitle,
} from "@paxeer-network/trading-leaderboard";

<LeaderboardPage
  campaigns={campaigns}
  campaignId="general"
  backgroundSrc="/bg.mp4"
  hideCampaignsBanner={false}
/>
```
