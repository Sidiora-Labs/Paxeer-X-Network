# useAuthStatus.tsx

## Overview

Provides the current “auth step” as an enum value, derived from account status and app context (wrong network, disabled connect). Useful for conditional UI or copy (e.g. “Connect wallet” vs “Create account” vs “Enable trading”).

## Exports

### `AuthStatusEnum`

| Member | Description |
|--------|-------------|
| `WrongNetwork` | User is on wrong network and connect is not disabled |
| `ConnectWallet` | Not connected or connect disabled |
| `CreateAccount` | Connected but not signed in |
| `EnableTrading` | Signed in but trading not enabled, or already enabled / EnableTradingWithoutConnected |

### `useAuthStatus()`

**Returns:** `AuthStatusEnum` — current step based on `useAccount().state.status`, `wrongNetwork`, and `disabledConnect` from `useAppContext()`.

## Usage example

```tsx
import { useAuthStatus, AuthStatusEnum } from "@paxeer-network/ui-connector";

const StatusBanner = () => {
  const step = useAuthStatus();

  switch (step) {
    case AuthStatusEnum.WrongNetwork:
      return <span>Switch network</span>;
    case AuthStatusEnum.ConnectWallet:
      return <span>Connect wallet</span>;
    case AuthStatusEnum.CreateAccount:
      return <span>Create account</span>;
    case AuthStatusEnum.EnableTrading:
      return <span>Enable trading</span>;
  }
};
```
