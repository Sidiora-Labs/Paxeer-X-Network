# AbstractConnectArea (abstractConnector)

## Overview

**AbstractConnectArea** shows a single “Abstract” option with the Abstract logo. Clicking calls `connect()` to start the Abstract Global Wallet connection flow.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| connect | `() => void` | Yes | Called when user taps Abstract. |

## Usage example

```tsx
<AbstractConnectArea connect={() => handleConnect({ walletType: WalletConnectType.ABSTRACT })} />
```
