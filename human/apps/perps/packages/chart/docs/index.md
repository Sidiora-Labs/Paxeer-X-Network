# Chart Package — Documentation Index

Overview of the `@paxeer-network/chart` package source. This directory mirrors `packages/chart/src` and contains TypeScript/React chart components built on Recharts and D3, including PnL/asset/volume charts and shared axis/legend/grid utilities.

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [paxeer-network](paxeer-network/index.md) | paxeer-network-specific chart components: PnL bar/line/area, asset line/area, volume bar, tooltip, axis label, colors hook |
| [common](common/index.md) | Shared chart primitives: Axis, Legend, Grid |
| [utils](utils/index.md) | Theme colors from CSS variables and Y-axis tick formatter |
| [line](line/index.md) | D3-based Line component for custom line charts |
| [bar](bar/index.md) | D3-based Bar component for custom bar charts |
| [hooks](hooks/index.md) | Chart context and scale state (useChartState, chartContext) |
| [constants](constants/index.md) | Types (Margin, Size, ChartProps, LineChart) and default config |
| [tailwindcss](tailwindcss/index.md) | Tailwind plugin for chart axis tick text-anchor styles |

## Root-level files

| File | Language | Description | Link |
|------|----------|-------------|------|
| index.ts | TypeScript | Package entry: exports PnL/Asset/Vol charts, Recharts re-exports, chartPlugin | [entry.md](entry.md) |
| version.ts | TypeScript | Injects package version into `window.__PAXEER_NETWORK_VERSION__` and exports version string | [version.md](version.md) |
| chart.tsx | React/TSX | Main Chart component with ResizeObserver, margin, scale registration, ChartContext | [chart.md](chart.md) |
| chartBase.tsx | React/TSX | Minimal wrapper that renders ChartRenderer | [chartBase.md](chartBase.md) |
| chartRenderer.tsx | React/TSX | Renders SVG viewBox from context size; used by Chart | [chartRenderer.md](chartRenderer.md) |
| theme.ts | TypeScript | ChartTheme type and defaultTheme (bar tick/gap) | [theme.md](theme.md) |
