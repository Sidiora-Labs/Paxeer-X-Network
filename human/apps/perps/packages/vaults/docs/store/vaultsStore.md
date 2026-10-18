# vaultsStore

## Overview

Zustand store that holds vault API data (vault list, LP performance per vault, LP info per vault, overall info) and page config. All fetch methods require a base URL (or use the store’s `baseUrl`). Exposes selector hooks and action hooks for components.

## State

| Slice | Type | Description |
|-------|------|-------------|
| baseUrl | string | Default base URL for API calls |
| vaultInfo | { data, loading, error, lastUpdated, params } | Vault list and fetch state |
| vaultLpPerformance | { data: Record&lt;vault_id, VaultLpPerformance[]&gt;, ... } | LP performance by vault_id |
| vaultLpInfo | { data: Record&lt;vault_id, VaultLpInfo[]&gt;, ... } | LP info by vault_id |
| vaultOverallInfo | { data, loading, error, ... } | Overall stats |
| vaultsPageConfig | VaultsPageConfig \| null | Page config (e.g. overallInfoBrokerIds) |

## Actions

| Action | Description |
|--------|-------------|
| setBaseUrl(baseUrl) | Set default base URL |
| fetchVaultInfo(params?, baseUrl?) | Fetch vault list |
| refreshVaultInfo() | Re-fetch with last params |
| fetchVaultLpPerformance(params, baseUrl?) | Fetch LP performance for one vault |
| refreshVaultLpPerformance() | Re-fetch LP performance |
| fetchVaultLpInfo(params, baseUrl?) | Fetch LP info for one vault + wallet |
| refreshVaultLpInfo() | Re-fetch LP info |
| fetchVaultOverallInfo(params?, baseUrl?) | Fetch overall info |
| refreshVaultOverallInfo() | Re-fetch overall info |
| setVaultsPageConfig(config) | Set page config |

## Selector hooks

| Hook | Returns |
|-----|--------|
| useVaultInfoState() | vaultInfo slice |
| useVaultLpPerformanceState() | vaultLpPerformance slice |
| useVaultLpInfoState() | vaultLpInfo slice |
| useVaultOverallInfoState() | vaultOverallInfo slice |
| useVaultLpPerformanceById(vaultId) | VaultLpPerformance[] for vault |
| useVaultLpInfoById(vaultId) | VaultLpInfo[] for vault |
| useVaultLpPerformanceIds() | vault_ids with performance data |
| useVaultLpInfoIds() | vault_ids with LP info |
| useVaultLpPerformanceArray() | Flat array of all performance rows |
| useVaultLpInfoArray() | Flat array of all LP info rows |

## Action hooks

| Hook | Returns |
|------|--------|
| useVaultInfoActions() | { fetchVaultInfo, refreshVaultInfo } |
| useVaultLpPerformanceActions() | { fetchVaultLpPerformance, refreshVaultLpPerformance } |
| useVaultLpInfoActions() | { fetchVaultLpInfo, refreshVaultLpInfo } |
| useVaultOverallInfoActions() | { fetchVaultOverallInfo, refreshVaultOverallInfo } |

## Usage example

```typescript
import { useVaultsStore, useVaultInfoState, useVaultLpInfoById } from "./store";
const { data, loading } = useVaultInfoState();
const lpInfo = useVaultLpInfoById("vault-id");
const { fetchVaultInfo } = useVaultsStore();
await fetchVaultInfo(undefined, "https://api.paxeer.network");
```
