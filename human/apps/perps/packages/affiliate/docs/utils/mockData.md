# utils/mockData.ts

## Responsibility of utils/mockData.ts

Static mock data for referral info (referrer_info, referral_codes, totals) used in development or testing when the real referral API is unavailable.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MockData | class | Static data | MockData.referralInfo holds a full referral info–shaped object |

## MockData.referralInfo Shape

- referrer_info: total_invites, total_traded, total_referee_volume, referral_codes[]
- referral_codes: code, max_rebate_rate, referrer_rebate_rate, referee_rebate_rate, total_invites, total_traded

Not inferable from code: exact usage sites; assume dev/test only.

## utils/mockData.ts Example

```typescript
import { MockData } from "./mockData";

const mock = MockData.referralInfo;
```
