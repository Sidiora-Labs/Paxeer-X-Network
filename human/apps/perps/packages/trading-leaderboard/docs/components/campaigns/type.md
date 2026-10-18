# type.ts (campaigns)

## Overview

Campaign and reward types: campaign config, prize pools, ticket rules, user data, and API response shapes.

## Exports

### Enums

- **`CampaignTagEnum`**: `ONGOING` \| `COMING` \| `ENDED` \| `EXCLUSIVE`

### Interfaces / Types

#### `CampaignStatistics`

| Field | Type | Description |
|-------|------|-------------|
| `total_participants` | `number?` | From campaign stats API |
| `total_volume` | `number?` | From campaign stats API |

#### `PrizePoolTier`

| Field | Type | Description |
|-------|------|-------------|
| `position` | `number?` | Rank position |
| `position_range` | `[number, number]?` | Rank range |
| `amount` | `number` | Prize amount |

#### `PrizePool`

| Field | Type | Description |
|-------|------|-------------|
| `pool_id` | `string` | Pool id |
| `label` | `string` | Label |
| `total_prize` | `number` | Total prize |
| `currency` | `string` | Currency |
| `metric` | `"volume" \| "pnl"` | Metric type |
| `tiers` | `PrizePoolTier[]` | Tiers |
| `volume_limit` | `number?` | Optional volume cap |

#### `TicketTierRule`, `TicketLinearRule`, `TicketRules`

Ticket-based reward rules (tiered or linear).

#### `CampaignConfig`

| Field | Type | Description |
|-------|------|-------------|
| `campaign_id` | `number \| string` | ID |
| `title` | `string` | Title |
| `subtitle` | `string?` | Subtitle |
| `description` | `string` | Description |
| `content` | `ReactNode?` | Custom content |
| `classNames` | `object?` | Container/title/description class names |
| `register_time`, `start_time`, `end_time` | `string?` | Dates |
| `reward_distribution_time` | `string?` | Reward distribution time |
| `volume_scope` | `string \| string[]?` | Symbol scope |
| `referral_codes` | `string[] \| string?` | Referral codes |
| `prize_pools` | `PrizePool[]?` | Prize pools |
| `tiered_prize_pools` | `PrizePool[][]?` | Tiered pools by volume |
| `ticket_rules` | `TicketRules?` | Ticket rules |
| `image`, `rule_url`, `rule_config` | various | Assets and rule config |
| `trading_url`, `trading_config`, `href` | various | Trading link and config |
| `highlight_pool_id`, `user_account_label` | various | UI hints |
| `rule` | `{ rule; terms; ruleConfig? }?` | Rule/terms (DescriptionItem[]) |

#### `UserData`

| Field | Type | Description |
|-------|------|-------------|
| `rank` | `number \| string?` | User rank |
| `pnl` | `number` | PnL |
| `total_participants` | `number?` | Total participants |
| `volume` | `number` | Volume |
| `referral_code` | `string?` | Referral code |

#### API response types

- **`CampaignStatsDetailsResponse`**: array of `{ broker_id, user_count, volume, symbol }`
- **`CampaignStatsResponse`**: `{ sign_up_count, user_count, volume, updated_time }`
- **`UserCampaignsResponse`**: array of `{ id, register_time }`
