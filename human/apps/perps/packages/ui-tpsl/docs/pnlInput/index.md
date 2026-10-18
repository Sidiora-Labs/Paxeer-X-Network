# pnlInput

## Overview

PnL-based input builder and UI: computes trigger/order prices from PnL and exposes a widget that composes `usePNLInputBuilder` and `PNLInput` UI. Used internally for TP/SL price derivation from PnL.

## Files

| File | Language | Description |
|------|----------|-------------|
| pnlInput.widget | TSX | `PnlInputWidget` – composes builder hook and UI |
| pnlInput.ui | TSX | `PNLInput` – presentational PnL input |
| useBuilder.script | TS | `usePNLInputBuilder`, `BuilderProps` – PnL calculation and state |

## Exports

- `PnlInputWidget` (from widget); builder and UI are internal to the package.

### PnlInputWidget props

- Extends `BuilderProps` (from `useBuilder.script`) plus:
  - `testId?: string`
  - `quote: string`

## Usage example

```tsx
import { PnlInputWidget } from "@paxeer-network/ui-tpsl"; // if exported from package
<PnlInputWidget quote="USDC" {...builderProps} />
```
