# trading-rewards — Directory Index

## Directory Responsibility

Trading rewards program: epoch info, brokers, current epoch estimate, account/wallet rewards history, claimed state, trading rewards status (component), and env type. Used by apps that show trading rewards or claim flow.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports trading-rewards module | useEpochInfo, useAllBrokers, useCurEpochEstimate, useAccountRewardsHistory, useGetClaimed, useWalletRewardsHistory, useTradingRewardsStatus, useGetEnv, types | [index.md](index.md) |
| types.ts | TS | Trading rewards types | TWType, etc. | [types.md](types.md) |
| useEpochInfo.ts | TS | Epoch info hook | useEpochInfo, EpochInfoType, EpochInfoItem | [useEpochInfo.md](useEpochInfo.md) |
| useAllBrokers.ts | TS | All brokers hook | useAllBrokers, Brokers | [useAllBrokers.md](useAllBrokers.md) |
| useCurEpochEstimate.ts | TS | Current epoch estimate hook | useCurEpochEstimate, CurrentEpochEstimate | [useCurEpochEstimate.md](useCurEpochEstimate.md) |
| useAccountRewardHistory.ts | TS | Account reward history hook | useAccountRewardsHistory, AccountRewardsHistoryRow, AccountRewardsHistory | [useAccountRewardHistory.md](useAccountRewardHistory.md) |
| useGetClaimed.ts | TS | Get claimed hook | useGetClaimed, DistributionId | [useGetClaimed.md](useGetClaimed.md) |
| useWalletRewardsHistory.ts | TS | Wallet rewards history hook | useWalletRewardsHistory, WalletRewards, WalletRewardsItem, WalletRewardsHistoryReturns | [useWalletRewardsHistory.md](useWalletRewardsHistory.md) |
| useTradingRwardsStatus.tsx | TSX | Trading rewards status component | useTradingRewardsStatus, EpochStatus, StatusInfo | [useTradingRwardsStatus.md](useTradingRwardsStatus.md) |
| useGetEnv.ts | TS | Get env hook | useGetEnv, ENVType | [useGetEnv.md](useGetEnv.md) |
| utils.ts | TS | Trading rewards utils | (utils) | [utils.md](utils.md) |
