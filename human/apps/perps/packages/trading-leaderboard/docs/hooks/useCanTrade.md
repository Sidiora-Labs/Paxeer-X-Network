# useCanTrade.ts

## Overview

Hook that returns whether the current user can trade based on account status and app context (network, connect disabled). Uses `@paxeer-network/hooks` and `@paxeer-network/react-app`.

## Exports

### `useCanTrade()`

Returns a boolean: `true` when the user can trade.

**Logic:** `canTrade` is `true` when:

- `!wrongNetwork`
- `!disabledConnect`
- `state.status` is `AccountStatusEnum.EnableTrading` or `AccountStatusEnum.EnableTradingWithoutConnected`

## Usage example

```tsx
import { useCanTrade } from "./hooks/useCanTrade";

function MyComponent() {
  const canTrade = useCanTrade();
  return <Button disabled={!canTrade}>Trade</Button>;
}
```
