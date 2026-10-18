# storageKey

## Overview

LocalStorage key constants used by the paxeer-network SDK for persisting wallet type (Ledger), connector, chain, link device state, TradingView fullscreen, and order entry sort keys.

## Exports

| Name | Value | Description |
|------|-------|-------------|
| LedgerWalletKey | `"paxeer-network:ledger-wallet"` | Ledger wallet flag |
| ConnectorKey | `"paxeer-network:connectorKey"` | Connector identifier |
| ChainKey | `"order:chain"` | Selected chain |
| LinkDeviceKey | `"paxeer-network_link_device"` | Link device state |
| TradingviewFullscreenKey | `"paxeer-network:tradingview-fullscreen"` | TradingView fullscreen state |
| OrderEntrySortKeys | `"paxeer-network:order_entry_sort_keys"` | Order entry column sort keys |

## Usage example

```typescript
import { ConnectorKey, ChainKey } from "@paxeer-network/types";
localStorage.setItem(ChainKey, String(chainId));
```
