# curEpoch

## Overview

Current epoch block: countdown, epoch id/dates, max reward, estimated rewards (with broker tooltip), and optional pause countdown. Handles active / paused / ended states and uses `AuthGuard` for connect CTA.

## Files

| File | Language | Description |
|------|----------|-------------|
| [curEpoch.script](curEpoch.script.md) | TSX | `useCurEpochScript`, `CurEpochReturns`, and rewards tooltip data. |
| [curEpoch.ui](curEpoch.ui.md) | TSX | `CurEpoch` UI and internal stat/countdown/tooltip components. |
| [curEpoch.widget](curEpoch.widget.md) | TSX | `CurEpochWidget` that wires script to UI. |
| [rewardsTooltip](rewardsTooltip.md) | TSX | `RewardsTooltip` and `RewardsTooltipProps`. |
