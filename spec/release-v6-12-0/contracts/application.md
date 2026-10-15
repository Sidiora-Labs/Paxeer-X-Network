# Application contract: the Hyperpax application on the v6.12.0 precompiles

Version 1.1.0. This file binds the Hyperpax application (github.com/paxlabs-inc/Hyperpax, commit 242152b) to the precompiles and data services of release v6.12.0 of the Paxeer X Network. It lists every contract call site and the precompile address and method it resolves to, every data surface and the gateway or Chainflow route, topic or operation it reads, and the amount, allowance, time-in-force, take-profit / stop-loss, gas and error semantics.

Citations use `Hyperpax:path:line` at 242152b, `Launch-x:path:line` at c5eb7c7 and plain `path:line` for this repository at 211acbc6c. A "required change" names the current code and the target behaviour. Every command of feature 6 runs in the root of the Hyperpax checkout.

Interface authorities:
- The method signatures, enumerations, events and custom errors of the perpetuals precompile are those of contracts/perps.md.
- The spot precompile set follows contracts/spot.md.
- The gateway routes, topics and GraphQL schema follow contracts/data-services.md.
- Addresses, including the uhpx and SID denom ERC20 native pointers, come from contracts/address-table.md.
- Token custody follows the bank model of release v6.12.0: USDL is the existing ERC20 token 0x85FcD13735F4309833A503EE804ea32395851479 with 6 decimals (Launch-x:contracts/libraries/Constants.sol:14-16), pulled by the precompile with transferFrom against an allowance the user gives to the precompile address; PAX (uhpx) and SID (the SID denom layerxbridgetypes.SidioraDenom()) are native bank coins moved by bank transfer with no allowance.
- Charts, prices, candles and reference book snapshots come from Chainflow for every symbol; the gateway serves the venue's own fills and book for perpetuals and spot and the account-scoped state and history.

This file states what the application sends and reads. It does not redefine those interfaces.

## Address table

The application resolves every chain call through one module, `src/core/network/precompiles.ts` (planned path).

| Role | Address | Replaces in the application |
|---|---|---|
| Perpetuals precompile | 0x1015 | `DIAMOND_ADDRESS` at Hyperpax:src/core/perps/config.ts:10-11, plus every per-user TradingAccount vault address from `getVaultAddress` (Hyperpax:src/hooks/perps/useVault.ts:8-15) |
| Launchpad precompile | 0x1017 | not called by the application; listed as an allowance spender for USDL |
| Spot PECOR (market rail) | 0x1021 | `PECOR_CONTRACTS.PECOR` (Hyperpax:src/core/spot/addresses.ts:32-35) |
| Spot PECOROrders (limit rail) | 0x1022 | `PECOR_CONTRACTS.PECOROrders` (Hyperpax:src/core/spot/addresses.ts:40-43) |
| Spot PECORStopOrders (stop rail) | 0x1023 | `PECOR_CONTRACTS.PECORStopOrders` (Hyperpax:src/core/spot/addresses.ts:44-47) |
| Spot PECORQuoter | 0x1024 | `PECOR_CONTRACTS.PECORQuoter` (Hyperpax:src/core/spot/addresses.ts:36-39) |
| Spot PECORVault | 0x1025 | `PECOR_CONTRACTS.PECORVault` (Hyperpax:src/core/spot/addresses.ts:48-51); no application call |
| USDL token | 0x85FcD13735F4309833A503EE804ea32395851479 | `COLLATERAL_TOKENS.usdl` (Hyperpax:src/core/perps/config.ts:75-79) and `SPOT_TOKENS.USDL` (Hyperpax:src/core/spot/addresses.ts:146-153) |
| PAX token | the ERC20 native pointer of uhpx (contracts/address-table.md) | `SPOT_TOKENS.PAX` with the native placeholder and WPAX9 wrapped address (Hyperpax:src/core/spot/addresses.ts:85-97) and `SPOT_TOKENS.WPAX9` (:98-105) |
| SID token | the ERC20 native pointer of the SID denom (contracts/address-table.md) | `SPOT_TOKENS.SID` (Hyperpax:src/core/spot/addresses.ts:106-113) |

Rules:
1. Each precompile address is the 20-byte form of the short address (0x1015 is 0x0000000000000000000000000000000000001015) and is exported as a viem `Address`.
2. One override set exists: NEXT_PUBLIC_PERPS_PRECOMPILE, NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE and NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE, NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE, NEXT_PUBLIC_PECOR_STOP_PRECOMPILE, NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE, NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE, read through the pattern of `envAddress` (Hyperpax:src/core/spot/addresses.ts:23-27), so a local node can supply its own addresses. A value that is not 0x plus 40 hex digits throws at module load with the variable name. Required change: `envAddress` returns the raw value today, and the old NEXT_PUBLIC_SPOT_PECOR, _QUOTER, _ORDERS, _STOP_ORDERS, _VAULT, _TX_TRACKER and _PRICE_ORACLE overrides (Hyperpax:src/core/spot/addresses.ts:31-60) are removed in the same change.
3. `PECOR_CONTRACTS.TransactionTracker` (Hyperpax:src/core/spot/addresses.ts:52-55) and `PECOR_CONTRACTS.PriceOracle` (:56-59) are removed. The application has no oracle call: the oracle precompile 0x1020 serves the pricefeed interface of contracts/pricefeed.md, which matches neither the spot `priceOracleAbi` nor `OracleFacet.json`, and a pricefeed ABI is added only with a call site.
4. PECORVaultShare (0x1026) and PECORRouter (0x1027) carry no code and have no entry.
5. USDL is one token at one address. Required change: the spot USDL entry (Hyperpax:src/core/spot/addresses.ts:146-153) carries a different address today; both entries read the one USDL address from the address module.
6. The PAX and SID entries are the uhpx and SID denom pointer addresses that the baseline task "Create the native ERC20 pointers and bank metadata for uhpx and the SID denom before venue activation" records in contracts/address-table.md, each with 6 decimals as contracts/spot.md states them. No WPAX9 entry remains, and the native placeholder `NATIVE_PAX_ADDRESS` (Hyperpax:src/core/spot/addresses.ts:85) is used only for the native-value rail of rule 3 of Spot call sites.
7. The chain stays chain id 125 (Hyperpax:src/core/chain.ts:16-17). The embedded wallet reads its chain id from that object, not from a second literal (Hyperpax:src/core/wallet/embedded/client.ts:41).

## Spot tokens and markets

| Table | Today | v6.12.0 |
|---|---|---|
| `SPOT_TOKENS` (Hyperpax:src/core/spot/addresses.ts:88-155) | PAX (native, 18 decimals), WPAX9, SID, WETH, WBNB, USDC, USDT, USDL | USDL, PAX (uhpx pointer, 6 decimals), SID (SID denom pointer, 6 decimals) |
| `SPOT_STABLECOIN_LIST` (Hyperpax:src/core/spot/addresses.ts:167-171) | USDL, USDC, USDT | USDL |
| `SPOT_MARKETS` (Hyperpax:src/core/spot/config.ts:59-84) | SID, BNB, ETH and PAX markets | PAX/USDL and SID/USDL, the opening pairs of contracts/spot.md |
| `selectStableForBuy` / `selectStableForSell` (Hyperpax:src/core/spot/stableSelector.ts:78, :119; imported at Hyperpax:src/widgets/spot/SpotOrderForm.tsx:29) | ranks USDL, USDC and USDT by balance | returns USDL |
| `SPOT_TOKENS` and `SPOT_MARKETS_META` of the perpetuals configuration (Hyperpax:src/core/perps/config.ts:89-117, :119-141) | Diamond-era spot tables with no reader | deleted |

