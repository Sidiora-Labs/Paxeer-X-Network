# InitPrivyProvider

## Overview

**InitPrivyProvider** wraps children with **PrivyProvider** from `@privy-io/react-auth` when `privyConfig` is provided. It builds **PrivyClientConfig** from `initChains` and `network`: default chain, embedded EVM/Solana wallets, and optional external WalletConnect. If `privyConfig` is absent, it renders only `children`.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| privyConfig | `InitPrivy` | No | App ID and config (loginMethods, appearance). |
| initChains | `Chain[]` | Yes | Chains for Privy (EVM). |
| children | `ReactNode` | Yes | App content. |

## Usage example

```tsx
<InitPrivyProvider privyConfig={{ appid: "..." }} initChains={initChains}>
  {children}
</InitPrivyProvider>
```
