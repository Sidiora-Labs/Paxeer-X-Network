# icons.tsx

> Location: `packages/ui-transfer/src/icons.tsx`

## Overview

React SVG icon components used in transfer/deposit/withdraw UI: exchange, arrows, refresh, transfer vertical. All accept standard SVG props and use `currentColor`.

## Exports (Components)

| Component | Description |
| --------- | ----------- |
| ExchangeIcon | 12×12 swap/exchange arrows |
| ArrowDownIcon | 20×21 down arrow |
| SelectArrowDownIcon | 10×10 dropdown arrow |
| RefreshIcon | 12×12 refresh |
| TransferVerticalIcon | 20×21 vertical transfer |

All are `FC<SVGProps<SVGSVGElement>>`.

## Usage example

```tsx
import { ExchangeIcon, ArrowDownIcon, RefreshIcon } from "@paxeer-network/ui-transfer/icons";

<ExchangeIcon className="text-inherit" />
<ArrowDownIcon />
<RefreshIcon />
```
