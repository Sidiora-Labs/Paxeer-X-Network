# defaultConfigStore.ts

## defaultConfigStore.ts Responsibility

Implements `ConfigStore` with a Map. Constructor accepts partial config (env, networkId, brokerId, brokerName, chainNamespace) and fills in URLs from `API_URLS[networkId]`. get(key) for object values (e.g. operatorUrl) may return value[chainNamespace]. Exports `API_URLS` and `URLS` type.

## defaultConfigStore.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| URLS | type | Shape | apiBaseUrl, publicWsUrl, privateWsUrl, operatorUrl (Record ChainNamespace → string) |
| API_URLS | constant | URLs | mainnet and testnet URL sets |
| DefaultConfigStore | class | Impl | Map-based ConfigStore |

## DefaultConfigStore Responsibility

Provides the default configuration for paxeer-network: mainnet/testnet API and WS URLs, operator URLs per chain namespace, and broker/env/networkId/chainNamespace. Used by Account and rest of core.

## DefaultConfigStore Constructor Input

| Key | Default | Description |
|-----|---------|-------------|
| env | "prod" | Environment |
| networkId | "mainnet" | mainnet \| testnet |
| brokerId | "paxeer-network" | Broker ID |
| brokerName | "paxeer-network" | Broker name |
| chainNamespace | ChainNamespace.evm | EVM \| SOL |

## DefaultConfigStore Methods

| Method | Description |
|--------|-------------|
| get<T>(key) | map.get(key); if value is object and not null, return value[get("chainNamespace")] for chain-specific keys |
| getOr<T>(key, defaultValue) | map.get(key) ?? defaultValue |
| set<T>(key, value) | map.set(key, value) |
| clear() | throw "Method not implemented." |

## API_URLS Shape

- mainnet: apiBaseUrl, publicWsUrl, privateWsUrl, operatorUrl (EVM, SOL).
- testnet: same keys, testnet hostnames.

## defaultConfigStore.ts Dependencies and Call Relationships

- **Upstream**: configStore (ConfigKey, ConfigStore), @paxeer-network/types (ChainNamespace, NetworkId).
- **Downstream**: Account and app bootstrap use DefaultConfigStore; core uses get("apiBaseUrl"), get("brokerId"), get("networkId"), get("env"), get("chainNamespace").

## defaultConfigStore.ts Example

```typescript
import { DefaultConfigStore, API_URLS } from "@paxeer-network/core";

const config = new DefaultConfigStore({ networkId: "testnet", brokerId: "my_broker" });
const apiBaseUrl = config.get<string>("apiBaseUrl");
const operatorUrl = config.get("operatorUrl");
```
