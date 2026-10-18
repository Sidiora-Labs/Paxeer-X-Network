# WalletCard

## Overview

**WalletCard** displays a single wallet: address, chain type (EVM / Solana / Abstract), copy button, and either disconnect (non-Privy) or export menu (Privy). Supports multi-wallet active state with a checkbox.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| type | `WalletType` | Yes | - | EVM, SOL, or Abstract. |
| address | `string` | Yes | - | Wallet address. |
| isActive | `boolean` | Yes | - | Whether this wallet is the active one. |
| isPrivy | `boolean` | No | - | If true, show export option instead of disconnect. |
| isMulti | `boolean` | No | - | If true, show active checkbox. |
| onActiveChange | `(active: boolean) => void` | Yes | - | Called when active state changes (e.g. switch wallet). |

## Usage example

```tsx
<WalletCard
  type={WalletType.EVM}
  address={address}
  isActive={isActive(type)}
  isPrivy={true}
  isMulti={walletList.length > 1}
  onActiveChange={() => switchWallet(type)}
/>
```
