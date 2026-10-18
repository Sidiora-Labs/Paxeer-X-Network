# index (package entry)

## Overview

Package entry point for `@paxeer-network/chart`. Re-exports paxeer-network chart components (PnL bar/line/area, asset line/area, volume bar), related types and data item types, Recharts primitives (LineChart, Line, BarChart, Bar, Cell, Area, AreaChart), and the Tailwind chart plugin.

## Exports

### Chart components

- `PnLBarChart` — from `./paxeer-network/pnlBar`
- `PnlLineChart` — from `./paxeer-network/pnlLine`
- `PnlAreaChart` — from `./paxeer-network/pnlArea`
- `AssetLineChart` — from `./paxeer-network/assetLine`
- `AssetAreaChart` — from `./paxeer-network/assetArea`
- `VolBarChart` — from `./paxeer-network/volBar`

### Types

- `PnlLineChartProps`, `AssetChartDataItem` — from `./paxeer-network/assetLine`
- `PnlAreaChartProps` — from `./paxeer-network/assetArea`
- `VolChartDataItem` — from `./paxeer-network/volBar`

### Recharts

- `LineChart`, `Line`, `BarChart`, `Bar`, `Cell`, `Area`, `AreaChart` — from `recharts`

### Tailwind

- `chartPlugin` — from `./tailwindcss/theme`

## Usage example

```ts
import {
  PnLBarChart,
  PnlLineChart,
  AssetLineChart,
  VolBarChart,
  chartPlugin,
} from "@paxeer-network/chart";

<PnLBarChart data={pnlData} />
<PnlLineChart data={cumulativePnl} />
<AssetLineChart data={accountValueData} />
<VolBarChart data={volumeData} />

// tailwind.config.js
import { chartPlugin } from "@paxeer-network/chart";
plugins: [chartPlugin],
```
