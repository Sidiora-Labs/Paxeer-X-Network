# EVMConnectArea (wagmiConnector)

## Overview

**EVMConnectArea** lists all Wagmi connectors from **useWagmiWallet** and lets the user pick one to connect. Each item shows **RenderWalletIcon** and the connector name. On mobile, WalletConnect may close the drawer after selection.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| connect | `(connector: Connector) => void` | Yes | Called with the selected Wagmi connector. |

## Usage example

```tsx
<EVMConnectArea
  connect={(connector) => handleConnect({ walletType: WalletConnectType.EVM, connector })}
/>
```
