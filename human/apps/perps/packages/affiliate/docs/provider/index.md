# provider

## Directory Responsibility

Referral context and provider: `ReferralContext` (type and `useReferralContext`) and `ReferralProvider` (fetches referral info, volume, multi-level data, and exposes them to the tree). All affiliate/trader UI depends on this context.

## Files

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| context.ts | TypeScript | Context type, props, returns, TabTypes, Overwrite, ChartConfig | ReferralContext, useReferralContext, ReferralContextProps, ReferralContextReturns, TabTypes, UserVolumeType, BuildNode, Overwrite, ChartConfig | [context.md](context.md) |
| provider.tsx | TSX | ReferralProvider implementation | ReferralProvider | [provider.md](provider.md) |
| index.ts | TypeScript | Re-exports context and provider | (all from context + provider) | [index.md](index.md) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| ReferralContext | context.ts | React context for referral state and callbacks |
| useReferralContext | context.ts | Hook to read referral context |
| ReferralProvider | provider.tsx | Fetches referral info, volume, generateCode, multi-level data; provides context value |
