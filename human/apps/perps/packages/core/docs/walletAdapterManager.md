# walletAdapterManager.ts

## walletAdapterManager.ts Responsibility

Manages the current active wallet adapter by chain namespace. `WalletAdapterManager` holds a list of `WalletAdapter` instances and exposes `switchWallet(chainNamespace, address, chainId, options)` to set the active adapter (and deactivate the previous one). Provides `adapter`, `chainId`, `isAdapterExist`, `isEmpty`.

## walletAdapterManager.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| WalletAdapterManager | class | Manager | Switch and hold current WalletAdapter by chainNamespace |

## WalletAdapterManager Responsibility

Single point to switch between EVM and Solana (or other) adapters when the user changes wallet or chain. Account uses it after setAddress to activate the correct adapter with provider and contractManager.

## WalletAdapterManager Input and Output

- **Input**: Constructor(walletAdapters: WalletAdapter[]). switchWallet(chainNamespace, address, chainId, { provider?, contractManager? }).
- **Output**: adapter getter (current WalletAdapter or undefined), chainId getter, isAdapterExist (boolean), isEmpty (boolean). No events.

## WalletAdapterManager Execution Flow

1. Constructor: store walletAdapters; throw if length 0.
2. switchWallet: find adapter where adapter.chainNamespace === chainNamespace.toUpperCase(); throw "Unsupported chain namespace" if not found. If current _adapter exists and equals found adapter, call _adapter.update(config); else call _adapter.deactivate(), then set _adapter = adapter and adapter.active(config).

## walletAdapterManager.ts Dependencies and Call Relationships

- **Upstream**: wallet/walletAdapter (WalletAdapter), contract (IContract), ethers (Eip1193Provider), @paxeer-network/types (ChainNamespace).
- **Downstream**: Account constructs it with wallet adapters and calls switchWallet from setAddress; Account exposes walletAdapter and chainId via this manager.

## walletAdapterManager.ts Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| No adapters | Constructor with [] | throw "No wallet adapters provided" | Pass at least one adapter |
| Unsupported chain | switchWallet with no matching chainNamespace | throw "Unsupported chain namespace" | Register adapter for that chain |

## walletAdapterManager.ts Example

```typescript
import { WalletAdapterManager } from "@paxeer-network/core";
import { EvmWalletAdapter } from "./wallet/evmAdapter";
import { SolanaWalletAdapter } from "./wallet/solanaAdapter";

const manager = new WalletAdapterManager([evmAdapter, solanaAdapter]);
manager.switchWallet("EVM", "0x...", 421614, { provider, contractManager });
const adapter = manager.adapter;
const chainId = manager.chainId;
```
