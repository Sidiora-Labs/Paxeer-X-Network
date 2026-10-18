# PrivyConnectArea

## Overview

**PrivyConnectArea** renders Privy login options (email, Google, Twitter, Telegram) based on `privyConfig.loginMethods` from context. Desktop uses a single-column layout; mobile uses a 2x2 grid. A divider is shown below when Wagmi or Solana is also enabled.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| connect | `(type: any) => void` | Yes | Called when user selects a login method; typically passes `{ walletType: "privy", extraType }`. |

## Usage example

```tsx
<PrivyConnectArea
  connect={(type) => handleConnect({ walletType: WalletConnectType.PRIVY, extraType: type })}
/>
```
