# positions

## Overview

Positions table (desktop), list (mobile), and combined multi-account view. Includes row context/provider, column config, close position, TP/SL, share, and reverse button.

## Files

| File | Language | Description |
| ---- | -------- | ----------- |
| [positions.widget](./positions.widget.md) | TSX | `PositionsWidget`, `MobilePositionsWidget`, `CombinePositionsWidget` |
| [positions.ui](./positions.ui.md) | TSX | `Positions`, `MobilePositions`, `CombinePositions` presentational components |
| [positions.script](./positions.script.md) | TS | `usePositionsScript`, `PositionsState` |
| [positionsRowContext](./positionsRowContext.md) | TSX | `PositionsRowContext`, `usePositionsRowContext`, row state type |
| [positionsRowProvider](./positionsRowProvider.md) | TSX | `PositionsRowProvider` – close order state, submit, quantity/price/type |
| [calculatePositions](./calculatePositions.md) | TS | `calculatePositions` – enrich positions with notional, MMR, PnL, TP/SL |
| [combinePositions.script](./combinePositions.script.md) | TS | `useCombinePositionsScript`, `CombinePositionsState`, account grouping |
| [desktop/useColumn](./desktop/useColumn.md) | TSX | `useColumn` – desktop table column config |
| [desktop/components](./desktop/components.md) | TSX | Leverage badge and desktop shared components |
| [desktop/shareButton](./desktop/shareButton/index.md) | TSX | Share PnL button |
| [desktop/triggerPrice](./desktop/triggerPrice.md) | TSX | TP/SL trigger price display |
| [desktop/partialTPSL](./desktop/partialTPSL.md) | TSX | Partial TP/SL column |
| [desktop/quantityInput](./desktop/quantityInput.md) | TSX | Quantity input for close |
| [desktop/priceInput](./desktop/priceInput.md) | TSX | Price input for close |
| [desktop/numeralWithCtx](./desktop/numeralWithCtx.md) | TSX | Numeral using symbol context |
| [desktop/listElement](./desktop/listElement.md) | TSX | List/table cell helpers |
| [desktop/unrealPnLHover](./desktop/unrealPnLHover.md) | TSX | Unrealized PnL hover card |
| [desktop/reversePotisionButton](./desktop/reversePotisionButton.md) | TSX | Reverse position button |
| [mobile/positionCell](./mobile/positionCell/index.md) | TSX | Mobile position cell widget |
| [mobile/tpSLBtn](./mobile/tpSLBtn/index.md) | TSX | Mobile TP/SL button |
| [closePosition](./closePosition/index.md) | TSX | Close position widget and UI |
| [hooks/useSubAccountQuery](./hooks/useSubAccountQuery.md) | TS | Sub-account API query |
| [hooks/useSubAccountTPSL](./hooks/useSubAccountTPSL.md) | TS | Sub-account TP/SL orders |
