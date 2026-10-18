# configStore.ts

## configStore.ts Responsibility

Defines the configuration key set (`ConfigKey`) and the configuration store contract (`ConfigStore`): get, getOr, set, clear. No implementation; implementations (e.g. DefaultConfigStore) provide storage.

## configStore.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| ConfigKey | type | Keys | Union of allowed key strings |
| ConfigStore | interface | Contract | get, getOr, set, clear |

## ConfigKey Values

| Key | Typical use |
|-----|-------------|
| apiBaseUrl | paxeer-network REST API base URL |
| klineDataUrl | Kline data URL |
| privateWsUrl | Private WebSocket URL |
| publicWsUrl | Public WebSocket URL |
| operatorUrl | Operator URL (per chain) |
| domain | Domain |
| brokerId | Broker ID |
| brokerName | Broker display name |
| networkId | mainnet / testnet |
| env | prod / qa / dev |
| chainNamespace | EVM / SOL |
| PROD_URL, paxeer-network_markets, markets | Other config |

## ConfigStore Methods

| Method | Description |
|--------|-------------|
| get<T>(key) | Return value for key; type T |
| getOr<T>(key, defaultValue) | Return value or defaultValue |
| set<T>(key, value) | Set key to value |
| clear() | Clear all config |

## configStore.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: defaultConfigStore.ts implements ConfigStore; Account, Assets, contract, wallet code use ConfigStore for apiBaseUrl, brokerId, networkId, env, chainNamespace.

## configStore.ts Example

```typescript
import type { ConfigStore, ConfigKey } from "@paxeer-network/core";

function useConfig(store: ConfigStore) {
  const apiBaseUrl = store.get<string>("apiBaseUrl");
  const brokerId = store.getOr<string>("brokerId", "paxeer-network");
  store.set("chainNamespace", "EVM");
}
```
