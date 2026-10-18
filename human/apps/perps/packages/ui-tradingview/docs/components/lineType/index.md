# LineType

## Overview

Dropdown to select chart type: bars, candles, hollow candles, line, area, baseline. Uses icons from `../../icons` and i18n for labels. Selected type is shown as the trigger icon.

## Props (IProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `lineType` | `string` | Yes | Current chart type value ("0", "1", "9", "2", "3", "10") |
| `changeLineType` | `(type: string) => void` | Yes | Callback when user selects a new type |

## Usage example

```tsx
<LineType lineType={lineType} changeLineType={changeLineType} />
```
