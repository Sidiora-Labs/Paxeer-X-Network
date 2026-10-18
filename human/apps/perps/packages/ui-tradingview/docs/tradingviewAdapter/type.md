# tradingviewAdapter type

## Overview

Re-exports TradingView library types (IBrokerConnectionAdapterHost, IChartingLibraryWidget, LibrarySymbolInfo, Bar, etc.) and defines enums and interfaces for chart orders, positions, and colors: ChartOrder, OrderType, Side, ChartMode, ChartPosition, OrderInterface, ColorConfigInterface, AlgoType, OrderCombinationType, TriggerPriceType, TriggerStatusType, OrderTagType, PositionSide, SideType.

## Key types

### ChartOrder

`Order` from library extended with `rootAlgoId?`, `parentAlgoId?`, `areBothPTPAndPSLActivated?`.

### ChartPosition

| Field            | Type         | Description |
| ---------------- | ------------ | ----------- |
| symbol           | string       |             |
| open             | number       |             |
| balance          | number       |             |
| closablePosition | number       |             |
| unrealPnl        | number       |             |
| interest         | number       |             |
| unrealPnlDecimal | number       |             |
| basePriceDecimal | number       |             |
| positionSide?    | PositionSide |             |

### OrderInterface

Order/algo fields: order_id, algo_order_id, symbol, side, quantity, price, trigger_price, type, status, algo_type, etc., plus overview (status, quantity, executed).

### ColorConfigInterface

chartBG?, upColor?, downColor?, pnlUpColor?, pnlDownColor?, pnlZeroColor?, textColor?, qtyTextColor?, font?, closeIconColor?, closeIcon?, volumeUpColor?, volumeDownColor?. The `closeIcon?` field is deprecated in favor of `closeIconColor?` and remains supported for backward compatibility. The misspelled `pnlZoreColor?` field is also deprecated; `closeIconColor?` and `pnlZeroColor?` take precedence when both aliases are provided.

### Enums

- **ChartMode**: BASIC, ADVANCED, UNLIMITED, MOBILE
- **OrderType** (numeric): Limit, Market, Stop, StopLimit, OCO, TrailingStop, PostOnly, Ask, Bid, POSITION_TP, POSITION_SL, BRACKET_TP, BRACKET_SL, BRACKET
- **OrderType** (string): LIMIT, MARKET, IOC, POST_ONLY, FOK, etc.
- **Side**: Buy = 1, Sell = -1
- **SideType**: BUY, SELL
- **AlgoType**: TAKE_PROFIT, STOP_LOSS, OCO, TRAILING_STOP, BRACKET, POSITIONAL_TP_SL, TP_SL, etc.
- **OrderCombinationType**: LIMIT, MARKET, STOP_LIMIT, STOP_MARKET, OCO, etc.
- **TriggerPriceType**, **TriggerStatusType**, **OrderTagType**, **PositionSide**
