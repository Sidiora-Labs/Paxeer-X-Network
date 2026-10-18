# ui-positions

## Overview

`@paxeer-network/ui-positions` provides UI components and logic for perpetual positions, position history, liquidation history, funding fee history, close position, close all positions, and reverse position. Documentation is generated from `packages/ui-positions/src`.

## Package exports (from `index.ts`)

- **Context**: `usePositionsRowContext`
- **Funding fee**: `FundingFeeHistoryUI`, `FundingFeeButton`
- **Positions**: `PositionsWidget`, `MobilePositionsWidget`, `CombinePositionsWidget`
- **Types**: `PositionsProps`, `SortType`
- **Hooks**: `useTabSort`, `PositionsTabName`, `useSort`, `sortList`
- **Dialog**: `MarketCloseConfirmID` (registered with `MarketCloseConfirm`)
- **Re-exports**: `positionHistory`, `liquidation`, `closeAllPositions`, `reversePosition`

## Directory structure

| Directory | Description |
| --------- | ----------- |
| [components](./components/index.md) | Positions, position history, liquidation, funding fee, close/reverse position UI |
| [provider](./provider/index.md) | Symbol context and provider |
| [types](./types/index.md) | Shared types (`PositionsProps`, `SortType`) |
| [utils](./utils/index.md) | Sorting and date/range utilities |

## Root-level files

| File | Language | Description |
| ---- | -------- | ----------- |
| [version](./version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` |
| [constants](./constants.md) | TypeScript | Storage key and liquidation threshold constants |
