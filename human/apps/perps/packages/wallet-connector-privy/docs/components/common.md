# common

## Overview

Shared UI pieces: **RenderPrivyTypeIcon** (email, google, twitter, telegram icons), **RenderWalletIcon** (EVM/Solana connector or adapter icon), and **EVMChainPopover** (chain list popover for EVM).

## Exports

### RenderPrivyTypeIcon

Renders the icon for a Privy login type.

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| type | `string` | Yes | e.g. `email`, `google_oauth`, `twitter_oauth`, `telegram`. |
| size | `number` | Yes | Icon size in px. |
| black | `boolean` | No | Use black variant for light backgrounds. |

### RenderWalletIcon

Renders icon for a Wagmi **Connector** or Solana **WalletAdapter** (uses `getWalletIcon` or adapter icon).

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| connector | `Connector \| WalletAdapter` | Yes | Wagmi connector or Solana adapter. |

### EVMChainPopover

Wraps children in a popover that shows supported EVM chains (from context). Used next to “Evm” label to switch chain.

## Usage example

```tsx
<RenderPrivyTypeIcon type={linkedAccount.type} size={18} black />
<RenderWalletIcon connector={connector} />
<EVMChainPopover>
  <MoreIcon />
</EVMChainPopover>
```
