# TimeInterval

## Overview

Resolution selector for the chart. Renders different UIs for desktop (inline tabs) and mobile (default tabs + dropdown "More"). Uses `useMediaQuery(MEDIA_TABLET)` to switch. Exports `TimeInterval`, `DesktopTimeInterval`, `MobileTimeInterval`, and internal `DropDownTimeInterval`.

## Props (IProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `interval` | `string` | Yes | Current resolution value (e.g. "1", "15", "1D") |
| `changeInterval` | `(interval: string) => void` | Yes | Callback when user selects a new interval |

## Usage example

```tsx
<TimeInterval interval={interval} changeInterval={changeInterval} />
```
