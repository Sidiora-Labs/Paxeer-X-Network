# @paxeer-network/types

## Overview

This package provides shared TypeScript types, enums, constants, and API/WebSocket interfaces for the paxeer-network Network SDK and applications. It covers orders, positions, chains, wallet/signing, tracking, storage keys, and REST/WS API response shapes.

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [types](./types/index.md) | REST API and WebSocket message interfaces (`API.*`, `WSMessage.*`) |

## Root-level files

| File | Language | Description |
|------|----------|-------------|
| [version](./version.md) | TypeScript | Package version string and optional `window.__PAXEER_NETWORK_VERSION__` registration |
| [constants](./constants.md) | TypeScript | Account/system/network enums, chain IDs, media breakpoints, token helpers, chain info presets |
| [order](./order.md) | TypeScript | Order types, sides, statuses, algo order types, entity interfaces (OrderEntity, AlgoOrderEntity, BracketOrder, etc.) |
| [context](./context.md) | TypeScript | Route and router adapter types for app integration |
| [withdraw](./withdraw.md) | TypeScript | Withdraw status enum |
| [chains](./chains.md) | TypeScript | Chain config interface, ChainInfo, NativeCurrency, chain presets, chainsInfoMap, testnet/mainnet lists |
| [track](./track.md) | TypeScript | Tracker event name enum for analytics |
| [wallet](./wallet.md) | TypeScript | CurrentChain type and WS wallet status enum |
| [storageKey](./storageKey.md) | TypeScript | LocalStorage key constants (Ledger, connector, chain, link device, etc.) |
| [errors](./errors.md) | TypeScript | ApiError and SDKError classes |
| [sign](./sign.md) | TypeScript | EIP-712 defined types (Registration, Withdraw, AddPaxeerNetworkKey, SettlePnl, etc.) |
| [assetHistory](./assetHistory.md) | TypeScript | Asset history status and side enums |
| [account](./account.md) | TypeScript | Default paxeer-network key scope constant |
| [web3provider](./web3provider.md) | TypeScript | Web3Provider interface (signTypedData, call, sendTransaction, callOnChain, getBalance) |
| [types/api](./types/api.md) | TypeScript | API and WSMessage namespaces: tokens, markets, orders, positions, funding, referrals, etc. |

The package entry point `src/index.ts` re-exports the above modules (version, constants, types/api, order, context, withdraw, chains, track, wallet, storageKey, errors, sign, assetHistory, account) plus `superstruct`.
