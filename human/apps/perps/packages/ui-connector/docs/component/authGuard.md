# authGuard.tsx

## Overview

`AuthGuard` wraps children and only renders them when the account status is at least the required status and the network is correct. Otherwise it renders a fallback (custom or default): connect wallet, switch chain, sign-in, or enable trading.

## Exports

### `alertMessages` (type)

| Property | Type | Description |
|----------|------|-------------|
| `connectWallet?` | `string` | Description for connect-wallet state |
| `switchChain?` | `string` | Description for wrong-network state |
| `enableTrading?` | `string` | Description for enable-trading state |
| `signin?` | `string` | Description for sign-in state |

### `AuthGuard` (component)

#### Props

Extends `React.ButtonHTMLAttributes<HTMLButtonElement>` and:

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `fallback` | `(props: { validating, status, wrongNetwork }) => ReactElement` | No | — | Custom fallback instead of default buttons |
| `status` | `AccountStatusEnum` | No | `EnableTrading` (or `EnableTradingWithoutConnected` when applicable) | Required state to be satisfied |
| `bridgeLessOnly` | `boolean` | No | — | Passed to chain selector when switching network |
| `buttonProps` | `ButtonProps` | No | — | Props for default fallback buttons |
| `descriptions` | `alertMessages` | No | — | Override default descriptions |
| `labels` | `alertMessages` | No | — | Override default button labels |
| `classNames` | `{ root?, description? }` | No | — | CSS classes for root and description |
| `networkId` | `NetworkId` | No | — | Preferred network for chain selector |

Children are rendered only when `state.status >= status` and `!wrongNetwork` and `!disabledConnect`.

## Usage example

```tsx
import { AuthGuard } from "@paxeer-network/ui-connector";

<AuthGuard
  status={AccountStatusEnum.EnableTrading}
  descriptions={{ connectWallet: "Please connect wallet to trade" }}
  buttonProps={{ size: "lg" }}
>
  <TradingPanel />
</AuthGuard>
```
