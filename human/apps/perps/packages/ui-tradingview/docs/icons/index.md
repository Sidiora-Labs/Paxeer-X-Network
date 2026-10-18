# icons

## Overview

SVG icon components used in the chart toolbar and display control: CaretIcon, DisplaySettingIcon, LineTypeIcon, IndicatorsIcon, SettingIcon, BarIcon, CandlesIcon, HollowCandlesIcon, LineIcon, AreaIcon, BaseLineIcon, UnSelectIcon, SelectedIcon. All accept `SVGProps<SVGSVGElement>` and spread `...props`.

## Exports

| Export | Description |
|--------|-------------|
| `CaretIcon` | Dropdown caret |
| `DisplaySettingIcon` | Display settings (overlays) |
| `LineTypeIcon` | Chart type |
| `IndicatorsIcon` | Indicators dialog |
| `SettingIcon` | Chart properties |
| `BarIcon` | Bar chart |
| `CandlesIcon` | Candlestick |
| `HollowCandlesIcon` | Hollow candlestick |
| `LineIcon` | Line chart |
| `AreaIcon` | Area chart |
| `BaseLineIcon` | Baseline chart |
| `UnSelectIcon` | Unselected state (display control) |
| `SelectedIcon` | Selected state (display control) |

## Usage example

```tsx
import { IndicatorsIcon, SettingIcon } from "../icons";
<IndicatorsIcon /> <SettingIcon className="oui-w-5 oui-h-5" />
```