Rules:
1. A market's `baseDecimals` and `baseAddress` (Hyperpax:src/core/spot/config.ts:37-58) come from the token entry, so a 1 PAX sell encodes 1000000 base units.
2. The PAX balance shown by the spot form (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:392-403) is the uhpx balance at 6 decimals, read as `balanceOf` on the pointer. The native wallet balance in wei and the pointer balance are the same coin: the pointer balance equals the wei balance divided by 1e12.
3. A further market enters the tables only when contracts/spot.md registers it.

## ABI modules

Two modules carry the precompile ABIs:
- `src/core/network/abi/PerpsPrecompile.ts` (planned path) is generated from contracts/perps.md.
- The spot ABI files under `src/core/network/spot_abi/` are regenerated from contracts/spot.md.

Both use the `as const` export shape of the current generated files (Hyperpax:src/core/network/abi/PositionFacet.ts:1-2), so viem infers argument and return types.

Rules:
1. The perps module exports one ABI holding every method the application calls (listed in Perpetual call sites and Account and collateral), the `OrderParams` and `PositionDetails` tuples, the `OrderType` and `TimeInForce` enumerations, every event and every custom error of contracts/perps.md#19-errors.
2. The application calls 32 distinct 0x1015 methods today, from six hook files: usePosition.ts, usePerpsOrders.ts, useMarginMode.ts, useVault.ts, useQuoter.ts and usePortfolioStats.ts. The ABI test lists each of them with its v6.12.0 replacement or removal from the tables below.
3. The barrels hold only the precompile ABIs. Required change: the Diamond facet exports of Hyperpax:src/core/network/abi/index.ts:1-27 (PositionFacet, OrderBookFacet, QuoterFacet, TradingAccount, VaultFactoryFacet, OracleFacet and the Spot facets) and the spot exports without a precompile (Hyperpax:src/core/network/spot_abi/index.ts:3-16: pecorRouterAbi, pecorVaultShareAbi, vaultAdapterAbi, oracleHubAbi, priceOracleAdapterAbi, sidioraFeedAdapterAbi, sidioraAdapterAbi, transactionTrackerAbi, priceOracleAbi) are deleted with their source files.
4. The spot ABI files keep the selectors the application encodes (for example `marketBuy` at Hyperpax:src/core/network/spot_abi/PECOR.ts:313 and `placeLimitBuy` at Hyperpax:src/core/network/spot_abi/PECOROrders.ts:526) and gain every custom error contracts/spot.md lists for the rail: Expired, SameToken, StalePrice, InsufficientLiquidity, InsufficientOutput, ZeroAmount, NativeTransferFailed, MulticallFailed, UseWethDeposit, UseWethWithdraw, WethOnly, UnsupportedTransfer, UnsupportedInputTransfer and the rest of that list. A selector that differs fails the ABI golden test.
5. Reshaped views are decoded from the regenerated ABI by output name, never by the Diamond tuple: getFees returns takerFeePpm, makerFeePpm, liquidationFeePpm and insuranceSharePpm in parts per million (a consumer divides by 1e6, where the Diamond outputs were basis points); getMarkPriceDetails returns markPrice, indexPrice, midPrice and fresh; getFundingDetails returns cumulativeFundingPerUnit, lastSettleTime, lastHourlyRate, premiumSampleSum and premiumSampleCount; getLiquidationPrice takes a positionId. No application module calls these views or decodes the Diamond MarkPriceChanged event today; the MarkPriceChanged record type (Hyperpax:src/types/perps.ts:56) is read only through the gateway GraphQL schema, and any on-chain event decoding uses the events of contracts/perps.md section 18.
6. `readContract` casts its params `as any` and sends no account today (Hyperpax:src/hooks/perps/useContract.ts:8-16). Required change: the precompile call builders pass typed ABIs, so a signature drift between contracts/perps.md and the application fails `pnpm tsc --noEmit`, and every read passes the connected account (Account and collateral rule 2).

## Perpetual call sites

Every perpetuals call goes to 0x1015, built by the pure call builders in `src/core/perps/calls.ts` (planned path). The hooks submit what the builders return through `writeContract` (Hyperpax:src/hooks/perps/useContract.ts:49-101). `writeContract` simulates first (Hyperpax:src/hooks/perps/useContract.ts:73-80) and then signs (Hyperpax:src/hooks/perps/useContract.ts:90-100).

The compatibility methods keep their fixed arity in contracts/perps.md. A bounded call, one that carries acceptablePrice, deadline, time in force, reduce-only or attached take-profit / stop-loss, goes through `placeOrder(OrderParams)` (contracts/perps.md section 5) or `closePositionWithParams(positionId, closeSizeUsd, acceptablePrice, deadline)` (contracts/perps.md section 8).

| Hook function (today) | Today's target | v6.12.0 call at 0x1015 |
|---|---|---|
| `openPosition` (Hyperpax:src/hooks/perps/usePosition.ts:63-64) | Diamond PositionFacet (Hyperpax:src/core/network/abi/PositionFacet.ts:765) | placeOrder with orderType MARKET, timeInForce IOC (FOK when chosen), collateral, leverage, acceptablePrice, deadline, takeProfitPrice, stopLossPrice |
| `addSize` (Hyperpax:src/hooks/perps/usePosition.ts:121-122) | Diamond (PositionFacet.ts:556) | placeOrder with orderType MARKET, timeInForce IOC, on the market and side of the position, with the added collateral, leverage, acceptablePrice and deadline |
| `closePosition` (Hyperpax:src/hooks/perps/usePosition.ts:79-80) | Diamond (PositionFacet.ts:569) | closePositionWithParams with closeSizeUsd equal to the position size |
| `partialClose` (Hyperpax:src/hooks/perps/usePosition.ts:89-90) | Diamond (PositionFacet.ts:789) | closePositionWithParams with the chosen closeSizeUsd |
| `addCollateral` (Hyperpax:src/hooks/perps/usePosition.ts:100-101) | Diamond (PositionFacet.ts:533) | addCollateral, unchanged arguments |
| `removeCollateral` (Hyperpax:src/hooks/perps/usePosition.ts:110-111) | Diamond (PositionFacet.ts:807) | removeCollateral, unchanged arguments |
| `getPosition` (Hyperpax:src/hooks/perps/usePosition.ts:29-30) | Diamond (PositionFacet.ts:638) | getPosition; take-profit, stop-loss and liquidation price come from getPositionDetails |
| `getUserPositionIds` (Hyperpax:src/hooks/perps/usePosition.ts:38-39) | Diamond (PositionFacet.ts:726) | getUserPositionIds(account) |
| `getOpenInterest` (Hyperpax:src/hooks/perps/usePosition.ts:47-48) | Diamond (PositionFacet.ts:595) | getOpenInterest |
| `placeLimitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:36-40) | Diamond OrderBookFacet (Hyperpax:src/core/network/abi/OrderBookFacet.ts:709) | placeOrder with orderType LIMIT, timeInForce, reduceOnly, takeProfitPrice, stopLossPrice, deadline and cloid |
| `placeStopLimitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:58-59) | Diamond (OrderBookFacet.ts:763) | placeOrder with orderType STOP_LIMIT, the trigger and limit prices and the same fields |
| `placeTakeProfitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:81-82) | Diamond (OrderBookFacet.ts:811) | setPositionTpSl(positionId, takeProfitPrice, stopLossPrice) from the take-profit / stop-loss modal |
| `placeStopLossOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:91-92) | Diamond (OrderBookFacet.ts:787) | setPositionTpSl, as above |
| `cancelOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:71-72) | Diamond (OrderBookFacet.ts:559) | cancelOrder; a scaled ladder placed with cloids is cancelled rung by rung with cancelByCloid(marketId, cloid) |
| `getOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:10-11) | Diamond (OrderBookFacet.ts:591) | getOrder |
| `getUserOrderIds` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:19-20) | Diamond (OrderBookFacet.ts:660) | getUserOrderIds(account) |
| `quoteMarket` (Hyperpax:src/hooks/perps/useQuoter.ts:116-122) | Diamond QuoterFacet (Hyperpax:src/core/network/abi/QuoterFacet.ts:67) | quoteMarket, the fallback of the Chainflow ticker |
| `quoteOpenPosition` (Hyperpax:src/hooks/perps/useQuoter.ts:126-140) | Diamond (QuoterFacet.ts:163) | quoteOpenPosition |
| `quoteClosePosition` (Hyperpax:src/hooks/perps/useQuoter.ts:142-150) | Diamond (QuoterFacet.ts:11) | quoteClosePosition |
| none today | Diamond (QuoterFacet.ts:239) | quotePartialClose(positionId, closeSizeUsd), returning exitPrice, closedPnl, fee and estimatedPayout |
| none today | none | getPositionDetails(positionId), read by the positions panel and the take-profit / stop-loss readback |

