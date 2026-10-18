# @paxeer-network/hooks — Documentation Index

## Module Overview

The `packages/hooks/src` directory contains the **@paxeer-network/hooks** package: React hooks, providers, and services for paxeer-network Network trading and account flows. It provides data fetching (SWR-based), WebSocket streams, order creation/merge logic, config/wallet wiring, referral and trading-rewards hooks, and shared utilities.

## Module Responsibilities

| Responsibility | Description |
|----------------|-------------|
| **Data & queries** | `useQuery`, `usePrivateQuery`, `useInfiniteQuery`, `useLazyQuery`, `useMutation`, fetcher, SWR middleware |
| **Account & wallet** | `useAccount`, `useAccountInstance`, wallet connector context, key store, config provider |
| **paxeer-network domain** | Order book, positions, orders, markets, funding, leverage, deposit/withdraw, TPSL (paxeer-network/) |
| **Order creation** | Order creator services, validators, builders, merge handlers (services/orderCreator, services/orderMerge) |
| **Next / order entry** | `useOrderEntry`, order store, position close, TPSL helpers (next/) |
| **Provider & config** | PaxeerNetworkConfigProvider, status/dataCenter/store providers, extended config store |
| **Referral & rewards** | Referral hooks, trading-rewards hooks (referral/, trading-rewards/) |
| **Utilities** | Utils, middleware (IndexedDB, timestamp, signature), painter service, feature flags, API keys |

## Key Entities and Entry Points

| Entity | Type | Role | Entry / Location |
|--------|------|------|------------------|
| `version` | constant | Package version | `src/version.ts` |
| `useAccount` | hook | Account state, create key, switch account | `src/useAccount.ts` |
| `useAccountInstance` | hook | Raw account instance from context | `src/useAccountInstance.ts` |
| `useQuery` / `usePrivateQuery` / `useInfiniteQuery` / `useMutation` | hooks | Data fetching with SWR | `src/useQuery.ts`, `usePrivateQuery.ts`, etc. |
| `PaxeerNetworkConfigProvider` | component | Config, wallet adapters, chain filter | `src/configProvider.tsx` |
| `WalletConnectorContext` / `useWalletConnector` | context/hook | Wallet connection state | `src/walletConnectorContext.tsx` |
| `PaxeerNetworkProvider` / `PaxeerNetworkContext` | context | Core paxeer-network context | `src/paxeerNetworkContext.ts` |
| `useOrderEntry` | hook | Order entry (next) | `src/next/useOrderEntry/useOrderEntry.ts` |
| Order creator factory & creators | service | Build and submit orders | `src/services/orderCreator/` |
| `paxeerNetworkHooks` | re-exports | All paxeer-network domain hooks | `src/paxeer-network/paxeerNetworkHooks.ts` |
| `useApiKeyManager` | hook | API key management | `src/apiKeys/useApiKeyManager.ts` |
| `useReferralInfo` / referral hooks | hooks | Referral data | `src/referral/` |
| Trading rewards hooks | hooks | Epoch, rewards history, status | `src/trading-rewards/` |

## Package Entry (index.ts)

The main entry `src/index.ts` re-exports the public API: version, data hooks (useQuery, usePrivateQuery, useInfiniteQuery, useMutation, etc.), account/wallet hooks, PaxeerNetworkConfigProvider, paxeer-network context, middleware, paxeer-network domain hooks, referral, trading-rewards, API keys, next order entry, sub-account, feature flag, and SWR/debounce utilities. Consumers import from `@paxeer-network/hooks` (e.g. `import { useAccount, useOrderEntry } from "@paxeer-network/hooks"`). See [useAccount](useAccount.md), [configProvider](configProvider.md), [paxeerNetworkContext](paxeerNetworkContext.md) for core setup.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [apiKeys](apiKeys/index.md) | API key management hooks | [apiKeys/index.md](apiKeys/index.md) |
| [deprecated](deprecated/index.md) | Deprecated order entry | [deprecated/index.md](deprecated/index.md) |
| [dev](dev/index.md) | Dev-only stores (e.g. proxy config) | [dev/index.md](dev/index.md) |
| [feature-flag](feature-flag/index.md) | Feature flags and keys | [feature-flag/index.md](feature-flag/index.md) |
| [middleware](middleware/index.md) | SWR/IndexedDB/timestamp middleware | [middleware/index.md](middleware/index.md) |
| [next](next/index.md) | Next-order-entry, positions, TPSL, order book | [next/index.md](next/index.md) |
| [paxeer-network](paxeer-network/index.md) | paxeer-network hooks (markets, positions, funding, etc.) | [paxeer-network/index.md](paxeer-network/index.md) |
| [provider](provider/index.md) | Status, dataCenter, store providers | [provider/index.md](provider/index.md) |
| [referral](referral/index.md) | Referral hooks and API | [referral/index.md](referral/index.md) |
| [services](services/index.md) | Order creator, merge, painter, dataCenter | [services/index.md](services/index.md) |
| [shared](shared/index.md) | Shared hooks (useMemoizedFn, useAudio, etc.) | [shared/index.md](shared/index.md) |
| [subAccount](subAccount/index.md) | Sub-account queries, mutations, WS | [subAccount/index.md](subAccount/index.md) |
| [trading-rewards](trading-rewards/index.md) | Trading rewards hooks and types | [trading-rewards/index.md](trading-rewards/index.md) |
| [utils](utils/index.md) | Fetcher, JSON, order helpers, WS, etc. | [utils/index.md](utils/index.md) |

