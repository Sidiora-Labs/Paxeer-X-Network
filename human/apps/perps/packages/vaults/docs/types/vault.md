# vault

## Overview

Core vault domain types: vault metadata, LP and performance data, operation types, and page configuration.

## Type list

### VaultSupportedChain

| Field | Type | Description |
|-------|------|-------------|
| chain_id | string | Chain identifier |
| chain_name | string | Display name |

### VaultInfo

| Field | Type | Description |
|-------|------|-------------|
| vault_id | string | Unique vault id |
| vault_address | string | Contract address |
| vault_type | "protocol" \| "community" \| "user" | Vault type |
| vault_name | string | Display name |
| description | string | Description text |
| sp_address | string | Strategy provider address |
| sp_name | string \| null | SP name |
| asset | string | Asset symbol (e.g. USDC) |
| vault_age | number \| null | Days since launch |
| status | VaultStatus | pre_launch, live, closing, closed |
| vault_start_time | number | Start timestamp |
| performance_fee_rate | number | Fee rate |
| supported_chains | VaultSupportedChain[] | Supported chains |
| tvl | number | Total value locked |
| valid_hpr | number | — |
| 30d_apy | number | 30d APY |
| recovery_30d_apy | number | — |
| lifetime_apy | number | Lifetime APY |
| vault_lifetime_net_pnl | number | Lifetime net PnL |
| lp_counts | number | LP count |
| total_main_shares | number | Total shares |
| est_main_share_price | number | Share price |
| lock_duration | number | Lock duration |
| min_deposit_amount | number | Min deposit |
| min_withdrawal_amount | number | Min withdrawal |
| gate_threshold_pct | number | Gate threshold |
| gate_triggered | boolean | Gate triggered |
| broker_id | string | Broker id |

### VaultLpPerformance

| Field | Type | Description |
|-------|------|-------------|
| time_range | VaultTimeRange | 24h, 7d, 30d, all_time |
| tvl_max_drawdown | number | TVL max drawdown |
| incremental_net_pnl | number | Net PnL in range |
| pnl_max_drawdown | number | PnL max drawdown |

### VaultLpInfo

| Field | Type | Description |
|-------|------|-------------|
| vault_id | string | Vault id |
| lp_nav | number | LP NAV |
| lp_tvl | number | LP TVL |
| total_main_shares | number | Total shares |
| available_main_shares | number | Available shares |
| potential_pnl | number | Potential PnL |

### VaultOverallInfo

| Field | Type | Description |
|-------|------|-------------|
| strategy_vaults_tvl | number | Total TVL |
| strategy_vaults_lifetime_net_pnl | number | Total lifetime net PnL |
| strategy_vaults_count | number | Vault count |
| strategy_vaults_lp_count | number | Total LP count |

### VaultOperation

| Field | Type | Description |
|-------|------|-------------|
| type | OperationType | deposit \| withdrawal |
| vault_id | string | Vault id |
| amount_change | number | Amount change |
| created_time | string | Created time |
| status | string | Operation status |

### VaultsPageConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| headerImage | React.ReactNode | No | Custom header image |
| overallInfoBrokerIds | string | No | broker_ids for overall info API (default: "paxeer-network,{current_broker_id}") |

### Enums / type aliases

- **VaultTimeRange**: `"24h" | "7d" | "30d" | "all_time"`
- **VaultStatus**: `"pre_launch" | "live" | "closing" | "closed"`
- **RoleType**: `LP = "lp"`, `SP = "sp"`
- **OperationType**: `DEPOSIT = "deposit"`, `WITHDRAWAL = "withdrawal"`

## Usage example

```typescript
import type { VaultInfo, VaultStatus, VaultsPageConfig } from "./vault";
const vault: VaultInfo = { ... };
const config: VaultsPageConfig = { overallInfoBrokerIds: "paxeer-network,woofi_pro" };
```
