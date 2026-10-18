# referral — Directory Index

## Directory Responsibility

Referral program hooks and API: commission, daily stats, distribution, referee history/info/rebate, referral code check/get, referral info component. Used by apps that integrate the referral program.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports referral module | useCommission, useDaily, useDistribution, useReferralRebateSummary, useRefereeHistory, useRefereeInfo, useRefereeRebateSummary, useCheckReferralCode, useGetReferralCode, useReferralInfo, CheckReferralCodeReturns, api | [index.md](index.md) |
| api.ts | TS | Referral API client | (api) | [api.md](api.md) |
| useCommission.ts | TS | Commission hook | useCommission | [useCommission.md](useCommission.md) |
| useDaily.ts | TS | Daily stats hook | useDaily | [useDaily.md](useDaily.md) |
| useDistribution.ts | TS | Distribution hook | useDistribution | [useDistribution.md](useDistribution.md) |
| useReferralRebateSummary.ts | TS | Referral rebate summary | useReferralRebateSummary | [useReferralRebateSummary.md](useReferralRebateSummary.md) |
| useRefereeHistory.ts | TS | Referee history hook | useRefereeHistory | [useRefereeHistory.md](useRefereeHistory.md) |
| useRefereeInfo.ts | TS | Referee info hook | useRefereeInfo | [useRefereeInfo.md](useRefereeInfo.md) |
| useRefereeRebateSummary.ts | TS | Referee rebate summary | useRefereeRebateSummary | [useRefereeRebateSummary.md](useRefereeRebateSummary.md) |
| useCheckReferralCode.ts | TS | Check referral code hook | useCheckReferralCode, CheckReferralCodeReturns | [useCheckReferralCode.md](useCheckReferralCode.md) |
| useGetReferralCode.ts | TS | Get referral code hook | useGetReferralCode | [useGetReferralCode.md](useGetReferralCode.md) |
| useReferralInfo.tsx | TSX | Referral info component | useReferralInfo | [useReferralInfo.md](useReferralInfo.md) |
| format.ts | TS | Formatting helpers | (format) | [format.md](format.md) |
| swr.ts | TS | SWR config for referral | (swr) | [swr.md](swr.md) |
