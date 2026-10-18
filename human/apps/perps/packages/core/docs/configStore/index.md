# configStore Directory Index

## Directory Description

`configStore` defines the configuration storage interface and default implementation: ConfigKey type, ConfigStore (get/set/getOr/clear), DefaultConfigStore backed by a Map, and API_URLS for mainnet/testnet (apiBaseUrl, publicWsUrl, privateWsUrl, operatorUrl).

## Directory Responsibility and Boundary

- **Responsible for**: Config key type, get/set/getOr/clear contract, default URL mapping by networkId.
- **Not responsible for**: Environment variable parsing, runtime config fetching, business logic.

## File List

| File | Language | Summary | Entry symbols | Link |
|------|----------|--------|---------------|------|
| configStore.ts | TS | ConfigStore interface and ConfigKey type | ConfigStore, ConfigKey | [configStore.md](./configStore.md) |
| defaultConfigStore.ts | TS | Map-based default impl and API_URLS | DefaultConfigStore, API_URLS, URLS | [defaultConfigStore.md](./defaultConfigStore.md) |

## Key Entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| ConfigStore | configStore.ts | Config read/write contract | None |
| ConfigKey | configStore.ts | Union of allowed config keys | None |
| DefaultConfigStore | defaultConfigStore.ts | Init URLs and broker from init; implements ConfigStore | ConfigStore, API_URLS |
| API_URLS | defaultConfigStore.ts | mainnet/testnet URL sets | None |

## Subdirectories

None.
