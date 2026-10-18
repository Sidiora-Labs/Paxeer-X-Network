# hooks

## Directory Responsibility

Hooks for referral and multi-level referral: API queries (volume prerequisite, max rebate rate, rebate info, statistics), mutations (create/edit referral code, update/reset rebate rate), referral history with pagination, multi-level referral codes list and referees list. Used by provider and page components.

## Files

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| useReferralApi.ts | TypeScript | useVolumePrerequisite, useMaxRebateRate, useMultiLevelRebateInfo, useMultiLevelStatistics | useVolumePrerequisite, useMaxRebateRate, useMultiLevelRebateInfo, useMultiLevelStatistics | [useReferralApi.md](useReferralApi.md) |
| useReferralCode.ts | TypeScript | create/edit referral code, update/reset rebate rate mutations | useReferralCode | [useReferralCode.md](useReferralCode.md) |
| useReferralHistory.ts | TypeScript | Paginated referral history with size, startDate, endDate, page | useReferralHistory | [useReferralHistory.md](useReferralHistory.md) |
| useMultiLevelReferralCodes.ts | TypeScript | Codes list (multi + single) and copyCode | useMultiLevelReferralCodes, ReferralCodesRow | [useMultiLevelReferralCodes.md](useMultiLevelReferralCodes.md) |
| useMultiLevelReferralData.ts | TypeScript | Volume prerequisite, max rebate, rebate info, isMultiLevelEnabled, isMultiLevelReferralUnlocked | useMultiLevelReferralData, MultiLevelReferralData | [useMultiLevelReferralData.md](useMultiLevelReferralData.md) |
| useMultiLevelReferees.ts | TypeScript | Paginated referee list for multi-level | useMultiLevelReferees, RefereeDataType, RefereePaginationMeta | [useMultiLevelReferees.md](useMultiLevelReferees.md) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| useReferralApi | useReferralApi.ts | Four query hooks for multi-level API |
| useReferralCode | useReferralCode.ts | Mutations for referral code and rebate rate |
| useReferralHistory | useReferralHistory.ts | Referral history rows and pagination meta |
| useMultiLevelReferralData | useMultiLevelReferralData.ts | Aggregated multi-level state for provider |
| useMultiLevelReferees | useMultiLevelReferees.ts | Referee list with pagination |