Rules:
1. The enumerations are those of contracts/perps.md section 2: orderType LIMIT 0, STOP_LIMIT 1, TAKE_PROFIT 2, STOP_LOSS 3, MARKET 4, STOP_MARKET 5; tradeType OPEN 0, CLOSE 1, LIQUIDATION 2, REDUCE 3, INCREASE 4, ADL 5, BACKSTOP 6; feeType TRADING 0 and MAKER_REBATE 4. Required change: `ORDER_TYPE_LABELS` and `ORDER_TYPE_COLORS` (Hyperpax:src/core/perps/config.ts:212-217, :219-224) keep Limit 0, Stop-Limit 1, Take Profit 2 and Stop Loss 3 and gain Market 4 and Stop Market 5; `TRADE_TYPE_LABELS` (:270-275) keeps 0 to 3 and gains Increase 4, ADL 5 and Backstop 6; `FEE_TYPE_LABELS` (:262-267) keeps Trading 0 and gains Maker Rebate 4. Every order, fee and trade row then decodes to its label.
2. `getPositionCollateralToken` reads getPosition output index 5 (Hyperpax:src/hooks/perps/usePosition.ts:69-73). It reads the field by name from the regenerated ABI instead of by index.
3. Required change: partialClose has no caller in the interface. The positions panel adds a partial-close action beside `handleClose` (Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:857-876). It quotes through quotePartialClose and then submits closePositionWithParams with a price bound.
4. Every write keeps the fee clamp of `estimateClampedFees` (Hyperpax:src/hooks/perps/useContract.ts:22-47) and the receipt wait of `waitForTx` (Hyperpax:src/hooks/perps/useContract.ts:146-175).

## Account and collateral

Margin is cross margin in USDL, held by the perpetuals precompile per account. The per-user TradingAccount vault contract of today is not deployed against v6.12.0. Its calls move to the account methods of 0x1015 named in contracts/perps.md sections 3 and 4:

| Hook function (today) | Today's target | v6.12.0 |
|---|---|---|
| `getVaultAddress` / `createVault` (Hyperpax:src/hooks/perps/useVault.ts:8-24) | Diamond VaultFactoryFacet (Hyperpax:src/core/network/abi/VaultFactoryFacet.ts:43, :75) | No application call. The first deposit opens the account at 0x1015; createVault and getVault stay in the interface of contracts/perps.md for callers that use them. The create step of `DepositWithdrawModal` (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:233) is removed. |
| `deposit` (Hyperpax:src/hooks/perps/useVault.ts:44-52) | per-user vault, TradingAccount (Hyperpax:src/core/network/abi/TradingAccount.ts:281) | deposit(USDL, amount) at 0x1015, after the USDL allowance of Allowances |
| `withdraw` (Hyperpax:src/hooks/perps/useVault.ts:54-62) | per-user vault (TradingAccount.ts:1034) | withdraw(USDL, amount) at 0x1015 |
| `getBalance` / `getLockedBalance` (Hyperpax:src/hooks/perps/useVault.ts:26-42; Multicall3 aggregate at Hyperpax:src/hooks/perps/usePortfolioStats.ts:153-161) | per-user vault (TradingAccount.ts:388, :581) | getAccount(account) at 0x1015 (collateral, equityUsd, initialMarginUsd, maintenanceMarginUsd, withdrawable), and the account-argument forms of getBalance and getLockedBalance |
| `useTopUpPosition` (Hyperpax:src/hooks/perps/useMarginMode.ts:147-178) | per-user vault (TradingAccount.ts:957) | topUpPosition at 0x1015 |
| `useTransferMargin` (Hyperpax:src/hooks/perps/useMarginMode.ts:180-211) | per-user vault (TradingAccount.ts:980) | Removed with `TransferMarginModal` (Hyperpax:src/widgets/trading/modals/TransferMarginModal.tsx), its import and mount (Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:34, :1240), its action button (:514) and its copy (Hyperpax:src/core/perps/config.ts:362, :371, :376). Margin is cross, so transferMargin stays in the interface reverting CROSS_MARGIN_ONLY (contracts/perps.md section 4) and the application never sends it. |
| `useAccountSummary` (Hyperpax:src/hooks/perps/useMarginMode.ts:213-264) | per-user vault (TradingAccount.ts:320) | the account-argument form of getAccountSummary at 0x1015 |
| `usePositionMargin` (Hyperpax:src/hooks/perps/useMarginMode.ts:96-145) | per-user vault (TradingAccount.ts:613) | getPositionMargin(positionId) at 0x1015 |
| `useMarginMode` / `useSetMarginMode` (Hyperpax:src/hooks/perps/useMarginMode.ts:15-94) | per-user vault (TradingAccount.ts:594, :939) | Removed. The mode is cross. `MARGIN_MODE_LABELS` (Hyperpax:src/core/perps/config.ts:286-289) shows the cross label, and the toggle (Hyperpax:src/widgets/trading/modals/MarginModeToggle.tsx:38, :53; mounted at Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:1008) is removed. |
| `useEmergencyWithdraw` (Hyperpax:src/hooks/perps/useMarginMode.ts:266-297) | per-user vault (TradingAccount.ts:307) | Removed. Withdraw-all sends withdraw(USDL, getAccount(account).withdrawable). |
| `useAutoCollateral` (Hyperpax:src/hooks/perps/useAutoCollateral.ts:5, :36, :43) | getVaultAddress and getBalance of useVault per collateral token | Rewritten onto the account hook: the balance is getAccount(account).collateral in USDL base units at 6 decimals, and the fallback entry (:76, :90-91, "USID" at 18 decimals today) is USDL at 6 decimals. |

Rules:
1. Collateral is USDL only. `COLLATERAL_TOKENS` (Hyperpax:src/core/perps/config.ts:64-80) and `COLLATERAL_OPTIONS` (Hyperpax:src/core/perps/config.ts:82-87) list the single USDL entry.
2. Every view that contracts/perps.md resolves for an account takes that account as an explicit argument (getAccount(address account) at contracts/perps.md section 3 and the account-argument forms that feature 3 adds for getBalance, getLockedBalance and getAccountSummary), and the application passes the connected address. Required change: the Multicall3 aggregate of Hyperpax:src/hooks/perps/usePortfolioStats.ts:153-161 (`MULTICALL3` at :16) is replaced by direct reads of getAccount(account), because a caller-resolved read through Multicall3 resolves the multicall contract, and a read without `account` resolves the zero address.
3. Deposit and withdrawal amounts are USDL base units (six decimals). Withdraw-all reads withdrawable, never the idle balance, because idle exceeds withdrawable while a position is open.

## Spot call sites

Every spot call keeps its method and argument list and changes only the address it resolves to.

