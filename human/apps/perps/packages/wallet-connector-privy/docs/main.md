# main

## Overview

**Main** wraps the app with `WalletConnectorContext` from `@paxeer-network/hooks`, providing `connect`, `disconnect`, `wallet`, `setChain`, `connectedChain`, and `namespace`. It renders **ConnectDrawer** and wires the connector’s open state and target wallet type.

## Exports

### Main

React component that:

- Uses `useWallet` and `useWalletConnectorPrivy` to get wallet state and drawer controls.
- Implements a `connect` function that opens the connect drawer (and handles `autoSelect` from wallet-connector).
- Provides `WalletConnectorContextState` via `WalletConnectorContext.Provider`.
- Renders `ConnectDrawer` and `children`.

#### Props (MainProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| headerProps | `{ mobile?: ReactNode }` | No | Header content for mobile. |
| children | `ReactNode` | Yes | App content. |

## Usage example

```tsx
// Typically used inside WalletConnectorPrivyProvider via provider.tsx
<Main headerProps={{ mobile: <MobileHeader /> }}>
  {children}
</Main>
```
