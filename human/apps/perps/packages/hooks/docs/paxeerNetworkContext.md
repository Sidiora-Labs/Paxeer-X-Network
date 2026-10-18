# paxeerNetworkContext.ts — PaxeerNetworkProvider and PaxeerNetworkContext

## paxeerNetworkContext Responsibility

Provides `PaxeerNetworkProvider` and `PaxeerNetworkContext` that hold config store, key store, and related state (e.g. filtered chains, notification, data adapter). Consumed by hooks such as `useAccount`, `useConfig`, `useKeyStore`. Must be used inside (or as) the config provider tree.

## paxeerNetworkContext Exports

| Name | Type | Role |
|------|------|------|
| PaxeerNetworkProvider | FC | Provider component that supplies PaxeerNetworkContext value. |
| PaxeerNetworkContext | React.Context | Context with configStore, keyStore, and other config state. |
| FilteredChains, PaxeerNetworkConfigContextState | types | Context state and chain filter types. |

## PaxeerNetworkContext Value (typical)

- configStore, keyStore, and other fields from config context state.
- Used by: useAccount, useConfig, useKeyStore, useAccountInstance, etc.

## paxeerNetworkContext Example

```tsx
import { PaxeerNetworkProvider, PaxeerNetworkContext } from "@paxeer-network/hooks";

// PaxeerNetworkProvider is usually used via PaxeerNetworkConfigProvider (configProvider.tsx).
// To read context:
const { configStore, keyStore } = useContext(PaxeerNetworkContext);
```
