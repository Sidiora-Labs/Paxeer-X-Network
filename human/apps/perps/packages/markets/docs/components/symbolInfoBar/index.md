# symbolInfoBar — Component Module

## symbolInfoBar Responsibilities

Exports the compact symbol info bar: UI component, script hook, and widget. Displays key info for the current symbol (e.g. price, change, volume) in a compact bar; used in headers or above trading views.

## symbolInfoBar Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| SymbolInfoBar | Component | UI | Renders compact symbol info bar |
| useSymbolInfoBarScript | Hook | Logic | Fetches/derives symbol data for the bar |
| SymbolInfoBarWidget | Component | Widget | Composes script + SymbolInfoBar UI |

## SymbolInfoBar Input/Output

- **Input**: Props from useSymbolInfoBarScript (symbol, data fields, loading, etc.).
- **Output**: Bar UI showing symbol name and key metrics.

## useSymbolInfoBarScript

- **Input**: Optional symbol override or config.
- **Output**: Symbol data (price, 24h change, volume, etc.) and loading state for the bar.

## Dependencies

- **Upstream**: @paxeer-network/hooks (market data), type (API.Symbol), ui components.
- **Downstream**: Markets header, trading layout, pages that show current symbol info.

## symbolInfoBar Example

```tsx
import { SymbolInfoBarWidget, SymbolInfoBar, useSymbolInfoBarScript } from "@paxeer-network/markets";

<SymbolInfoBarWidget />

// Or with custom symbol:
function CustomBar() {
  const script = useSymbolInfoBarScript({ symbol: "PERP_BTC_USDC" });
  return <SymbolInfoBar {...script} />;
}
```
