# authGuardTooltip.tsx

## Overview

`AuthGuardTooltip` wraps children in a `Tooltip` that shows a hint based on current account and network status (connect wallet, create account, enable trading, wrong network). Can reduce opacity of children when not fully authorized.

## Exports

### `AuthGuardTooltip`

#### Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `content` | `string` | No | — | Override tooltip content (used in default switch) |
| `align` | `"center" \| "end" \| "start"` | No | — | Tooltip alignment |
| `alignOffset` | `number` | No | — | Offset for alignment |
| `side` | `"top" \| "right" \| "bottom" \| "left"` | No | — | Tooltip side |
| `sideOffset` | `number` | No | — | Offset from side |
| `opactiy` | `number` | No | `90` | Opacity applied to children when not EnableTrading (typo in prop name) |
| `tooltip` | `{ connectWallet?, signIn?, enableTrading?, wrongNetwork? }` | No | i18n setup tooltips | Per-status tooltip text |
| `children` | `ReactNode` | Yes | — | Wrapped content |

## Usage example

```tsx
import { AuthGuardTooltip } from "@paxeer-network/ui-connector";

<AuthGuardTooltip side="top" opactiy={80}>
  <button>Place order</button>
</AuthGuardTooltip>
```
