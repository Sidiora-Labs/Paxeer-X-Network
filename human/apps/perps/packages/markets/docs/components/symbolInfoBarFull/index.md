# symbolInfoBarFull

## Overview

Full symbol info bar: shows selected symbol, token icon, price/mark/24h data, funding rate, open interest, RWA info, favorites dropdown, and optional countdown. Uses ticker stream, funding rate, and RWA hooks.

## Files

| File | Description |
|------|-------------|
| `symbolInfoBarFull.script.ts` | `useSymbolInfoBarFullScript(symbol)` – data, favorite, funding, RWA, scroll visibility |
| `symbolInfoBarFull.ui.tsx` | `SymbolInfoBarFull` – main UI; props extend script return + `className`, `trailing`, `closeCountdown`, `showCountdown` |
| `symbolInfoBarFull.widget.tsx` | `SymbolInfoBarFullWidget` – wires script + UI; props: script options + `className`, `trailing`, `onSymbolChange`, `closeCountdown`, `showCountdown` |
| `rwaTooltip.tsx` | `RwaTooltip` – RWA tooltip content |
| `dataItem.ui.tsx` | `DataItem` – single data item display |
| `index.ts` | Re-exports UI, script, widget |

## Script return (UseSymbolInfoBarFullScriptReturn)

- `symbol`, `isFavorite`, `favorite`, `data` (ticker), `quotoDp`, `openInterest`, `fundingRate`
- `containerRef`, `leadingElementRef`, `tailingElementRef`, `leadingVisible`, `tailingVisible`, `onScoll`
- `fundingPeriod`, `capFunding`, `floorFunding`, `isRwa`, `open`, `closeTimeInterval`, `openTimeInterval`

## Widget props (SymbolInfoBarFullWidgetPros)

- `symbol` (required), `className`, `trailing`, `onSymbolChange`, `closeCountdown`, `showCountdown`

## Usage example

```tsx
import { SymbolInfoBarFullWidget } from "@paxeer-network/markets";

<SymbolInfoBarFullWidget symbol={symbol} onSymbolChange={setSymbol} />
```