| Function (Hyperpax:src/hooks/spot/useSpotOrders.ts) | Method | Address | Allowance spender for an ERC20 input |
|---|---|---|---|
| `marketBuy` :129-172 (write :163-168) | marketBuy | 0x1021 | 0x1021 (:151-156) |
| `marketSell` :197-261 (write :253-258) | marketSell, or swapExactInNative for a native PAX input | 0x1021 | 0x1021 |
| `placeLimitBuy` :280 (write :297-300) | placeLimitBuy | 0x1022 | 0x1022 (:288-293) |
| `placeLimitSell` :315 | placeLimitSell | 0x1022 | 0x1022 |
| `cancelLimitOrder` :341 | cancelLimitOrder | 0x1022 | none |
| `modifyLimitOrder` :351 | modifyLimitOrder | 0x1022 | none |
| `getUserLimitOrders` :367 / `getLimitOrder` :377 | getUserLimitOrders / getLimitOrder | 0x1022 | none |
| `placeStopLimitBuy` :402 / `placeStopLimitSell` :445 | placeStopLimitBuy / placeStopLimitSell | 0x1023 | 0x1023 |
| `cancelStopLimitOrder` :476 | cancelStopLimitOrder | 0x1023 | none |
| `placeStopLoss` :500 | placeStopLoss | 0x1023 | 0x1023 |
| `cancelStopLossOrder` :526 | cancelStopLossOrder | 0x1023 | none |
| `getUserStopLimitOrders` :538, `getUserStopLossOrders` :548, `getStopLimitOrder` :558, `getStopLossOrder` :567 | same names | 0x1023 | none |

| Function (Hyperpax:src/hooks/spot/useSpotQuoter.ts) | Method | Address |
|---|---|---|
| quote call at :141 | quoteMarketBuy | 0x1024 |
| quote call at :160 | quoteMarketSell | 0x1024 |
| quote call at :178 | quoteExactIn | 0x1024 |
| quote call at :194 | quoteExactOut | 0x1024 |
| quote call at :217 | batchQuote | 0x1024 |
| price call at :231 | getTokenPrice | 0x1024 |
| reserve call at :246 | `getLiquidityInfo` | 0x1024 |
| reserve call at :264 | `getAllLiquidityInfo` | 0x1024 |
| TWAP call at :286 | getTWAP | 0x1024 |

Rules:
1. Every quoter read targets 0x1024 and the 23-field QuoteResult parse is unchanged.
2. No wrap exists. Required change: the WPAX9 `deposit` before a sell (Hyperpax:src/hooks/spot/useSpotOrders.ts:213-232, the wrap call at :222-229) and the helpers of Hyperpax:src/core/spot/native.ts (imported at Hyperpax:src/hooks/spot/useSpotOrders.ts:35-41) are deleted.
3. A PAX sell passes the uhpx pointer as the token of marketSell, which moves bank uhpx with no allowance. A sell paid from native value calls swapExactInNative(tokenOut, amountOutMin, deadline) with value = amount × 1e12 wei, which contracts/spot.md credits as value / 1e12 uhpx; a value that is not a whole multiple of 1e12 is refused before signing.
4. Spot deadlines keep the semantics of the engine (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:132-135): a non-zero deadline in the past reverts Expired. The application always sends now plus `DEFAULT_MARKET_DEADLINE_SECONDS` (Hyperpax:src/core/spot/slippage.ts:27, `buildDeadline` at :75).
5. The spot route stays gated as it is today. `getServerSideProps` returns notFound (Hyperpax:src/pages/spot.tsx:2-3), asserted by Hyperpax:tests/audit-regressions.test.ts:7-11. The spot widgets are retargeted behind that gate, and opening the route is a cut-over step of the release runbook.

## Amounts

| Quantity | Units on the wire | Conversion |
|---|---|---|
| USDL collateral, deposit, withdrawal, top-up, add-collateral, remove-collateral, add-size collateral | 6-decimal base units | `safeParseUnits(value, 6)` (Hyperpax:src/core/perps/amounts.ts:26-38) |
| Prices: trigger, limit, take-profit, stop-loss, acceptable price | 18-decimal USD | `safeParseUnits(value, 18)`, as the form already does (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:335-348) |
| sizeUsd, closeSizeUsd | 18-decimal USD | `safeParseUnits(value, 18)` |
| Leverage | 18-decimal multiple, at least 1x | `safeParseUnits(value, 18)` with the sub-1x refusal at Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:327-333 |
| Fees from getFees | parts per million | value / 1e6 |
| Spot token amounts | the token's own decimals: 6 for USDL, PAX and SID | `parseTokenAmount` (Hyperpax:src/core/spot/slippage.ts:99) |
| Native PAX value | wei, 18 decimals | uhpx amount × 1e12 |

Required changes:
1. AdjustCollateralModal converts add-collateral, remove-collateral and the add-size collateral with `parseEther`, which is 18 decimals (Hyperpax:src/widgets/trading/modals/AdjustCollateralModal.tsx:68, :81, :86). They convert with six decimals. The leverage argument at :86 stays at 18 decimals.
2. TopUpPositionModal (Hyperpax:src/widgets/trading/modals/TopUpPositionModal.tsx:39) converts with six decimals.
3. DepositWithdrawModal builds the amount with floating point, `BigInt(Math.floor(num * 10 ** decimals))` (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:198). It uses `safeParseUnits` with the token's decimals.
4. `getCollateralDecimals` returns 18 for an unknown token (Hyperpax:src/core/perps/amounts.ts:40-45). It throws for an unknown token, so no amount is ever scaled by a guessed decimal count.
5. Spot stable amounts keep `decimalsStable = 6` only through the token table (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:418). The value is read from the market's quote token metadata.
6. The modal conversions live in the pure module `src/core/perps/collateralInputs.ts` (planned path), so the six-decimal rule is tested without rendering.

## Approvals

The precompiles pull ERC20 tokens with transferFrom against the allowance the user gives to the precompile address, as Hyperpax approves the Diamond today. Native coins (PAX as uhpx, SID as the SID denom) move by bank transfer and need no allowance. The allowance planner stays and is retargeted:

| Rail | Token | Spender | Allowance requested |
|---|---|---|---|
| Perpetuals deposit | USDL | 0x1015 | exactly the deposit amount when the current allowance is below it |
| Perpetuals order and position writes that pull collateral from the wallet (contracts/perps.md section 3) | USDL | 0x1015 | exactly the collateral amount of the call when the current allowance is below it |
| Launchpad buy (no application call in this release) | USDL | 0x1017 | exactly the input amount |
| Spot market rail | ERC20 input (USDL) | 0x1021 | exactly the input amount |
| Spot limit rail | ERC20 input (USDL) | 0x1022 | exactly the escrowed amount |
| Spot stop rail | ERC20 input (USDL) | 0x1023 | exactly the escrowed amount |
| Any rail | PAX or SID pointer, native value | none | no allowance is read or requested |

Rules:
1. `needsApproval(token, allowance, amount)` returns false for the PAX and SID pointers and for native value, and otherwise true only when allowance < amount. Required change: today `ensureAllowance` also requires the allowance to be at least `MAX_UINT256 / 2` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:40, :139). That threshold is removed.
2. When the allowance is short, `ensureAllowance` approves exactly the amount through `approveExact` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:87-100), not `approveMax` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:67-79, called at :143).
3. The perpetuals approval helper `ensureDiamondAllowance` (Hyperpax:src/hooks/perps/usePosition.ts:13-22) becomes the planner step with spender PERPS_SPENDER = 0x1015.
4. DepositWithdrawModal's own allowance check (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:203-204) uses the same planner.
5. No approval requests more than the amount of the call it precedes, and none equals 2^256 − 1.

## Time in force

The perpetuals order form has an Order options popover (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1082-1088). Its toggles map to the OrderParams fields of contracts/perps.md (TimeInForce GTC 0, IOC 1, FOK 2, POST_ONLY 3):

