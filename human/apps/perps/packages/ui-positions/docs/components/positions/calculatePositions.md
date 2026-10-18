# calculatePositions

## Overview

Maps raw `API.PositionExt[]` to `API.PositionTPSLExt[]` by adding notional, MMR, mm, unrealized PnL/ROI, and full/partial TP/SL from algo orders.

## Exports

### calculatePositions(positions, symbolsInfo, accountInfo, tpslOrders)

- **Parameters**
  - `positions`: `API.PositionExt[]`
  - `symbolsInfo`: `SymbolsInfo` from hooks
  - `accountInfo`: `API.AccountInfo[]`
  - `tpslOrders`: `(API.AlgoOrder & { account_id: string })[]`
- **Returns**: `API.PositionTPSLExt[]`

## Usage example

```typescript
import { calculatePositions } from "./calculatePositions";
const extended = calculatePositions(positions, symbolsInfo, accountInfo, tpslOrders);
```
