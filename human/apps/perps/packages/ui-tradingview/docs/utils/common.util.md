# common.util

## Overview

Kline resolution enum and mapping for API/WebSocket (e.g. "1" → "1m", "1D" → "1d"), and localStorage keys for TradingView SDK (interval, lineType, displayControlSetting).

## Exports

### KlineResolution

Enum of resolution strings: RESOLUTION_1m = '1', RESOLUTION_3m, RESOLUTION_5m, … RESOLUTION_1D, RESOLUTION_3D, RESOLUTION_1W, RESOLUTION_1M.

### mapResolution(resolution)

Maps TradingView resolution string to kline API format (e.g. "1" → "1m", "60" → "1h", "1D" → "1d", "1M" → "1M ").

### TradingViewSDKLocalstorageKey

| Key | Value | Description |
|-----|--------|-------------|
| `interval` | "TradingviewSDK.lastUsedTimeBasedResolution" | Last resolution |
| `lineType` | "TradingviewSDK.lastUsedStyle" | Last chart type |
| `displayControlSetting` | "TradingviewSDK.displaySetting" | Display toggles JSON |
