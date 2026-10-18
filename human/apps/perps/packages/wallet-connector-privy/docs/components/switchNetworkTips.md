# SwitchNetworkTips / StorageChainNotCurrentWalletType

## Overview

**SwitchNetworkTips** shows a warning bar with optional “switch network” action that opens the chain selector modal/sheet. **StorageChainNotCurrentWalletType** (from the same file) shows a tip when the current storage chain does not match the connected wallet type and suggests switching.

## Exports

### SwitchNetworkTips

| Prop | Type | Description |
|------|------|-------------|
| tipsContent | `string \| null` | Chain name or message; if null, component renders nothing. |

### StorageChainNotCurrentWalletType

| Prop | Type | Description |
|------|------|-------------|
| currentWalletType | `Set<WalletType>` | Set of currently connected wallet types. |

## Usage example

```tsx
<SwitchNetworkTips tipsContent={chainName} />
<StorageChainNotCurrentWalletType currentWalletType={currentConnectWalletType} />
```
