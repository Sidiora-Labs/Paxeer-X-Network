# api (api.ts)

## Overview

Vault strategy API functions: fetch vault list, vault LP performance, LP info, and overall vault statistics. Uses the shared request client with a configurable base URL.

## Exports

### Functions

| Function | Description |
|----------|-------------|
| `getVaultInfo(baseUrl, params?)` | GET vault list; optional `vault_id`, `status`, `broker_ids` |
| `getVaultLpPerformance(baseUrl, params)` | GET LP performance for a vault; `vault_id`, optional `time_range` |
| `getVaultLpInfo(baseUrl, params)` | GET LP info for a vault and wallet; `vault_id`, `wallet_address` |
| `getVaultOverallInfo(baseUrl, params?)` | GET overall stats; optional `broker_ids` |

### Parameter types

| Type | Description |
|------|-------------|
| `VaultInfoParams` | `vault_id?`, `status?`, `broker_ids?` |
| `VaultPerformanceParams` | `vault_id`, `time_range?` (VaultTimeRange) |
| `VaultLpInfoParams` | `vault_id`, `wallet_address` |
| `VaultOverallInfoParams` | `broker_ids?` |

### Response types

| Type | Description |
|------|-------------|
| `VaultInfoResponse` | `{ rows: VaultInfo[] }` |
| `VaultLpPerformanceResponse` | `{ rows: VaultLpPerformance[] }` |
| `VaultLpInfoResponse` | `{ rows: VaultLpInfo[] }` |
| `VaultOverallInfoResponse` | Overall stats (tvl, lifetime_net_pnl, count, lp_count) |

## Usage example

```typescript
import { getVaultInfo, getVaultOverallInfo } from "./api";
const baseUrl = "https://api.paxeer.network";
const vaults = await getVaultInfo(baseUrl, { status: "live" });
const overall = await getVaultOverallInfo(baseUrl, { broker_ids: "paxeer-network,woofi_pro" });
```
