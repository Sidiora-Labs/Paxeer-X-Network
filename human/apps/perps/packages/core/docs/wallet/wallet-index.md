# wallet index

> Location: `packages/core/src/wallet/index.ts`

## Overview

Re-exports legacy adapter types as `WalletAdapter`, `getWalletAdapterFunc`, `WalletAdapterOptions`, and `EtherAdapter`. The main `Account` and contract flows use the `WalletAdapter` interface from `walletAdapter.ts` (with ChainNamespace), not this legacy `IWalletAdapter`.

## Exports

- **WalletAdapter** – Alias for `IWalletAdapter` from adapter.ts.
- **getWalletAdapterFunc**, **WalletAdapterOptions** – From adapter.ts.
- **EtherAdapter** – From etherAdapter.ts.

## Usage Example

```ts
import { EtherAdapter } from "@paxeer-network/core";
```
