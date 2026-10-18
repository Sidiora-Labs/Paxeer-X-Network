# ui-connector (src)

Package `@paxeer-network/ui-connector` source: wallet connection UI, auth guards, and related hooks/components.

## Directory structure

| Directory | Description |
|-----------|-------------|
| [component](./component/index.md) | Wallet connector modal/sheet, auth guard components, steps, and builder hook |
| [constants](./constants/index.md) | Alert message types and labels |
| [hooks](./hooks/index.md) | Auth status hook |

## Top-level files

| File | Language | Description |
|------|----------|-------------|
| [index.ts](./index.ts.md) | TypeScript | Public API re-exports for the package |
| [version.ts](./version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration |

---

# index.ts (entry point)

## Overview

Main entry file for `@paxeer-network/ui-connector`. Re-exports public components, hooks, types, and constants.

## Exports

| Export | Description |
|--------|-------------|
| `WalletConnectContent`, `WalletConnectContentProps` | Wallet connect dialog/sheet content and its props |
| `WalletConnectorWidget`, `WalletConnectorModalId`, `WalletConnectorSheetId` | Widget and modal/sheet IDs for wallet connector |
| `useWalletConnectorBuilder` | Hook that builds wallet connector state and handlers |
| `useAuthGuard` | Hook to check if user meets auth/network requirements |
| `AuthGuard`, `AuthGuardDataTable`, `AuthGuardEmpty`, `AuthGuardTooltip` | Auth guard components |
| `useAuthStatus`, `AuthStatusEnum` | Current auth step hook and enum |

## Usage example

```ts
import {
  AuthGuard,
  useAuthGuard,
  useAuthStatus,
  AuthStatusEnum,
} from "@paxeer-network/ui-connector";

<AuthGuard descriptions={{ connectWallet: "Please connect wallet" }}>
  <YourContent />
</AuthGuard>

const isReady = useAuthGuard();
const authStep = useAuthStatus(); // AuthStatusEnum
```
