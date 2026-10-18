# hooks/useReferralApi.ts

## Responsibility of useReferralApi.ts

Exposes four private query hooks for multi-level referral: volume prerequisite, max rebate rate, rebate info, and statistics (with time_range). Used by useMultiLevelReferralData and by pages that need statistics.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| useVolumePrerequisite | function | Query | GET /v1/referral/multi_level/volume_prerequisite |
| useMaxRebateRate | function | Query | GET /v1/referral/multi_level/max_rebate_rate |
| useMultiLevelRebateInfo | function | Query | GET /v1/referral/multi_level/rebate_info |
| useMultiLevelStatistics | function | Query | GET /v1/referral/multi_level/statistics?time_range= |

## useMultiLevelStatistics Parameter

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| time_range | StatisticsTimeRange | Yes | 1d, 7d, 30d, all_time |

## Dependencies

- @paxeer-network/hooks (usePrivateQuery)
- @paxeer-network/types (API.Referral.*)
- ../types (StatisticsTimeRange)

## hooks/useReferralApi.ts Example

```typescript
import { useVolumePrerequisite, useMultiLevelStatistics } from "../hooks/useReferralApi";
import { StatisticsTimeRange } from "../types";

const { data: vol } = useVolumePrerequisite();
const { data: stats } = useMultiLevelStatistics(StatisticsTimeRange["7d"]);
```
