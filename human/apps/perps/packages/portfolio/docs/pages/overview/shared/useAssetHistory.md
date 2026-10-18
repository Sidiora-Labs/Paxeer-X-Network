# useAssetHistory.ts

## Overview

Hook for asset/performance history over a configurable period (7D, 30D, 90D). Fetches daily stats, transfer history, asset history; computes PnL, ROI, volume; supports realtime merge of current day. Used by overview performance and charts.

## Exports

### Enums

- **`PeriodType`** — WEEK = "7D", MONTH = "30D", QUARTER = "90D"

### Hooks

- **`useAssetsHistoryData(localKey, options?)`**  
  - `localKey`: localStorage key for period.  
  - `options.isRealtime`: merge today's calculated row.  
  - Returns: periodTypes, period, onPeriodChange, periodLabel, curPeriod, data (daily rows), aggregateValue (vol, pnl, roi), createFakeData, volumeUpdateDate.

### Types

- **`useAssetsHistoryDataReturn`** — Return type of useAssetsHistoryData.

## Dependencies

Uses useAccount, useAssetsHistory, useCollateral, useIndexPricesStream, useLocalStorage, usePrivateQuery, useStatisticsDaily, useBalanceTopic.
