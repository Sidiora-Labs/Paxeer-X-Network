# chainSelector.script.ts

## Overview

Implements the chain selector logic: hook `useChainSelectorScript` and internal helpers for chain data, recent chains, and tab state. Uses `@paxeer-network/hooks` (config, storage chain, chains, wallet connector), `@paxeer-network/types` (`NetworkId`, `WalletChainChangeState`), and `@paxeer-network/react-app` (onChainChanged, currentChainId, wrongNetwork).

## Exports

### UseChainSelectorScriptReturn

Return type of `useChainSelectorScript`. Contains the state and handlers passed to the UI component.

### UseChainSelectorScriptOptions

Options for `useChainSelectorScript`.

| Property              | Type                                                       | Required | Description                                                                             |
| --------------------- | ---------------------------------------------------------- | -------- | --------------------------------------------------------------------------------------- |
| `networkId`           | `NetworkId`                                                | No       | Network context.                                                                        |
| `bridgeLessOnly`      | `boolean`                                                  | No       | If true, only show bridgeless mainnet chains.                                           |
| `close`               | `() => void`                                               | No       | Callback to close dialog/sheet.                                                         |
| `resolve`             | `(result: ChainSelectorResult) => void`                    | No       | Resolve the selected chain and network state.                                           |
| `reject`              | `(reason?: unknown) => void`                               | No       | Reject promise.                                                                         |
| `onChainChangeBefore` | `(chainId: number, state: { isTestnet: boolean }) => void` | No       | Called before switching chain.                                                          |
| `onChainChangeAfter`  | `(chainId: number, state: WalletChainChangeState) => void` | No       | Called after switch attempt, including whether a fallback wallet connection is pending. |

### useChainSelectorScript(options)

Hook that:

- Builds `chains` (mainnet/testnet) from config, optionally filtered by `bridgeLessOnly`.
- Derives `showTestnet` from theme overrides and testnet list length.
- Manages `selectChainId`, recent chains (local storage, max 6), and mainnet/testnet tab.
- Exposes `onChainClick`: switches chain via wallet or storage, settles the selector, records the selected chain, and reports success, failure, or pending fallback state through `onChainChangeAfter`/`onChainChanged`.

**Returns**: Object with `recentChains`, `chains`, `selectChainId`, `onChainClick`, `selectedTab`, `onTabChange`, `showTestnet` for use by `ChainSelector` UI.

## Internal helpers

- **useChainTab** – Manages `selectedTab` (Mainnet/Testnet) and syncs with `currentChainId` and `wrongNetwork`.
- **useRecentChains** – Loads/saves recent chain IDs from local storage (`paxeer-network_selected_chains`), only mainnet; max 6 items.

## Usage example

```typescript
import {
  useChainSelectorScript,
  UseChainSelectorScriptOptions,
} from "./chainSelector.script";

const options: UseChainSelectorScriptOptions = {
  bridgeLessOnly: true,
  close: () => {},
  onChainChangeAfter: (
    chainId,
    { isTestnet, isWalletConnected, isWalletConnectionPending },
  ) => {
    console.log(
      "Switched to",
      chainId,
      isTestnet,
      isWalletConnected,
      isWalletConnectionPending,
    );
  },
};

const state = useChainSelectorScript(options);
// state.recentChains, state.chains, state.onChainClick, state.selectedTab, state.onTabChange, state.showTestnet
```
