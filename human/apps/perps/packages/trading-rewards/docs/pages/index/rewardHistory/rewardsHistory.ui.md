# rewardsHistory.ui

## Overview

Reward history section: title, then either desktop `DataTable` or mobile `ListView` (via `useMediaQuery`). Columns: Epoch, Start/End date, Epoch rewards, Rewards earned (with optional `RewardsTooltip`). Uses `AuthGuardEmpty` for signed-in empty state.

## Exports

### RewardHistory (component)

Accepts `RewardsHistoryReturns`: data, originalData, pagination, isLoading. Renders list with responsive layout.

## Internal

- **List**: Chooses mobile ListView or desktop DesktopList.
- **MobileCell**: Single row for mobile.
- **DesktopList**: DataTable with columns and row tooltip.
- **formatTimestamp**: Splits timestamp into date and time strings.

## Usage example

```tsx
<RewardHistory {...rewardsHistoryScriptReturn} />
```
