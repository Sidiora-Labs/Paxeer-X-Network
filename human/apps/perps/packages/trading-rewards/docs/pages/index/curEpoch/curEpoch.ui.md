# curEpoch.ui

## Overview

Presentational current-epoch block: countdown bar, epoch id & date range, max reward, “My est. rewards” with ORDER/esORDER icon and optional `RewardsTooltip`, and optional pause countdown + Twitter link. Renders different content when epoch is active vs paused/ended.

## Exports

### CurEpoch (component)

Accepts `CurEpochReturns` (all fields from `useCurEpochScript`). Renders countdown, stats, est. rewards, and `AuthGuard` placeholder.

### ArrowRightIcon (component)

SVG arrow icon used by the Twitter link.

## Internal components

- **EstRewards**: Displays “My est. rewards” with icon and optional tooltip.
- **Statics**: Label + highlight + optional text.
- **Countdown**: Days/hours/minutes/seconds to target timestamp.
- **TwitterLInk**: Link to PaxeerNetworkNetwork Twitter.

## Usage example

```tsx
<CurEpoch {...curEpochScriptReturn} />
```
