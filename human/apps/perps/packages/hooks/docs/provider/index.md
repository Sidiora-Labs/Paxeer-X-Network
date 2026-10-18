# provider — Directory Index

## Directory Responsibility

React providers and stores: status provider, data center provider, and store (chains, tokens, symbol, swap support). Used to supply app-wide status, data center, and chain/token config.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [dataCenter](dataCenter/index.md) | Data center context and provider | [dataCenter/index.md](dataCenter/index.md) |
| [status](status/index.md) | Status context and provider | [status/index.md](status/index.md) |
| [store](store/index.md) | Chain/token/symbol/swap stores | [store/index.md](store/index.md) |

## Key Entities

| Entity | Location | Responsibility |
|--------|----------|----------------|
| StatusProvider | provider/status | Status context provider |
| DataCenterProvider | provider/dataCenter | Data center context provider |
| useMainnetChainsStore / useTestnetChainsStore | provider/store | Chain config stores |
| useMainTokenStore / useTestTokenStore | provider/store | Token config stores |
| useSwapSupportStore | provider/store | Swap support store |
