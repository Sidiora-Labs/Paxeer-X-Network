# useAuthGuard.ts

## Overview

Hook that returns whether the current user meets the required account status and network (no wrong network, connect not disabled). Used to gate UI or logic without rendering the full `AuthGuard` component.

## Exports

### `useAuthGuard(status?: AccountStatusEnum): boolean`

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `status` | `AccountStatusEnum` | No | Required status; default is `EnableTrading` or `EnableTradingWithoutConnected` based on current state |

**Returns:** `true` when `state.status >= _status` and `!wrongNetwork` and `!disabledConnect`; otherwise `false`.

## Usage example

```tsx
import { useAuthGuard } from "@paxeer-network/ui-connector";
import { AccountStatusEnum } from "@paxeer-network/types";

const MyComponent = () => {
  const canTrade = useAuthGuard(AccountStatusEnum.EnableTrading);
  const canSubmit = useAuthGuard(); // default required status

  return (
    <button disabled={!canTrade}>Place order</button>
  );
};
```
