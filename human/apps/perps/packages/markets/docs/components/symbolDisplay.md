# SymbolDisplay

## Overview

Displays a trading symbol with optional token icon. Uses `useSymbolsInfo()` to show `displayName` when available; otherwise falls back to formatted symbol text via `Text.formatted` with `rule="symbol"`.

## Props

Extends `FormattedTextProps` from `@paxeer-network/ui`. Main usage: pass the symbol as `children` (string).

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `children` | string | Yes | - | Symbol (e.g. `PERP_BTC_USDC`) |
| `size` | - | No | `"xs"` | Token icon size when `showIcon` is true |
| `showIcon` | boolean | No | - | Whether to show token icon |
| `className` | string | No | - | Additional CSS class |

## Usage example

```tsx
import { SymbolDisplay } from "@paxeer-network/markets";

<SymbolDisplay showIcon size="sm">PERP_BTC_USDC</SymbolDisplay>
```
