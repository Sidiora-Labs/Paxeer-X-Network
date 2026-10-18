# TopBar

## Overview

Simple wrapper component for the chart top toolbar. Renders a flex container with fixed height and padding that accepts `children` (e.g. time interval, display control, line type, indicators, settings, fullscreen icons).

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `children` | `ReactNode` | No | Toolbar content |

## Usage example

```tsx
<TopBar>
  <TimeInterval interval={interval} changeInterval={changeInterval} />
  <OperateButton onClick={openChartIndicators}><IndicatorsIcon /></OperateButton>
  <DesktopDisplayControl ... />
</TopBar>
```
