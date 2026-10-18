# layout.widget

## Overview

Container widget that connects `useTradingRewardsLayoutScript` to `TradingRewardsLayout`. Accepts `ScaffoldProps` (e.g. `leftSideProps?.current`) and passes script result plus props to the layout.

## Exports

### TradingRewardsLayoutWidget (component)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | ReactNode | Yes | Page content. |
| leftSideProps?.current | string | No | Current path for the script. |
| ...rest | ScaffoldProps | No | Other scaffold props. |

## Usage example

```tsx
import { TradingRewardsLayoutWidget } from "./layout.widget";

<TradingRewardsLayoutWidget leftSideProps={{ current: "/rewards/trading" }}>
  <TradingRewards.HomePage />
</TradingRewardsLayoutWidget>
```
