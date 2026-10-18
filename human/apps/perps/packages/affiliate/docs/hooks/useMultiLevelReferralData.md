# hooks/useMultiLevelReferralData.ts

## Responsibility of useMultiLevelReferralData.ts

Composes useVolumePrerequisite, useMaxRebateRate, useMultiLevelRebateInfo to expose: volumePrerequisite, maxRebateRate, multiLevelRebateInfo (with computed referee_rebate_rate and referrer_rebate_rate), isMultiLevelEnabled (true when max rebate API returns), isMultiLevelReferralUnlocked (current_volume >= required_volume), multiLevelRebateInfoMutate, isLoading. Used by ReferralProvider and any UI that needs multi-level state.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| useMultiLevelReferralData | function | Hook | Returns volumePrerequisite, maxRebateRate, multiLevelRebateInfo, isMultiLevelEnabled, isMultiLevelReferralUnlocked, multiLevelRebateInfoMutate, isLoading |
| MultiLevelReferralData | type | Return type | ReturnType<typeof useMultiLevelReferralData> |

## Dependencies

- react (useMemo)
- @paxeer-network/utils (Decimal)
- ./useReferralApi (useVolumePrerequisite, useMaxRebateRate, useMultiLevelRebateInfo)

## hooks/useMultiLevelReferralData.ts Example

```typescript
const {
  volumePrerequisite,
  isMultiLevelEnabled,
  isMultiLevelReferralUnlocked,
  multiLevelRebateInfoMutate,
} = useMultiLevelReferralData();
```
