# theme (utils)

## Overview

Reads OUI theme CSS variables from `document.documentElement` and returns an object of RGB color strings (e.g. for primary, success, loss, profit). Used by `useColors` in paxeer-network charts.

## Exports

### getThemeColors

Returns an object with:

| Property | Source variable | Description |
|----------|------------------|-------------|
| primary | --oui-color-primary | Primary color. |
| primaryLight | --oui-color-primary-light | Light primary. |
| secondary | --oui-color-secondary | Secondary. |
| success | --oui-color-success | Success. |
| warning | --oui-color-warning | Warning. |
| danger | --oui-color-danger | Danger. |
| info | --oui-color-info | Info. |
| loss | --oui-color-trading-loss | Trading loss. |
| profit | --oui-color-trading-profit | Trading profit. |

Values are converted to `rgb(r,g,b)` form via internal `convertToRGB`.

## Usage example

```ts
import { getThemeColors } from "./utils/theme";

const colors = getThemeColors();
// colors.profit, colors.loss, etc.
```
