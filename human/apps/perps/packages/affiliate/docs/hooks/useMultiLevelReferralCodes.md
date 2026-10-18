# hooks/useMultiLevelReferralCodes.ts

## Responsibility of useMultiLevelReferralCodes.ts

Builds a unified list of referral codes: one multi-level code (from useMultiLevelRebateInfo) plus legacy single-level codes from referralInfo, with totals from useRefereeInfo. Exposes copyCode (copies to clipboard via copyText). Used by referral codes table UI.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| ReferralCodesRow | type | Row shape | code, direct_invites, indirect_invites, direct_volume, indirect_volume, direct_rebate, indirect_rebate, direct_bonus_rebate?, max_rebate_rate, referee_rebate_rate, referrer_rebate_rate, total_*, referral_type |
| UseMultiLevelReferralCodesReturns | type | Return type | codes?, copyCode |
| useMultiLevelReferralCodes | function | Hook | Returns { codes, copyCode } |

## Dependencies

- react (useMemo)
- @paxeer-network/hooks (useRefereeInfo)
- ../provider (useReferralContext)
- ../utils/utils (copyText)
- ./useReferralApi (useMultiLevelRebateInfo)

## hooks/useMultiLevelReferralCodes.ts Example

```typescript
const { codes, copyCode } = useMultiLevelReferralCodes();
copyCode(codes?.[0].code ?? "");
```