## Top-Level Files (root of src)

| File | Language | Responsibility | Entry symbol(s) | Link |
|------|----------|----------------|-----------------|------|
| index.ts | TS | Package re-exports | (all public API) | (this index) |
| version.ts | TS | Package version | version | [version.md](version.md) |
| constants.ts | TS | Default tick sizes, depths | constants | [constants.md](constants.md) |
| types.ts | TS | Shared types | types | [types.md](types.md) |
| configProvider.tsx | TSX | PaxeerNetworkConfigProvider, config props | PaxeerNetworkConfigProvider, ConfigProviderProps | [configProvider.md](configProvider.md) |
| extendedConfigStore.ts | TS | Extended config store | ExtendedConfigStore | [extendedConfigStore.md](extendedConfigStore.md) |
| paxeerNetworkContext.ts | TS | PaxeerNetworkProvider, PaxeerNetworkContext | PaxeerNetworkProvider, PaxeerNetworkContext | [paxeerNetworkContext.md](paxeerNetworkContext.md) |
| walletConnectorContext.tsx | TSX | Wallet connector context/hook | WalletConnectorContext, useWalletConnector | [walletConnectorContext.md](walletConnectorContext.md) |
| useAccount.ts | TS | Account hook | useAccount | [useAccount.md](useAccount.md) |
| useAccountInstance.ts | TS | Account instance hook | useAccountInstance | [useAccountInstance.md](useAccountInstance.md) |
| useQuery.ts | TS | SWR-based query hook | useQuery | [useQuery.md](useQuery.md) |
| useLazyQuery.ts | TS | Lazy query hook | useLazyQuery | [useLazyQuery.md](useLazyQuery.md) |
| useMutation.ts | TS | Mutation hook | useMutation | [useMutation.md](useMutation.md) |
| usePrivateQuery.ts | TS | Private (auth) query hook | usePrivateQuery | [usePrivateQuery.md](usePrivateQuery.md) |
| usePrivateInfiniteQuery.ts | TS | Private infinite query hook | usePrivateInfiniteQuery | [usePrivateInfiniteQuery.md](usePrivateInfiniteQuery.md) |
| useInfiniteQuery.ts | TS | Infinite query hook | useInfiniteQuery | [useInfiniteQuery.md](useInfiniteQuery.md) |
| useBoolean.ts | TS | Boolean state hook | useBoolean | [useBoolean.md](useBoolean.md) |
| useConfig.ts | TS | Config hook | useConfig | [useConfig.md](useConfig.md) |
| useKeyStore.ts | TS | Key store hook | useKeyStore | [useKeyStore.md](useKeyStore.md) |
| useSimpleDI.ts | TS | Simple DI hook | useSimpleDI | [useSimpleDI.md](useSimpleDI.md) |
| useWS.ts | TS | WebSocket hook | useWS | [useWS.md](useWS.md) |
| useWsStatus.ts | TS | WS status hook | useWsStatus, WsNetworkStatus | [useWsStatus.md](useWsStatus.md) |
| useSessionStorage.ts | TS | Session storage hook | useSessionStorage | [useSessionStorage.md](useSessionStorage.md) |
| useLocalStorage.ts | TS | Local storage hook | useLocalStorage | [useLocalStorage.md](useLocalStorage.md) |
| useNetworkInfo.ts | TS | Network info hook | useNetworkInfo | [useNetworkInfo.md](useNetworkInfo.md) |
| useFeeState.ts | TS | Fee state hook | useFeeState | [useFeeState.md](useFeeState.md) |
| useTrack.ts | TS | Tracking hook | useTrack | [useTrack.md](useTrack.md) |
| useTrackingInstance.ts | TS | Tracking instance hook | useTrackingInstance | [useTrackInstance.md](useTrackInstance.md) |
| useEventEmitter.ts | TS | Event emitter hook | useEventEmitter | [useEventEmitter.md](useEventEmitter.md) |
| usePreloadData.ts | TS | Preload data hook | usePreLoadData | [usePreloadData.md](usePreloadData.md) |
| useMediaQuery.ts | TS | Media query hook | useMediaQuery | [useMediaQuery.md](useMediaQuery.md) |
| usePoster.ts | TS | Poster hook | usePoster | [usePoster.md](usePoster.md) |
| useRestrictedInfo.ts | TS | Restricted info hook | useRestrictedInfo | [useRestrictedInfo.md](useRestrictedInfo.md) |
| useCalculatorService.ts | TS | Calculator service hook | useCalculatorService | [useCalculatorService.md](useCalculatorService.md) |
| useDatabaseInitialization.ts | TS | DB init hook | (internal) | [useDatabaseInitialization.md](useDatabaseInitialization.md) |
| useObserve.ts | TS | Observe hook | useObserve | [useObserve.md](useObserve.md) |
| useParamsCheck.ts | TS | Params check hook | (internal) | [useParamsCheck.md](useParamsCheck.md) |
| useTimestampAwareQuery.ts | TS | Timestamp-aware query | (internal) | [useTimestampAwareQuery.md](useTimestampAwareQuery.md) |

## Search Keywords / Aliases

- hooks, React hooks, SWR, useQuery, usePrivateQuery, useAccount, order entry
- paxeer-network, config provider, wallet connector, key store
- order book, positions, orders, markets, funding, leverage
- order creator, order validation, merge handler
- referral, trading rewards, API key, feature flag
- middleware, IndexedDB, timestamp, fetcher
