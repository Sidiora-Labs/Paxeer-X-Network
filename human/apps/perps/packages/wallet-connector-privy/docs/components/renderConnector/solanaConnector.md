# SOLConnectArea (solanaConnector)

## Overview

**SOLConnectArea** lists Solana wallet adapters from **useSolanaWallet** and lets the user pick one. Each item shows **RenderWalletIcon** and the adapter name; clicking calls `connect(item.adapter)`.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| connect | `(walletAdapter: WalletAdapter) => void` | Yes | Called with the selected Solana adapter. |

## Usage example

```tsx
<SOLConnectArea
  connect={(walletAdapter) =>
    handleConnect({ walletType: WalletConnectType.SOL, walletAdapter })
  }
/>
```