| Form state (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx) | Sent as |
|---|---|
| `ioc` :292 | timeInForce = IOC |
| `fok` :293 | timeInForce = FOK |
| `postOnly` :291 | timeInForce = POST_ONLY |
| none of the three on a limit or stop-limit order | timeInForce = GTC |
| none of the three on a market order | timeInForce = IOC |
| `reduceOnly` :290 | reduceOnly = true |
| `hidden` :295 | Removed. The perpetuals venue has no hidden orders. |
| `orderConfirm` :294 | A confirmation step before signing; its content is stated in Gas and confirmation. |

Rules:
1. IOC, FOK and post-only are mutually exclusive. The IOC/FOK toggles already exclude each other (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1084-1085), and post-only additionally clears both.
2. A market order sends IOC, or FOK when FOK is chosen, because a MARKET order with GTC reverts INVALID_TIME_IN_FORCE (contracts/perps.md section 5) and an omitted field encodes 0 = GTC. Post-only stays disabled for market orders (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1083).
3. Spot limit orders with IOC or FOK use the market rail today, with a minimum output taken from the market quote (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:421-427). Required change: the minimum output is computed from the limit price with `expectedOutFromPrice` (Hyperpax:src/core/spot/slippage.ts:160). A buy then never pays above the limit and a sell never receives below it. The swap is all-or-nothing, so IOC and FOK both fill completely at or inside the limit or revert InsufficientOutput (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:78).
4. Spot post-only keeps forcing GTC (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:367). The spot reduce-only toggle (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:207, :816) is removed, because the spot venue holds no positions.

## Take-profit and stop-loss

1. With TP/SL enabled (`tpslEnabled`, Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:234), the market branch sends `tpPrice` and `slPrice` (:235-236) as takeProfitPrice and stopLossPrice of the placeOrder call. Today the call at :482 sends neither. An unset price is sent as 0, which means none.
2. The limit and stop-limit branches (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:486, :490) send the same two fields. The precompile attaches them when the order fills.
3. Validation before signing:
   - A long requires stopLossPrice < reference price < takeProfitPrice. A short requires the mirror.
   - The reference price is the quote entryPrice for market orders and the trigger price for limit orders.
   - A failing pair blocks submit with the TRADE_TP_SL_INVALID message (Hyperpax:src/core/errors/classify.ts:251-253).
4. Required change: SetTpSlModal (Hyperpax:src/widgets/trading/modals/SetTpSlModal.tsx:84-91) places a separate take-profit and stop-loss order today. It sends one setPositionTpSl(positionId, takeProfitPrice, stopLossPrice) with 18-decimal prices, where 0 clears a side.
5. The readback of attached prices uses getPositionDetails(positionId), which carries takeProfitPrice, stopLossPrice and liquidationPrice; getPosition carries none of them.
6. After a successful submit, the chart order lines from the form are cleared together with the inputs.

## Price bound and deadline

1. Market opens and bounded addSize:
   - acceptablePrice = entryPrice * (10000 + bps) / 10000 for a long, and entryPrice * (10000 - bps) / 10000 for a short.
   - bps is `DEFAULT_SLIPPAGE_BPS` (Hyperpax:src/core/perps/config.ts:354), which nothing reads today.
2. Closes and partial closes use the exitPrice of quoteClosePosition / quotePartialClose with the opposite rounding.
3. deadline = the latest block timestamp plus `DEFAULT_MARKET_DEADLINE_SECONDS` (Hyperpax:src/core/spot/slippage.ts:27), built with `buildDeadline` (:75).
4. The quote expiry check (`QUOTE_MAX_AGE_MS`, Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:178) refreshes the quote instead of blocking submit, so an aged quote is re-fetched and the bound recomputed.
5. Integer arithmetic only. Every bound is a bigint at 18 decimals.

## Order plan

`src/core/perps/orderPlan.ts` (planned path) is a pure function. It takes the form state and returns the ordered call list that `handleSubmit` (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:444-506) submits:
- market: one placeOrder with orderType MARKET;
- limit: one placeOrder with orderType LIMIT;
- stop-limit: one placeOrder with orderType STOP_LIMIT;
- scaled: N placeOrder calls with orderType LIMIT, each with its own cloid.

Scaled order rules:
1. The ladder reads `scaledStartPrice`, `scaledEndPrice`, `scaledTotalOrders` and `scaledDistribution` (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:227-231).
2. The prices are N evenly spaced points from start to end inclusive.
3. The flat distribution splits sizeUsd equally. Ascending and descending weight rung i by i+1 and N-i respectively. The integer remainder goes to the last rung.
4. The custom distribution has no input field (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:29), so the plan returns no calls and the submit stays disabled.
5. 2 to 20 rungs are accepted.
6. Rung i carries the cloid of the ladder id and i, so the ladder is cancelled with cancelByCloid per rung.

## Gas and confirmation

1. Gas: every write is estimated by eth_estimateGas through the simulate path of `writeContract` (Hyperpax:src/hooks/perps/useContract.ts:73-80) and sent with gas = estimate × 12 / 10. Today the write at :90-100 sends no gas field and leaves the estimate to the wallet (comment :82-87). Each precompile charges the fixed per-method gas of its contract (contracts/perps.md section 21), so the estimate is at least the figure of that table plus the log gas, and the margin covers the log gas of a fill that the simulation did not see.
2. The confirmation step of `orderConfirm` shows side, size, acceptablePrice, the trading fee (tradingFeeUsd) and the liquidationPrice from quoteOpenPosition (contracts/perps.md section 17), and nothing is signed until it is accepted.
3. The quote fallback reads maxLeverage and maintenanceMarginBps from quoteMarket at 0x1015 instead of `PERPS_MARKETS` and the fixed 1000 of Hyperpax:src/hooks/perps/useQuoter.ts:108-109.
4. Every market lists maxLeverage 1000. Required change: `PERPS_MARKETS` (Hyperpax:src/core/perps/config.ts:167-186) carries 1000 for BTC, ETH, PAX and SID, 200 for LINK (:172) and 500 for the other thirteen entries; all 18 entries carry 1000, matching the market table of contracts/perps.md.

## Data surfaces

Two origins serve the application's data. `FEED_ORIGIN` (Hyperpax:src/core/feeds.ts:2, from NEXT_PUBLIC_MARKET_PROXY_HOST) is the Chainflow origin; `NEXT_PUBLIC_PERPS_GATEWAY_URL` is the gateway origin of contracts/data-services.md. No module outside `src/core/feeds.ts`, `src/core/network/gateway.ts` (planned path), `src/core/rpc.ts` and `src/core/chain.ts` names a data or chain endpoint host. Required change: the endpoint literals of Hyperpax:src/core/perps/config.ts:23-36, Hyperpax:src/core/spot/api.ts:16, Hyperpax:src/core/perfdb/client.ts:14, Hyperpax:src/hooks/spot/useSpotUserStream.ts:34, Hyperpax:src/hooks/chain/useExplorerStream.ts:29, the Chainflow host fallbacks of Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:50-52, Hyperpax:src/hooks/spot/useSpotMarketStream.ts:23-25 and Hyperpax:src/hooks/perps/useMarketsStream.ts:22-24, and the RPC fallbacks of Hyperpax:src/core/wagmi.ts:24 and Hyperpax:src/core/wallet/embedded/client.ts:35 move into those modules. Outbound page links and the embedded wallet's own service origins (Hyperpax:src/core/wallet/embedded/client.ts:15-16) are not data endpoints and stay.

