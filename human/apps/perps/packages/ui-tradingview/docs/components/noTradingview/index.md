# NoTradingview

## Overview

Fallback UI shown when the TradingView script URL is not provided. Displays i18n message and links (e.g. TradingView advanced charts, paxeer-network TradingViewConfig docs).

## Props

None. Uses `useTranslation()` and `<Trans>` for text and link placeholders.

## Usage example

```tsx
{!tradingViewScriptSrc ? <NoTradingview /> : <div ref={chartRef} />}
```
