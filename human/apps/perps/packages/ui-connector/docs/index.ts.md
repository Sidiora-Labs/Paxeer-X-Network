# index.ts

## Overview

Main entry file for `@paxeer-network/ui-connector`. Re-exports public components, hooks, types, and constants used by consumers.

## Exports

| Export | Source | Description |
|--------|--------|-------------|
| `WalletConnectContent` | `./component/walletConnectorContent` | Wallet connect dialog/sheet content component |
| `WalletConnectContentProps` | `./component/walletConnectorContent` | Props type for `WalletConnectContent` |
| `WalletConnectorWidget` | `./component/walletConnector` | Widget used inside wallet connector modal/sheet |
| `WalletConnectorModalId` | `./component/walletConnector` | Modal ID for desktop wallet connector |
| `WalletConnectorSheetId` | `./component/walletConnector` | Sheet ID for mobile wallet connector |
| `useWalletConnectorBuilder` | `./component/useWalletConnectorBuilder` | Hook that builds wallet connector state and handlers |
| `useAuthGuard` | `./component/useAuthGuard` | Hook to check if user meets auth/network requirements |
| `AuthGuard` | `./component/authGuard` | Guard component that shows fallback until auth/network is satisfied |
| `AuthGuardDataTable` | `./component/authGuardDataTable` | DataTable wrapper with auth guard empty state |
| `AuthGuardEmpty` | `./component/authGuardEmpty` | Auth guard with empty state fallback |
| `AuthGuardTooltip` | `./component/authGuardTooltip` | Tooltip that shows auth-related hints by status |
| `useAuthStatus` | `./hooks/useAuthStatus` | Hook returning current auth step enum |
| `AuthStatusEnum` | `./hooks/useAuthStatus` | Enum: WrongNetwork, ConnectWallet, CreateAccount, EnableTrading |

## Usage example

```ts
import {
  AuthGuard,
  WalletConnectorModalId,
  useAuthGuard,
  useAuthStatus,
  AuthStatusEnum,
} from "@paxeer-network/ui-connector";

<AuthGuard descriptions={{ connectWallet: "Please connect wallet" }}>
  <YourTradingContent />
</AuthGuard>

const isReady = useAuthGuard();
const authStep = useAuthStatus();
```