| Surface | Source | Origin | Rule |
|---|---|---|---|
| Chart candles: `candleFeedUrl` (Hyperpax:src/core/feeds.ts:8-14), the candle proxy (Hyperpax:src/pages/api/candle/[...path].ts:5, :10) | Chainflow | FEED_ORIGIN | unchanged; the gateway serves no candle route |
| Market ticker and open interest of useQuoter (`MARKET_DATA_URL` imported as `REST_API_URL` at Hyperpax:src/hooks/perps/useQuoter.ts:4; reads at :54 and :82) | Chainflow | MARKET_DATA_URL (Hyperpax:src/core/feeds.ts:3) | unchanged; on failure the quote falls back to quoteMarket at 0x1015 (Hyperpax:src/hooks/perps/useQuoter.ts:116-122) |
| Ticker prices of useRealtimePrices (`${MARKET_DATA_URL}/tickers`, Hyperpax:src/hooks/perps/useRealtimePrices.ts:3, :56) | Chainflow | MARKET_DATA_URL | unchanged |
| Network health probe (`${FEED_ORIGIN}/api/explorer/health`, Hyperpax:src/widgets/network-pill/NetworkPill.tsx:18, :38) | Chainflow | FEED_ORIGIN | unchanged |
| Reference order book and trades: `<SYMBOL>@orderbook`, `@trade`, `@kline_5m` on `/ws/<sym>` (Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:50-52, :69-70; Hyperpax:src/hooks/spot/useSpotMarketStream.ts:23-25, :34) | Chainflow | FEED_ORIGIN | unchanged; the reference book of every listed symbol |
| Markets stream `/ws/markets` (Hyperpax:src/hooks/perps/useMarketsStream.ts:22-24, :29) | Chainflow | FEED_ORIGIN | unchanged |
| Chain stream `/ws/explorer` (Hyperpax:src/hooks/chain/useExplorerStream.ts:29, frames at :165-166) | Chainflow | FEED_ORIGIN | Required change: the literal at :29 becomes the FEED_ORIGIN-derived constant |
| Venue book and fills, perpetuals and spot | gateway | `/ws` | Venue streams |
| Account state and history | gateway | `/graphql`, `/v1/users`, `/v1/ws`, `/v2` | Data hooks REST, GraphQL operations |
| Block explorer pages and Blockscout reads (`EXPLORER_URL`, Hyperpax:src/core/perps/config.ts:52; Hyperpax:src/core/chain.ts:26; the routes of Hyperpax:src/core/paxscan/api/resources.ts:54-144 on `PAXSCAN_API_URL`, Hyperpax:src/core/paxscan/config.ts:1) | paxscan.io | chain.ts and NEXT_PUBLIC_PAXSCAN_API_URL | unchanged; `EXPLORER_URL` reads `paxeerChain.blockExplorers` instead of a second literal |
| JSON-RPC (Hyperpax:src/core/rpc.ts:22, transports at :96; Hyperpax:src/core/chain.ts:22; `EMBEDDED_RPC_URL` at Hyperpax:src/core/wagmi.ts:24; the public client of the block stream, Hyperpax:src/hooks/chain/useEvmBlockStream.ts:3) | the node | rpc.ts | unchanged |
| Presence (`/api/presence`, Hyperpax:src/hooks/presence/usePresence.ts:62, :104) | the application's own route | relative path | unchanged |

Rules:
1. The legacy indexer REST order book poll (Hyperpax:src/widgets/trading/Orderbook.tsx:143-148, :211; Hyperpax:src/widgets/mobile/MobileTradeView.tsx:122) and its fallback host are removed: the Chainflow reference book covers every listed symbol (`SUPPORTED_WS_SYMBOLS`, Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:60-64), and the venue book comes from the gateway.
2. `INDEXER_BASE_URL`, `REST_API_URL` and `WS_URL` (Hyperpax:src/core/perps/config.ts:23-25, :33, :34-36) are removed; their readers move to the constants of this table.
3. The gateway venue routes /api/orderbook/<SYMBOL>, /api/open-interest and /api/ticker/<SYMBOL> (contracts/data-services.md section 24): the application reads /api/orderbook/<SYMBOL> for the resync of the venue book and reads no other venue REST route.

## Data hooks REST

The gateway origin comes from `NEXT_PUBLIC_PERPS_GATEWAY_URL`. `src/core/network/gateway.ts` (planned path) exports `gatewayUrls()` and every constant below is derived from it; an absent or non-http(s) value throws at module load with the variable name.

| Constant (today) | Use | v6.12.0 |
|---|---|---|
| `SPOT_API_BASE` (Hyperpax:src/core/spot/api.ts:14-16) | `/v1/users/:address/{holdings,positions,orders,trades,trades/stats,closed-legs,pnl/summary,pnl,identity}` and `/v1/leaderboards/:kind` (Hyperpax:src/core/spot/api.ts:283-361) | the bare gateway origin, same paths (contracts/data-services.md section 25) |
| `PERFDB_URL` (Hyperpax:src/core/perfdb/client.ts:12-14) | `/graphql` (:55, :71) and the REST routes of the next table through `perfdbRest` and `perfdbRestAuth` (:86-110) | the bare gateway origin; `PERFDB_GRAPHQL` (:117-124) is derived from it as origin + /graphql, never the reverse |
| venue book resync | `/api/orderbook/<SYMBOL>?depth=N` | gateway origin + /api (contracts/data-services.md section 24) |

PerfDB REST routes, served by contracts/data-services.md section 26:

| Route | Caller |
|---|---|
| POST /v2/auth/challenge, POST /v2/auth/verify, POST /v2/auth/refresh | Hyperpax:src/hooks/perps/usePerfdbAuth.ts:168, :175, :201 |
| POST /v2/alerts, DELETE /v2/alerts/<id> | Hyperpax:src/hooks/perps/usePerfdbAlerts.ts:138, :154-155 |
| POST /v2/referrals/generate, POST /v2/referrals/apply | Hyperpax:src/hooks/perps/usePerfdbReferrals.ts:178-179, :194 |
| POST /v2/competitions/<id>/join | Hyperpax:src/hooks/perps/usePerfdbCompetitions.ts:245-246 |
| GET /v2/user/<address>/daily-pnl | Hyperpax:src/hooks/perps/useDailyPnl.ts:67-68 |
| GET /v2/leaderboard | Hyperpax:src/page-modules/leaderboard/index.tsx:84-85 |

## Venue streams

The gateway speaks the shared WebSocket protocol of contracts/data-services.md section 8 on one socket at `/ws`.

Client frames:
- `{event: "subscribe", topic}`
- `{event: "unsubscribe", topic}`
- `{event: "ping"}`

Server envelope: `{topic, ts, snapshotId, seq, snapshot, data}`.

| Stream | Topics on gateway `/ws` | Reader |
|---|---|---|
| Perpetuals venue book | `<SYMBOL>@perp_orderbook` | the order book widget (Hyperpax:src/widgets/trading/Orderbook.tsx) and the mobile book (Hyperpax:src/widgets/mobile/MobileTradeView.tsx) through `src/core/net/venueStream.ts` (planned path) |
| Perpetuals venue fills | `<SYMBOL>@perp_trade` | `RecentTradesPanel` (Hyperpax:src/widgets/trading/Orderbook.tsx:491-612), which reads `useSidPaxTrades` (Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:519) today |
| Spot venue book and fills | `<SYMBOL>@orderbook`, `<SYMBOL>@trade` | Hyperpax:src/widgets/spot/SpotOrderbook.tsx:4 and Hyperpax:src/widgets/spot/SpotTradesTape.tsx:5, which read `useSpotOrderbook` and `useSpotTrades` of Hyperpax:src/hooks/spot/useSpotMarketStream.ts today |

| Stream | Socket | Reader |
|---|---|---|
| Spot account state | gateway `/v1/ws`, streams `trade`, `position`, `pnl`, `order`, `closed-leg` per address (contracts/data-services.md section 25) | `SPOT_USER_WS_URL` (Hyperpax:src/hooks/spot/useSpotUserStream.ts:32-34) |
| Perpetuals account state | gateway GraphQL subscriptions (GraphQL operations) | Hyperpax:src/hooks/perps/useIndexerSubscriptions.ts |

