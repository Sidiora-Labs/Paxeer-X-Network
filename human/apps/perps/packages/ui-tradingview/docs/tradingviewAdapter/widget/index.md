# widget

## Overview

TradingView widget lifecycle: Widget class (create, remove, setSymbol, setResolution, executeActionById, changeLineType, subscribeClick), option merging (getOptions) for enabled/disabled features by mode, persistUtils for chart/adapter settings (getChartData, saveChartData, saveChartAdapterSetting), util (waitForElm), and chart_hack for iframe DOM tweaks.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Widget class: creates TradingView.widget, manages datafeed/broker, auto-save, chart key | [widget.md](./widget.md) |
| `option.ts` | TypeScript | getOptions: merge enabled/disabled features by ChartMode, broker_config | [option.md](./option.md) |
| `adapter.ts` | TypeScript | getSettingAdapter for settings persistence (legacy/local) | [adapter.md](./adapter.md) |
| `persistUtils.ts` | TypeScript | getChartData, saveChartData, saveChartAdapterSetting, clearChartCache, copyChartData, deleteChartData | [persistUtils.md](./persistUtils.md) |
| `util.ts` | TypeScript | waitForElm(iframeDocument, selector): Promise<Element \| null> | [util.md](./util.md) |
| `chart_hack.ts` | TypeScript | ChartHack for iframe document patches | [chart_hack.md](./chart_hack.md) |
