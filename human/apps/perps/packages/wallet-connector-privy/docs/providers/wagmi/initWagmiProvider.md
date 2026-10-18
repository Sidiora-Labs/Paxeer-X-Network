# InitWagmiProvider

## Overview

**InitWagmiProvider** creates a Wagmi config with **createConfig** (chains from `initChains`, optional custom connectors and storage from `wagmiConfig`) and wraps children with **WagmiProvider** and **QueryClientProvider**. If `connectorWalletType.disableWagmi` is true, it renders only `children`.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| initChains | `Chain[]` | Yes | EVM chains for config. |
| wagmiConfig | `InitWagmi` | Yes | connectors, storage. |
| initialState | `any` | No | Optional hydration state. |
| children | `ReactNode` | Yes | App content. |

## Usage example

```tsx
<InitWagmiProvider initChains={initChains} wagmiConfig={{ connectors: [] }}>
  {children}
</InitWagmiProvider>
```
