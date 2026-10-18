# paxeer-network — Directory Index

## Directory Responsibility

paxeer-network domain hooks and services: order book stream, symbols info, markets, mark/index prices, leverage, margin modes, funding, positions, orders, deposit/withdraw, transfer, chains, wallet/balance subscriptions, TPSL, statistics, calculator, and related stores. This is the main data layer for trading and account features.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [calculator](calculator/index.md) | Portfolio/order/position calculator, scheduler | [calculator/index.md](calculator/index.md) |
| [statistics](statistics/index.md) | Asset/funding/transfer/vault history, user stats | [statistics/index.md](statistics/index.md) |
| [useIndexPrice](useIndexPrice/index.md) | Index price store | [useIndexPrice/index.md](useIndexPrice/index.md) |
| [useMarkPrice](useMarkPrice/index.md) | Mark price store | [useMarkPrice/index.md](useMarkPrice/index.md) |
| [useMarket](useMarket/index.md) | Market list/map store | [useMarket/index.md](useMarket/index.md) |
| [useOrderStream](useOrderStream/index.md) | Order stream, sub-account algo order stream | [useOrderStream/index.md](useOrderStream/index.md) |
| [usePositionStream](usePositionStream/index.md) | Position stream and store | [usePositionStream/index.md](usePositionStream/index.md) |
| [useTakeProfitAndStopLoss](useTakeProfitAndStopLoss/index.md) | TPSL order hook and utils | [useTakeProfitAndStopLoss/index.md](useTakeProfitAndStopLoss/index.md) |
| [useTokensInfo](useTokensInfo/index.md) | Tokens info store | [useTokensInfo/index.md](useTokensInfo/index.md) |
| [usePortfolio](usePortfolio/index.md) | Portfolio store | [usePortfolio/index.md](usePortfolio/index.md) |
| [wsTopic](wsTopic/index.md) | Wallet/balance topic hooks | [wsTopic/index.md](wsTopic/index.md) |
| [internal](internal/index.md) | Internal WS observer | [internal/index.md](internal/index.md) |

## Top-Level Files (paxeer-network/)

| File | Language | Summary | Link |
|------|----------|---------|------|
| paxeerNetworkHooks.ts | TS | Re-exports all paxeer-network hooks | [paxeerNetworkHooks.md](paxeerNetworkHooks.md) |
| appStore.ts | TS | App store | [appStore.md](appStore.md) |
| useOrderbookStream.ts | TS | Order book WebSocket stream | [useOrderbookStream.md](useOrderbookStream.md) |
| useSymbolsInfo.ts | TS | Symbols info hook | [useSymbolsInfo.md](useSymbolsInfo.md) |
| useSymbolInfo.ts | TS | Single symbol info | [useSymbolInfo.md](useSymbolInfo.md) |
| useAccountInfo.ts | TS | Account info hook | [useAccountInfo.md](useAccountInfo.md) |
| useMarketsStream.ts | TS | Markets stream | [useMarketsStream.md](useMarketsStream.md) |
| useMarket.ts | TS | Single market hook | [useMarket.md](useMarket.md) |
| useMarkets.ts | TS | Markets list/store | [useMarkets.md](useMarkets.md) |
| useMarkPricesStream.ts | TS | Mark prices stream | [useMarkPricesStream.md](useMarkPricesStream.md) |
| useIndexPricesStream.ts | TS | Index prices stream | [useIndexPricesStream.md](useIndexPricesStream.md) |
| useMarkPrice.ts | TS | Mark price hook | [useMarkPrice.md](useMarkPrice.md) |
| useIndexPrice.ts | TS | Index price hook | [useIndexPrice.md](useIndexPrice.md) |
| useLeverage.ts | TS | Leverage hook | [useLeverage.md](useLeverage.md) |
| useMarginModes.ts | TS | Margin modes hook | [useMarginModes.md](useMarginModes.md) |
| useFundingRate.ts | TS | Funding rate hook | [useFundingRate.md](useFundingRate.md) |
| useFundingDetails.ts | TS | Funding details hook | [useFundingDetails.md](useFundingDetails.md) |
| useFundingRates.ts | TS | Funding rates hook | [useFundingRates.md](useFundingRates.md) |
| useFundingRateHistory.ts | TS | Funding rate history | [useFundingRateHistory.md](useFundingRateHistory.md) |
| usePositionStream (dir) | — | Position stream | see [usePositionStream/index.md](usePositionStream/index.md) |
| useOrderStream (dir) | — | Order stream | see [useOrderStream/index.md](useOrderStream/index.md) |
| useDeposit.ts | TS | Deposit hook | [useDeposit.md](useDeposit.md) |
| useWithdraw.ts | TS | Withdraw hook | [useWithdraw.md](useWithdraw.md) |
| useTransfer.ts | TS | Transfer hook | [useTransfer.md](useTransfer.md) |
| useMaxWithdrawal.ts | TS | Max withdrawal hook | [useMaxWithdrawal.md](useMaxWithdrawal.md) |
| useChains.ts | TS | Chains hook | [useChains.md](useChains.md) |
| useChain.ts | TS | Single chain hook | [useChain.md](useChain.md) |
| useChainInfo.ts | TS | Chain info hook | [useChainInfo.md](useChainInfo.md) |
| useStorageChain.tsx | TSX | Storage chain hook | [useStorageChain.md](useStorageChain.md) |
| useCollateral.ts | TS | Collateral hook | [useCollateral.md](useCollateral.md) |
| useMaxQty.ts | TS | Max quantity hook | [useMaxQty.md](useMaxQty.md) |
| useMarginRatio.ts | TS | Margin ratio hook | [useMarginRatio.md](useMarginRatio.md) |
| useMaintenanceStatus.ts | TS | Maintenance status hook | [useMaintenanceStatus.md](useMaintenanceStatus.md) |
| (others) | TS/TSX | See subdirs and file list | (linked in subdir indexes) |

## Key Entities

| Entity | Responsibility |
|--------|----------------|
| useOrderbookStream | Order book depth stream |
| usePositionStream | Positions stream and store |
| useOrderStream | Orders stream |
| useMarkPrice / useIndexPrice | Mark/index price by symbol |
| useDeposit / useWithdraw / useTransfer | Deposit, withdraw, transfer |
| useChains / useChain | Chain list and current chain |
| useLeverage / useMarginModes | Leverage and margin mode |
