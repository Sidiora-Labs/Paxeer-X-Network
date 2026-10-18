# yieldBearingAssets.ts

> Location: `packages/ui-transfer/src/constants/yieldBearingAssets.ts`

## Overview

Configuration for yield-bearing collateral assets (earn APY while used as collateral). Defines symbol, display name, APY API URL/path, and external link. Chain support is determined by backend `/v1/public/token` (chain_details).

## Exports

### YieldBearingAsset (interface)

| Property | Type | Description |
| -------- | ---- | ----------- |
| symbol | string | e.g. "YUSD", "deUSD" |
| displayName | string | Display name |
| apyApiUrl | string | API URL for APY |
| apyPath | string | Path in response (e.g. "data.efficient_apr") |
| externalUrl | string | Link to issuer |

### YIELD_BEARING_ASSETS

`YieldBearingAsset[]` — list of configured yield-bearing assets.

### isYieldBearingAsset(symbol?: string): boolean

Returns whether the token symbol is configured as yield-bearing.

### getYieldBearingAsset(symbol?: string): YieldBearingAsset | undefined

Returns the config for the given symbol, or `undefined`.

## Usage example

```ts
import {
  YIELD_BEARING_ASSETS,
  isYieldBearingAsset,
  getYieldBearingAsset,
} from "@paxeer-network/ui-transfer";

if (isYieldBearingAsset("YUSD")) {
  const asset = getYieldBearingAsset("YUSD");
}
```
