# types.ts

## Responsibility of types.ts

Defines shared enums for statistics time range and referral code form (create/edit/reset and field names). Used by affiliate and multi-level referral UI and API hooks.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| StatisticsTimeRange | enum | Filter | Time range for statistics (1d, 7d, 30d, All) |
| ReferralCodeFormType | enum | Form mode | Create, Edit, or Reset |
| ReferralCodeFormField | enum | Form field key | referralCode, rebateRate |

## StatisticsTimeRange Values

| Value | String | Description |
|-------|--------|-------------|
| "1d" | "1d" | 1 day |
| "7d" | "7d" | 7 days |
| "30d" | "30d" | 30 days |
| "All" | "all_time" | All time |

## ReferralCodeFormType Values

| Value | String | Description |
|-------|--------|-------------|
| Create | "create" | Create new referral code |
| Edit | "edit" | Edit existing code |
| Reset | "reset" | Reset rebate rate |

## ReferralCodeFormField Values

| Value | String | Description |
|-------|--------|-------------|
| ReferralCode | "referralCode" | Referral code field |
| RebateRate | "rebateRate" | Rebate rate field |

## Dependencies

- None (no imports).

## types.ts Example

```typescript
import { StatisticsTimeRange, ReferralCodeFormType, ReferralCodeFormField } from "@paxeer-network/affiliate";

const timeRange = StatisticsTimeRange["7d"]; // "7d"
const formType = ReferralCodeFormType.Edit;   // "edit"
const field = ReferralCodeFormField.RebateRate; // "rebateRate"
```
