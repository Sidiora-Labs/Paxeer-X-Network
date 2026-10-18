# InitAbstractProvider

## Overview

**InitAbstractProvider** wraps children with **AbstractWalletProvider** from `@abstract-foundation/agw-react`, passing the chain: **abstract** for mainnet and **abstractTestnet** for testnet (from **useWalletConnectorPrivy().network**).

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | `ReactNode` | Yes | App content. |

## Usage example

```tsx
<InitAbstractProvider>{children}</InitAbstractProvider>
```
