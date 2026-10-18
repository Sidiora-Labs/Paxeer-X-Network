# ConnectDrawer

## Overview

**ConnectDrawer** is a right-side **SimpleSheet** that shows either the connector selection (Privy, EVM, Solana, Abstract) when not connected, or the “My Wallet” view (RenderPrivyWallet or RenderNonPrivyWallet) when connected. It also shows a PWA install entry on mobile and optional terms of use.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| open | `boolean` | Yes | Whether the drawer is open. |
| onChangeOpen | `(open: boolean) => void` | Yes | Called when open state should change. |
| headerProps | `{ mobile?: ReactNode }` | No | Custom header for mobile. |

## Usage example

```tsx
<ConnectDrawer
  open={openConnectDrawer}
  onChangeOpen={setOpenConnectDrawer}
  headerProps={{ mobile: <MobileHeader /> }}
/>
```
