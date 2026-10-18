# generalLeaderboard.widget.tsx

## Overview

Connects `useGeneralLeaderboardScript` to `GeneralLeaderboard` UI. Accepts optional `campaignDateRange` for weekly filter and passes through `style` / `className`.

## Exports

### `GeneralLeaderboardWidget`

#### Props (`GeneralLeaderboardWidgetProps`)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `style` | `React.CSSProperties` | No | Inline styles |
| `className` | `string` | No | CSS class |
| `campaignDateRange` | `{ start_time: Date \| string; end_time: Date \| string }` | No | Campaign period for weekly ranges |

## Usage example

```tsx
import { GeneralLeaderboardWidget } from "@paxeer-network/trading-leaderboard";

<GeneralLeaderboardWidget
  campaignDateRange={{
    start_time: "2025-01-01",
    end_time: "2025-01-31",
  }}
  className="oui-mt-10"
/>
```
