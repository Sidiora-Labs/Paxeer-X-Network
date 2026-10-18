# hooks/useReferralCode.ts

## Responsibility of useReferralCode.ts

Provides mutations for multi-level referral: claim code, edit referral code, update rebate rate, set default (reset) rebate rate. Returns async functions and a single isMutating flag.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| useReferralCode | function | Hook | Returns createReferralCode, editReferralCode, updateRebateRate, resetRebateRate, isMutating |

## createReferralCode Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| params.referee_rebate_rate | number | Rebate rate for referee |

## editReferralCode Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| params.current_referral_code | string | Existing code |
| params.new_referral_code | string | New code |

## updateRebateRate Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| params.referee_rebate_rate | number | New rate |
| params.account_ids | string[] | Optional account filter |

## resetRebateRate Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| params.account_ids | string[] | Accounts to reset |

## Dependencies

- @paxeer-network/hooks (useMutation)

## hooks/useReferralCode.ts Example

```typescript
const { createReferralCode, updateRebateRate, isMutating } = useReferralCode();
await createReferralCode({ referee_rebate_rate: 0.2 });
await updateRebateRate({ referee_rebate_rate: 0.25, account_ids: ["id1"] });
```