Resume rules:
1. Every stream passes each frame through `sequenceGuard` (Hyperpax:src/core/net/realtime.ts:29-47). A snapshot frame resets the per-topic baseline. A delta with seq greater than last + 1, or with a new snapshotId, returns false (:42).
2. On false, the stream closes the socket with code 4000 (the pattern at Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:294), fetches the REST snapshot (/api/orderbook/<SYMBOL> for a book), reconnects and resubscribes. Deltas with seq at or below the snapshot's are dropped.
3. A changed snapshotId drops local state for every topic of that socket and resubscribes.
4. Reconnect uses `scheduleActive` (Hyperpax:src/core/net/realtime.ts:8-22) with the retry-after and 1013 handling of `watchSocket` (:84-85).
5. Required change: the order book sends `{action: "subscribe", symbol, channel: "orderbook"}` on the indexer socket (Hyperpax:src/widgets/trading/Orderbook.tsx:217-235) and applies no sequence check. That socket is removed; the venue book subscribes `{event: "subscribe", topic: "<SYMBOL>@perp_orderbook"}` on the gateway and passes frames through `sequenceGuard`.
6. All gateway topics of one origin share one socket. While the gateway socket is not open, and for a symbol the venue does not list, the widget shows the Chainflow reference book of Data surfaces.

## WebSocket resume

The Chainflow sockets of Data surfaces keep their frames and paths and also pass every frame through `sequenceGuard` with the 4000 close of Venue streams rule 2; their URLs derive from FEED_ORIGIN. The gateway sockets follow Venue streams. No socket URL is a literal outside the constant modules of Data surfaces.

## GraphQL operations

The gateway GraphQL endpoint serves the schema of Hyperpax:src/core/perps/perpetual.graphql (`type Query` at :569, root fields :571-832) and the subscriptions the application issues. `INDEXER_GRAPHQL` (Hyperpax:src/core/perps/config.ts:47-50) points HTTP at gateway `/graphql` and its `wsUrl` at the gateway's graphql-transport-ws path; `GRAPHQL_URL`, `SUBGRAPH_PROXY_BASE` and `SUBGRAPH_WS_URL` (:28-30) derive from the same origin. The client keeps POST queries (Hyperpax:src/core/graphql/client.ts:92-93), the `graphql-transport-ws` subprotocol (:336), `connection_init` (:347) and `subscribe` (:366).

Query operations, 51 in Hyperpax:src/hooks/perps/useSubgraph.ts with their line numbers:
- GetPositions :23, GetClosedPositions :33, GetTrades :43, GetOrders :52;
- GetMarketInfo :61, GetMarkets :74, GetGlobalStats :84, GetUserStats :92, GetUserVault :101;
- GetFilledOrders :109, GetAllOrders :118, GetLiquidations :127, GetLatestPrices :135;
- GetVaultEvents :143, GetAccountLedger :153, GetPositionLedger :161, GetDelegates :169;
- GetTradingAccountEvents :177, GetKeeperCycles :185, GetTPSLOrders :194;
- DashboardV2 :205, GetAllOrdersV2 :227, GetLeaderboard :236;
- GetFees :247, GetTradeSettlements :256, GetOiSnapshots :266;
- GetMarkPriceHistory :274, GetVaultBalanceHistory :282, GetFundingPayments :291;
- GetMarketSnapshots :300, GetProtocolSnapshots :309;
- GetEnrichedGlobalStats :320, GetEnrichedMarketStats :334;
- GetOrderlyTickers :362, GetOrderlyFundingRates :372, GetOrderlyPriceChanges :381, GetOrderlyOpenInterests :390;
- GetSpotMarkets :400, GetSpotMarket :410, GetSpotOrders :420, GetSpotOrder :431, GetSpotOrderFills :446;
- GetSpotBatchClearings :455, GetSpotEpochSettlements :464, GetSpotFastSettlements :473;
- GetSpotCollateralMovements :482, GetSpotMarketModeChanges :491, GetSpotPofqUpdates :500;
- GetSpotPLVs :509, GetSpotKeeperCycles :519, GetSpotBatchClearFailures :528.

Subscription operations:
- indexerStatusUpdated (Hyperpax:src/hooks/perps/useIndexerSubscriptions.ts:79-80);
- latestPricesUpdated (:107-108);
- PriceUpdated / priceUpdated (:134-135);
- PositionChanged / positionChanged (:163-164; also Hyperpax:src/hooks/perps/usePortfolioStats.ts:176-177);
- OrderChanged / orderChanged (:196-197; also Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:798);
- TradeCreated / tradeCreated (:227-228);
- Liquidations / liquidationCreated (:258-259).

Rules:
1. Operation names, variables and selected fields are unchanged.
2. A query that selects a field the gateway schema lacks fails the operation golden test, not the page.
3. GetLeaderboard selects a root field that is absent from the `type Query` of perpetual.graphql. The gateway schema of contracts/data-services.md supplies it.
4. Required change: the non-null fields of perpetual.graphql that the gateway has no source for become nullable in the application schema, and every reader handles null: PoolState.baseReserve and quoteReserve; MarkPriceRecord.baseReserve and quoteReserve; SpotMarket.marketId, baseToken, quoteToken, mode and status; SpotOrder.side, offsetBps, size, filledSize and marketId; SpotOrderFill.fillScore; LatestPrice.onchainTimestamp; Market.maxLeverage; Trade.isLong; Trade.tradeType, which stays String! and carries the lowercase names that contracts/data-services.md section 13 maps from the contracts/perps.md numbering (open, close, liquidation, reduce, increase, adl, backstop); and the FeeConfig fields. The schema of contracts/data-services.md section 13 is the authority for every other field.

## PerfDB operations

The PerfDB operations are served by the same gateway GraphQL endpoint through `PERFDB_GRAPHQL` (Hyperpax:src/core/perfdb/client.ts:117-124), as contracts/data-services.md section 27 lists them.

Queries:
- User (Hyperpax:src/hooks/perps/usePerfdbUser.ts:17);
- UserBalances (Hyperpax:src/hooks/perps/usePerfdbBalances.ts:18);
- UserDailyPnl (Hyperpax:src/hooks/perps/useDailyPnl.ts:16);
- UserStats (Hyperpax:src/hooks/perps/usePerfdbTraderStats.ts:21);
- UserAlerts (Hyperpax:src/hooks/perps/usePerfdbAlerts.ts:37);
- UserReferrals (Hyperpax:src/hooks/perps/usePerfdbReferrals.ts:57);
- ReferralTiers (:75);
- Competitions (Hyperpax:src/hooks/perps/usePerfdbCompetitions.ts:64);
- Competition (:79).

Subscriptions:
- PriceUpdated (Hyperpax:src/hooks/perps/usePerfdbSubscriptions.ts:35);
- TradeExecuted (:68).

## Embedded wallet

Signing is unchanged. The embedded wallet keeps:
- `getWallet` (Hyperpax:src/core/wallet/embedded/sdk/index.ts:105);
- `signTransaction` (:118);
- `sendTransaction` (:123);
- `signMessage` (:128);
- the EIP-1193 bridge for eth_sendTransaction (Hyperpax:src/core/wallet/embedded/eip1193.ts:224), eth_signTransaction (:239), personal_sign (:254) and eth_sign (:261);
- the refusal of typed-data signing (:268-275).

Changes:
1. Chain id: the bridge and the configuration read the id from `paxeerChain` (Hyperpax:src/core/chain.ts:16-17). The literal at Hyperpax:src/core/wallet/embedded/client.ts:41 is removed, and the value stays 125.
2. Addresses: transactions target the addresses of the address table and the token entries, and nothing else changes in the payload.
3. Session detection:
   - The auth client is created with flowType "pkce", in place of the default flow left by the options at Hyperpax:src/core/wallet/embedded/sdk/index.ts:47-53.
   - URL session detection runs only on the callback route: detectSessionInUrl is false in the client (:51), and Hyperpax:src/pages/auth/callback.tsx calls the code exchange explicitly.
   - A URL fragment carrying an access token on any route, the callback included, creates no session.

