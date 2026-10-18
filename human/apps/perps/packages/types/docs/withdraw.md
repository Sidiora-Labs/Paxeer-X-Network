# withdraw

## Overview

Withdraw flow status enum used to represent the current state of the user’s ability or result of a withdraw action.

## Exports

### WithdrawStatus (enum)

| Value | Description |
|-------|-------------|
| NotSupported | Withdraw not supported |
| NotConnected | Wallet not connected |
| Unsettle | PnL unsettled |
| InsufficientBalance | Insufficient balance |
| Normal | Ready to withdraw |

## Usage example

```typescript
import { WithdrawStatus } from "@paxeer-network/types";
if (status === WithdrawStatus.Normal) {
  // allow withdraw
}
```
