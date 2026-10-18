# paxeer-network — paxeer-network chart components

paxeer-network-specific chart components built on Recharts: PnL (bar/line/area), asset (line/area), volume bar, shared tooltip, X-axis label, and a colors hook reading theme CSS variables.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| pnlBar.tsx | React/TSX | PnL bar chart with rounded bars, profit/loss colors, custom tooltip and cross cursor | [pnlBar.md](pnlBar.md) |
| pnlLine.tsx | React/TSX | Cumulative PnL line chart with optional invisibility and ResponsiveContainer | [pnlLine.md](pnlLine.md) |
| pnlArea.tsx | React/TSX | Cumulative PnL area chart with gradient fill and baseValue | [pnlArea.md](pnlArea.md) |
| assetLine.tsx | React/TSX | Asset value line/area chart; mobile uses AreaChart, desktop LineChart | [assetLine.md](assetLine.md) |
| assetArea.tsx | React/TSX | Asset value area chart with gradient and tooltip | [assetArea.md](assetArea.md) |
| volBar.tsx | React/TSX | Volume bar chart with optional tooltip rm/dp and opacity per cell | [volBar.md](volBar.md) |
| xAxisLabel.tsx | React/TSX | X-axis tick label showing first tick value or "Now" for last | [xAxisLabel.md](xAxisLabel.md) |
| customTooltip.tsx | React/TSX | PaxeerNetworkChartTooltip: label, value, unit, prefix, coloring, dp/rm | [customTooltip.md](customTooltip.md) |
| useColors.ts | TypeScript | Hook returning profit/loss/primary/primaryLight from props or theme CSS variables | [useColors.md](useColors.md) |
