# dataSource.ts

## Overview

Static fee tier data: array of tiers with volume_min, volume_max, maker_fee, taker_fee (and optional or). Used by fee tier table.

## Exports

- **`dataSource`** — `FeeDataType[]`: 7 tiers from 0–500k up to 100M+ volume, with corresponding maker/taker fee strings.

## Usage example

```ts
import { dataSource } from "./dataSource";
// dataSource[i].tier, volume_min, volume_max, maker_fee, taker_fee
```
