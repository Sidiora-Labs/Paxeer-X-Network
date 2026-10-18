# HorizontalMarkets Component

A horizontal scrolling markets component that displays market data above the SymbolInfoBar component.

## Features

- **Horizontal Scrolling**: Displays multiple market items in a horizontal layout with smooth scrolling
- **Market Data**: Shows symbol, price, change percentage, volume, and leverage for each market
- **Interactive**: Click on any market item to select it
- **Market Type Filter**: Clickable filter button with dropdown menu to switch between different market types
- **Responsive**: Adapts to different screen sizes
- **Mock Data**: Currently uses mock data for demonstration purposes

## Usage

### Basic Usage

```tsx
import { HorizontalMarketsWidget } from "@paxeer-network/markets";

<HorizontalMarketsWidget
  symbol={currentSymbol}
  onSymbolChange={handleSymbolChange}
  maxItems={8}
/>;
```

### With Market Type Filter

```tsx
<HorizontalMarketsWidget
  symbol={currentSymbol}
  onSymbolChange={handleSymbolChange}
  maxItems={8}
  defaultMarketType="trending"
/>
```

### With Custom Symbols

```tsx
<HorizontalMarketsWidget
  symbol={currentSymbol}
  onSymbolChange={handleSymbolChange}
  symbols={["PERP_BTC_USDC", "PERP_ETH_USDC", "PERP_SOL_USDC"]}
/>
```

### Above SymbolInfoBar

```tsx
<div className="oui-flex oui-flex-col">
  <HorizontalMarketsWidget
    symbol={symbol}
    onSymbolChange={handleSymbolChange}
  />
  <SymbolInfoBarWidget symbol={symbol} onSymbol={handleSymbolClick} />
</div>
```

## Props

### HorizontalMarketsWidgetProps

| Prop                | Type                           | Default     | Description                        |
| ------------------- | ------------------------------ | ----------- | ---------------------------------- |
| `symbol`            | `string`                       | -           | Current selected symbol            |
| `onSymbolChange`    | `(symbol: API.Symbol) => void` | -           | Callback when symbol changes       |
| `symbols`           | `string[]`                     | `undefined` | Custom list of symbols to display  |
| `maxItems`          | `number`                       | `10`        | Maximum number of items to display |
| `defaultMarketType` | `MarketType`                   | `"all"`     | Default market type filter         |
| `className`         | `string`                       | `undefined` | Additional CSS classes             |

### MarketType Options

- `"all"` - All available markets
- `"recent"` - Recently traded markets
- `"newListing"` - Newly listed markets
- `"favorites"` - User's favorite markets
- `"trending"` - Trending markets

## Filter Functionality

The filter button includes:

- **Click to Open**: Click the filter button to open the dropdown menu
- **Radio Button Selection**: Each option shows a radio button indicating the current selection
- **Horizontal Layout**: All filter options are arranged in a row with wrapping
- **Click Outside to Close**: Click anywhere outside the dropdown to close it
- **Auto-close on Selection**: Selecting an option automatically closes the dropdown
- **Consistent Styling**: Uses the same styling patterns as other paxeer-network UI components

## Component Structure

The component follows the established pattern:

- **`horizontalMarkets.script.ts`**: Data fetching and state management logic
- **`horizontalMarkets.ui.tsx`**: UI component with horizontal scrolling layout
- **`horizontalMarkets.widget.tsx`**: Widget wrapper with MarketsProvider
- **`index.ts`**: Exports for the component

## Styling

The component uses paxeer-network's design system classes:

- `oui-bg-base-1`: Background color
- `oui-border-b oui-border-line-12`: Bottom border
- `oui-overflow-x-auto`: Horizontal scrolling
- `oui-scrollbar-hide`: Hide scrollbar
- `oui-cursor-pointer`: Pointer cursor for interactive elements

## Data Structure

Currently uses mock data with the following structure:

```typescript
{
  symbol: string;
  "24h_close": number;
  "change": number;
  "24h_amount": number;
  leverage: number;
}
```

## Future Enhancements

- Replace mock data with real market data from APIs
- Add loading states
- Add error handling
- Add more customization options
- Add animations and transitions