## Deploy configuration

Every NEXT_PUBLIC_* variable the application reads is declared in Hyperpax:src/env/.env.template and in the deploy files that carry build variables: Hyperpax:deploy/.env.production.example (:42-46 today), Hyperpax:deploy/docker-compose.yml (:26-44), the ARG and ENV lists of Hyperpax:deploy/Dockerfile (:64-79, :80-92) and the variable list of Hyperpax:deploy/README.md (:120-122). Hyperpax:railway.json and Hyperpax:nixpacks.toml carry no variable list, and the env block of Hyperpax:ecosystem.config.js (:41-48) carries runtime values only, so the Railway service takes the variables deploy/README.md lists.

| Variable | Use |
|---|---|
| NEXT_PUBLIC_PERPS_GATEWAY_URL | the gateway origin of Data hooks REST and Venue streams |
| NEXT_PUBLIC_MARKET_PROXY_HOST | FEED_ORIGIN, the Chainflow origin (Hyperpax:src/core/feeds.ts:2) |
| NEXT_PUBLIC_PERPS_PRECOMPILE, NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE, NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE, NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE, NEXT_PUBLIC_PECOR_STOP_PRECOMPILE, NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE, NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE | optional address overrides for a local node |

Rules:
1. The removed variables leave every file:
   - DIAMOND_ADDRESS (Hyperpax:src/env/.env.template:26, Hyperpax:src/env/.env.node:19, Hyperpax:src/env/.env.remix:19) and NEXT_PUBLIC_DIAMOND_ADDRESS (Hyperpax:src/env/.env.local.nextjs:19);
   - USID_ADDRESS, USDC_ADDRESS and USDT_ADDRESS (Hyperpax:src/env/.env.template:29-31); USDL_ADDRESS (:32) carries 0x85FcD13735F4309833A503EE804ea32395851479;
   - NEXT_PUBLIC_INDEXER_URL, NEXT_PUBLIC_INDEXER_WS_URL and NEXT_PUBLIC_PERFDB_URL (Hyperpax:deploy/Dockerfile:68-70, :84-86; Hyperpax:deploy/docker-compose.yml:33-35; Hyperpax:deploy/.env.production.example:42-46; Hyperpax:deploy/README.md:121) and their readers (Hyperpax:src/core/perps/config.ts:23-25, :34-36; Hyperpax:src/core/perfdb/client.ts:12-14);
   - NEXT_PUBLIC_SPOT_API_BASE (Hyperpax:src/core/spot/api.ts:14-16), NEXT_PUBLIC_SPOT_USER_WS (Hyperpax:src/hooks/spot/useSpotUserStream.ts:32-34) and the NEXT_PUBLIC_SPOT_* address overrides (Hyperpax:src/core/spot/addresses.ts:31-60).
2. Hyperpax:src/env/deployed.json, named only by comments of Hyperpax:src/core/perps/config.ts:89-90, :119, carries the Diamond deployment and is deleted with the tables of Spot tokens and markets.
3. A build without NEXT_PUBLIC_PERPS_GATEWAY_URL fails at module load with the variable name.

## Errors

The precompiles revert with typed custom errors. The ABIs carry them (ABI modules rules 1 and 4), and the error classifier (`classifyError`, Hyperpax:src/core/errors/classify.ts:266) decodes the custom error from the revert data with `decodeErrorResult` over the perps and spot ABIs, ahead of the string rules at :239-255:

| Custom error | Source | Code |
|---|---|---|
| InsufficientOutput | contracts/spot.md; Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:78 | TRADE_SLIPPAGE_EXCEEDED |
| `InsufficientLiquidity` | contracts/spot.md; Hyperpax:src/core/network/spot_abi/PECOROrders.ts:949 | `TRADE_INSUFFICIENT_LIQUIDITY` |
| Expired | contracts/spot.md; Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:77 | RPC_REVERT, with the deadline message |
| StalePrice | contracts/spot.md | RPC_REVERT, with the stale price message |
| SameToken, ZeroAmount, UnsupportedTransfer, UnsupportedInputTransfer | contracts/spot.md | RPC_REVERT, with the input message |
| NativeTransferFailed | contracts/spot.md | RPC_REVERT |
| PriceAboveTarget / PriceBelowTarget | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1036, :1041 | TRADE_SLIPPAGE_EXCEEDED |
| LimitMustBeAboveStopBuy / LimitMustBeBelowStopSell | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1309, :1314 | TRADE_TP_SL_INVALID |
| each error of contracts/perps.md#19-errors | contracts/perps.md section 19 | the code given for it by the PRECOMPILE_ERROR_CODES table of the classifier, one code per error, with the price-bound and deadline errors mapped to TRADE_SLIPPAGE_EXCEEDED, the take-profit / stop-loss errors to TRADE_TP_SL_INVALID, the margin errors to the margin code and the rest to RPC_REVERT with the error name |

Rules:
1. An undecodable revert keeps today's RPC_REVERT path (Hyperpax:src/core/errors/classify.ts:153-160).
2. The classifier test encodes every error of both lists with `encodeErrorResult` and asserts its code, so an error added to a contract without an application entry fails the test.

## End-to-end test

`tests/e2e-local-node.test.ts` (planned path) runs with `node --test --import tsx` against a local node running v6.12.0 and the gateway. It reads LOCAL_NODE_RPC, LOCAL_NODE_KEY (a funded test key) and NEXT_PUBLIC_PERPS_GATEWAY_URL from the environment. It never skips: an unset variable fails the test with the variable name.

On the real precompiles, through the call builders and hooks, it runs these steps:
1. approve exactly 100 USDL (100000000 base units) to 0x1015 on the USDL token, deposit them and read getAccount(account).collateral back;
2. open a 20x market long on market 0 with TP and SL through placeOrder;
3. read getPositionDetails back and assert its take-profit and stop-loss;
4. place and cancel a limit order;
5. add collateral of 10 USDL (10000000 base units) after the exact allowance step;
6. partially close half the size with closePositionWithParams;
7. close the rest;
8. read a funding payment through the gateway GetFundingPayments operation (Hyperpax:src/hooks/perps/useSubgraph.ts:291);
9. withdraw getAccount(account).withdrawable;
10. assert that `SPOT_TOKENS.PAX.address` equals `WETH()` of 0x1024 (the uhpx pointer), that `decimals()` of the PAX and SID pointers is 6, and quote a 25 USDL market buy of PAX at 0x1024.

The test asserts every receipt status 1 and the USDL balance arithmetic to the base unit, net of the fees reported in the receipts' events.

## Invariants

1. No call site in `src/` names an address other than those of the address table or a token entry.
2. No on-chain amount in `src/widgets` or `src/hooks/perps` is produced by `parseEther` except leverage and 18-decimal prices.
3. No approval requests more than the amount of the call it precedes, and no approval is requested for a native coin.
4. Every perpetuals write that can fill at a market price carries acceptablePrice and deadline.
5. Every WebSocket stream applies `sequenceGuard` and resynchronises from REST on a gap.
6. The GraphQL operation names of the application are a subset of the gateway's operation set.
7. No module outside the constant modules of Data surfaces names a data or chain endpoint host.
8. No token entry, address constant, import or wrap path for WPAX9, USDC, USDT, WETH or WBNB, no transferMargin caller outside the generated ABI and no Diamond address remains in `src/core`, `src/hooks`, `src/widgets`, `src/app`, `src/pages` or `src/page-modules`. The Chainflow symbol form `PERP_<SYMBOL>_USDC` (Hyperpax:src/core/feeds.ts:6) and the `WETH()` view of the spot ABIs (Hyperpax:src/core/network/spot_abi/PECOR.ts:116), which returns the uhpx pointer, are not token entries.
