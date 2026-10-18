# useColors

## Overview

Hook that returns profit, loss, primary, and primaryLight colors. If a `colors` object with profit/loss is passed, those are used; otherwise values come from getThemeColors() (CSS variables). Memoized by colors reference.

## Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| colors | { profit: string; loss: string } | No | Override for profit and loss. |

## Returns

| Property | Type | Description |
|----------|------|-------------|
| profit | string | RGB string. |
| loss | string | RGB string. |
| primary | string | From theme. |
| primaryLight | string | From theme. |

## Usage example

```tsx
const colors = useColors({ profit: "#00B49E", loss: "#FF6B6B" });
<Line stroke={colors.primary} />
```
