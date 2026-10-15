# Application contract: the Hyperpax application on the v6.12.0 precompiles

Version 1.0.0. This file binds the Hyperpax application (github.com/paxlabs-inc/Hyperpax, commit 242152b) to the precompiles and data services of release v6.12.0 of the Paxeer X Network. It lists every contract call site and the precompile address and method it resolves to, every data hook and the gateway route, topic or operation it reads, and the corrected amount, approval, time-in-force and take-profit / stop-loss semantics.

Citations use `Hyperpax:path:line` at 242152b, `Launch-x:path:line` at c5eb7c7 and plain `path:line` for this repository at 211acbc6c. A "required change" names the current code and the target behaviour.

Interface authorities:
- The method signatures, enumerations, events and custom errors of the perpetuals precompile are those of contracts/perps.md.
- The spot precompile set follows contracts/spot.md.
- The oracle precompile follows contracts/pricefeed.md.
- The gateway routes, topics and GraphQL schema follow contracts/data-services.md.
- Addresses come from contracts/address-table.md.

This file states what the application sends and reads. It does not redefine those interfaces.

## Address table

The application resolves every chain call through one module, `src/core/network/precompiles.ts` (planned path).

| Role | Address | Replaces in the application |
|---|---|---|
| Perpetuals precompile | 0x1015 | `DIAMOND_ADDRESS` at Hyperpax:src/core/perps/config.ts:10-11, plus every per-user TradingAccount vault address from `getVaultAddress` (Hyperpax:src/hooks/perps/useVault.ts:8-15) |
| Spot PECOR | 0x1021 | `PECOR_CONTRACTS.PECOR` (Hyperpax:src/core/spot/addresses.ts:32-35) |
| Spot PECOROrders | 0x1022 | `PECOR_CONTRACTS.PECOROrders` (Hyperpax:src/core/spot/addresses.ts:40-43) |
| Spot PECORStopOrders | 0x1023 | `PECOR_CONTRACTS.PECORStopOrders` (Hyperpax:src/core/spot/addresses.ts:44-47) |
| Spot PECORQuoter | 0x1024 | `PECOR_CONTRACTS.PECORQuoter` (Hyperpax:src/core/spot/addresses.ts:36-39) |
| Spot PECORVault | 0x1025 | `PECOR_CONTRACTS.PECORVault` (Hyperpax:src/core/spot/addresses.ts:48-51) |
| Oracle precompile | 0x1020 | `PECOR_CONTRACTS.PriceOracle` (Hyperpax:src/core/spot/addresses.ts:56-59) |
| Launchpad precompile | 0x1017 | not called by the application |

Rules:
1. Each address is the 20-byte form of the short address (0x1015 is 0x0000000000000000000000000000000000001015) and is exported as a viem `Address`.
2. The environment override pattern of `envAddress` (Hyperpax:src/core/spot/addresses.ts:23-27) is kept, so a local node can supply its own addresses. A value that is not a 20-byte hex address is refused at module load.
3. `PECOR_CONTRACTS.TransactionTracker` (Hyperpax:src/core/spot/addresses.ts:52-55) has no precompile and no caller outside the address table. It is removed.
4. PECORVaultShare (0x1026) and PECORRouter (0x1027) have no caller in the application. They appear in the table only for reads by other services.
5. USDL is one token at one address: the six-decimal USDL token of contracts/libraries/Constants.sol:14-16. Required change: the perpetuals collateral entry (Hyperpax:src/core/perps/config.ts:75-79) and the spot USDL entry (Hyperpax:src/core/spot/addresses.ts:146-153) carry different addresses today. Both read the one USDL address from the address module.
6. The chain stays chain id 125 (Hyperpax:src/core/chain.ts:16-17). The embedded wallet reads its chain id from that object, not from a second literal (Hyperpax:src/core/wallet/embedded/client.ts:41).

## ABI modules

Two modules carry the precompile ABIs:
- `src/core/network/abi/PerpsPrecompile.ts` (planned path) is generated from contracts/perps.md.
- The spot ABI files under `src/core/network/spot_abi/` are regenerated from contracts/spot.md.

Both use the `as const` export shape of the current generated files (Hyperpax:src/core/network/abi/PositionFacet.ts:1-2), so viem infers argument and return types.

Rules:
1. The perps module exports one ABI holding every method the application calls (listed in Perpetual call sites), plus the `TimeInForce` enumeration, every event and every custom error of contracts/perps.md.
2. The Diamond facet ABIs the application imports today are replaced by that module: PositionFacet, OrderBookFacet, QuoterFacet, TradingAccount and VaultFactoryFacet (barrel at Hyperpax:src/core/network/abi/index.ts:1-27).
3. The spot ABI files keep the selectors and return tuples the application encodes today (for example `marketBuy` at Hyperpax:src/core/network/spot_abi/PECOR.ts:313 and `placeLimitBuy` at Hyperpax:src/core/network/spot_abi/PECOROrders.ts:526). The regenerated files must reproduce each selector. Any difference fails the ABI golden test.
4. `readContract` casts its params `as any` today (Hyperpax:src/hooks/perps/useContract.ts:8-16). Required change: the precompile call builders pass typed ABIs, so a signature drift between contracts/perps.md and the application fails `pnpm tsc --noEmit`.

## Perpetual call sites

Every perpetuals call goes to 0x1015, built by the pure call builders in `src/core/perps/calls.ts` (planned path). The hooks submit what the builders return through `writeContract` (Hyperpax:src/hooks/perps/useContract.ts:49-101). `writeContract` simulates first (Hyperpax:src/hooks/perps/useContract.ts:73-80) and then signs (Hyperpax:src/hooks/perps/useContract.ts:90-100).

| Hook function (today) | Today's target | v6.12.0 method at 0x1015 | Fields added by this release |
|---|---|---|---|
| `openPosition` (Hyperpax:src/hooks/perps/usePosition.ts:63-64) | Diamond PositionFacet (Hyperpax:src/core/network/abi/PositionFacet.ts:765) | openPosition | acceptablePrice, deadline, takeProfitPrice, stopLossPrice |
| `addSize` (Hyperpax:src/hooks/perps/usePosition.ts:121-122) | Diamond (PositionFacet.ts:556) | addSize | acceptablePrice, deadline |
| `closePosition` (Hyperpax:src/hooks/perps/usePosition.ts:79-80) | Diamond (PositionFacet.ts:569) | closePosition | acceptablePrice, deadline |
| `partialClose` (Hyperpax:src/hooks/perps/usePosition.ts:89-90) | Diamond (PositionFacet.ts:789) | partialClose | acceptablePrice, deadline |
| `addCollateral` (Hyperpax:src/hooks/perps/usePosition.ts:100-101) | Diamond (PositionFacet.ts:533) | addCollateral | none |
| `removeCollateral` (Hyperpax:src/hooks/perps/usePosition.ts:110-111) | Diamond (PositionFacet.ts:807) | removeCollateral | none |
| `getPosition` (Hyperpax:src/hooks/perps/usePosition.ts:29-30) | Diamond (PositionFacet.ts:638) | getPosition | none |
| `getUserPositionIds` (Hyperpax:src/hooks/perps/usePosition.ts:38-39) | Diamond (PositionFacet.ts:726) | getUserPositionIds | none |
| `getOpenInterest` (Hyperpax:src/hooks/perps/usePosition.ts:47-48) | Diamond (PositionFacet.ts:595) | getOpenInterest | none |
| `placeLimitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:36-40) | Diamond OrderBookFacet (Hyperpax:src/core/network/abi/OrderBookFacet.ts:709) | placeLimitOrder | timeInForce, reduceOnly, takeProfitPrice, stopLossPrice |
| `placeStopLimitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:58-59) | Diamond (OrderBookFacet.ts:763) | placeStopLimitOrder | timeInForce, reduceOnly, takeProfitPrice, stopLossPrice |
| `placeTakeProfitOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:81-82) | Diamond (OrderBookFacet.ts:811) | placeTakeProfitOrder | none |
| `placeStopLossOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:91-92) | Diamond (OrderBookFacet.ts:787) | placeStopLossOrder | none |
| `cancelOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:71-72) | Diamond (OrderBookFacet.ts:559) | cancelOrder | none |
| `getOrder` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:10-11) | Diamond (OrderBookFacet.ts:591) | getOrder | none |
| `getUserOrderIds` (Hyperpax:src/hooks/perps/usePerpsOrders.ts:19-20) | Diamond (OrderBookFacet.ts:660) | getUserOrderIds | none |
| `quoteMarket` (Hyperpax:src/hooks/perps/useQuoter.ts:116-122) | Diamond QuoterFacet (Hyperpax:src/core/network/abi/QuoterFacet.ts:67) | quoteMarket | none |
| `quoteOpenPosition` (Hyperpax:src/hooks/perps/useQuoter.ts:126-140) | Diamond (QuoterFacet.ts:163) | quoteOpenPosition | none |
| `quoteClosePosition` (Hyperpax:src/hooks/perps/useQuoter.ts:142-150) | Diamond (QuoterFacet.ts:11) | quoteClosePosition | none |
| none today | Diamond (QuoterFacet.ts:239) | quotePartialClose | used by the partial-close control |

Rules:
1. `getPositionCollateralToken` reads getPosition output index 5 (Hyperpax:src/hooks/perps/usePosition.ts:69-73). It reads the field by name from the regenerated ABI instead of by index.
2. Required change: partialClose has no caller in the interface. The positions panel adds a partial-close action beside `handleClose` (Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:857-876). It quotes through quotePartialClose and then submits partialClose with a price bound.
3. Every write keeps the fee clamp of `estimateClampedFees` (Hyperpax:src/hooks/perps/useContract.ts:22-47) and the receipt wait of `waitForTx` (Hyperpax:src/hooks/perps/useContract.ts:146-175).

## Account and collateral

Margin is cross margin in USDL, held by the perpetuals precompile per account (head decision.4). The per-user TradingAccount vault contract of today is not deployed against v6.12.0. Its calls move to the account methods of 0x1015 named in contracts/perps.md:

| Hook function (today) | Today's target | v6.12.0 |
|---|---|---|
| `getVaultAddress` / `createVault` (Hyperpax:src/hooks/perps/useVault.ts:8-24) | Diamond VaultFactoryFacet (Hyperpax:src/core/network/abi/VaultFactoryFacet.ts:43, :75) | Removed. The account exists implicitly at 0x1015. The create step of `DepositWithdrawModal` (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:233) is removed. |
| `deposit` (Hyperpax:src/hooks/perps/useVault.ts:44-52) | per-user vault, TradingAccount (Hyperpax:src/core/network/abi/TradingAccount.ts:281) | deposit at 0x1015 |
| `withdraw` (Hyperpax:src/hooks/perps/useVault.ts:54-62) | per-user vault (TradingAccount.ts:1034) | withdraw at 0x1015 |
| `getBalance` / `getLockedBalance` (Hyperpax:src/hooks/perps/useVault.ts:26-42; multicall at Hyperpax:src/hooks/perps/usePortfolioStats.ts:154-155) | per-user vault (TradingAccount.ts:388, :581) | getBalance / getLockedBalance at 0x1015 for the connected account |
| `useTopUpPosition` (Hyperpax:src/hooks/perps/useMarginMode.ts:147-178) | per-user vault (TradingAccount.ts:957) | topUpPosition at 0x1015 |
| `useTransferMargin` (Hyperpax:src/hooks/perps/useMarginMode.ts:180-211) | per-user vault (TradingAccount.ts:980) | transferMargin at 0x1015 |
| `useAccountSummary` (Hyperpax:src/hooks/perps/useMarginMode.ts:213-264) | per-user vault (TradingAccount.ts:320) | getAccountSummary at 0x1015 |
| `usePositionMargin` (Hyperpax:src/hooks/perps/useMarginMode.ts:96-145) | per-user vault (TradingAccount.ts:613) | getPositionMargin at 0x1015 |
| `useMarginMode` / `useSetMarginMode` (Hyperpax:src/hooks/perps/useMarginMode.ts:15-94) | per-user vault (TradingAccount.ts:594, :939) | Removed. The mode is cross. `MARGIN_MODE_LABELS` (Hyperpax:src/core/perps/config.ts:286-289) shows the cross label, and the toggle (Hyperpax:src/widgets/trading/modals/MarginModeToggle.tsx:38, :53; mounted at Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:1008) is removed. |
| `useEmergencyWithdraw` (Hyperpax:src/hooks/perps/useMarginMode.ts:266-297) | per-user vault (TradingAccount.ts:307) | Removed. Withdrawal of the available balance uses withdraw. |

Rules:
1. Collateral is USDL only. `COLLATERAL_TOKENS` (Hyperpax:src/core/perps/config.ts:64-80) and `COLLATERAL_OPTIONS` (Hyperpax:src/core/perps/config.ts:82-87) list the single USDL entry.
2. Deposit and withdrawal amounts are USDL base units (six decimals).

## Spot call sites

Every spot call keeps its method and argument list and changes only the address it resolves to.

| Function (Hyperpax:src/hooks/spot/useSpotOrders.ts) | Method | Address | Approval spender |
|---|---|---|---|
| `marketBuy` :129-172 (write :163-168) | marketBuy | 0x1021 | 0x1021 (:151-156) |
| `marketSell` :197-261 (write :253-258) | marketSell | 0x1021 | 0x1021 |
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
| liquidity call at :246 | getLiquidityInfo | 0x1024 |
| liquidity call at :264 | getAllLiquidityInfo | 0x1024 |
| TWAP call at :286 | getTWAP | 0x1024 |

Rules:
1. Native PAX is wrapped through WPAX9 `deposit` before a sell (Hyperpax:src/hooks/spot/useSpotOrders.ts:223-227). This path is unchanged; WPAX9 stays an ERC20 contract (Hyperpax:src/core/spot/addresses.ts:91).
2. Spot deadlines keep the semantics of the engine (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:132-135): a non-zero deadline in the past reverts Expired. The application always sends now plus `DEFAULT_MARKET_DEADLINE_SECONDS` (Hyperpax:src/core/spot/slippage.ts:27, `buildDeadline` at :75).
3. The spot route stays gated as it is today. `getServerSideProps` returns notFound (Hyperpax:src/pages/spot.tsx:2-3), asserted by Hyperpax:tests/audit-regressions.test.ts:7-11. The spot widgets are retargeted behind that gate, and opening the route is a cut-over step of the release runbook.

## Amounts

| Quantity | Units on the wire | Conversion |
|---|---|---|
| USDL collateral, deposit, withdrawal, top-up, transfer, add-collateral, remove-collateral, add-size collateral | 6-decimal base units | `safeParseUnits(value, 6)` (Hyperpax:src/core/perps/amounts.ts:26-38) |
| Prices: trigger, limit, take-profit, stop-loss, acceptable price | 18-decimal USD | `safeParseUnits(value, 18)`, as the form already does (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:335-348) |
| sizeUsd, closeSizeUsd | 18-decimal USD | `safeParseUnits(value, 18)` |
| Leverage | 18-decimal multiple, at least 1x | `safeParseUnits(value, 18)` with the sub-1x refusal at Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:327-333 |
| Spot token amounts | the token's own decimals | `parseTokenAmount` (Hyperpax:src/core/spot/slippage.ts:99) |

Required changes:
1. AdjustCollateralModal converts add-collateral, remove-collateral and the add-size collateral with `parseEther`, which is 18 decimals (Hyperpax:src/widgets/trading/modals/AdjustCollateralModal.tsx:68, :81, :86). They convert with six decimals. The leverage argument at :86 stays at 18 decimals.
2. TopUpPositionModal (Hyperpax:src/widgets/trading/modals/TopUpPositionModal.tsx:39) and TransferMarginModal (Hyperpax:src/widgets/trading/modals/TransferMarginModal.tsx:46) convert with six decimals.
3. DepositWithdrawModal builds the amount with floating point, `BigInt(Math.floor(num * 10 ** decimals))` (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:198). It uses `safeParseUnits` with the token's decimals.
4. `getCollateralDecimals` returns 18 for an unknown token (Hyperpax:src/core/perps/amounts.ts:40-45). It throws for an unknown token, so no amount is ever scaled by a guessed decimal count.
5. Spot stable amounts keep `decimalsStable = 6` only through the token table (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:418). The value is read from the market's quote token metadata.

## Approvals

| Rail | Token | Spender | Allowance requested |
|---|---|---|---|
| Perpetuals account deposit | USDL | 0x1015 | exactly the deposit amount |
| Perpetuals openPosition, addSize, addCollateral, placeLimitOrder, placeStopLimitOrder (when the precompile pulls collateral from the wallet as contracts/perps.md states) | USDL | 0x1015 | exactly the collateral amount of the call |
| Spot market rail | input token | 0x1021 | exactly the input amount |
| Spot limit rail | input token | 0x1022 | exactly the escrowed amount |
| Spot stop rail | input token | 0x1023 | exactly the escrowed amount |

Rules:
1. `ensureAllowance` treats an existing allowance at or above the amount as sufficient. Required change: today it also requires the allowance to be at least `MAX_UINT256 / 2` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:40, :139). That threshold is removed.
2. When the allowance is short, `ensureAllowance` approves exactly the amount through `approveExact` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:87-100), not `approveMax` (Hyperpax:src/hooks/spot/useSpotApprovals.ts:67-79, called at :143).
3. The perpetuals approval helper `ensureDiamondAllowance` (Hyperpax:src/hooks/perps/usePosition.ts:13-22) becomes a perpetuals-precompile allowance helper with spender 0x1015.
4. DepositWithdrawModal's own allowance check (Hyperpax:src/app/modals/DepositWithdrawModal.tsx:203-204) uses the same helper.

## Time in force

The perpetuals order form has an Order options popover (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1082-1088). Its toggles map to the order fields of contracts/perps.md:

| Form state (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx) | Sent as |
|---|---|
| `ioc` :292 | timeInForce = TimeInForce.IOC |
| `fok` :293 | timeInForce = TimeInForce.FOK |
| `postOnly` :291 | timeInForce = TimeInForce.PostOnly |
| none of the three | timeInForce = TimeInForce.GTC |
| `reduceOnly` :290 | reduceOnly = true |
| `hidden` :295 | Removed. The perpetuals venue has no hidden orders. |
| `orderConfirm` :294 | A confirmation step before signing that shows side, size, price bound, liquidation price and fees from the quote. |

Rules:
1. IOC, FOK and post-only are mutually exclusive. The IOC/FOK toggles already exclude each other (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1084-1085), and post-only additionally clears both.
2. A market order sends no time in force, since openPosition fills immediately or reverts. Post-only stays disabled for market orders (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:1083).
3. Spot limit orders with IOC or FOK use the market rail today, with a minimum output taken from the market quote (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:421-427). Required change: the minimum output is computed from the limit price with `expectedOutFromPrice` (Hyperpax:src/core/spot/slippage.ts:160). A buy then never pays above the limit and a sell never receives below it. The swap is all-or-nothing, so IOC and FOK both fill completely at or inside the limit or revert InsufficientOutput (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:78).
4. Spot post-only keeps forcing GTC (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:367). The spot reduce-only toggle (Hyperpax:src/widgets/spot/SpotOrderForm.tsx:207, :816) is removed, because the spot venue holds no positions.

## Take-profit and stop-loss

1. With TP/SL enabled (`tpslEnabled`, Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:234), the market branch sends `tpPrice` and `slPrice` (:235-236) as takeProfitPrice and stopLossPrice in the openPosition call. Today the call at :482 sends neither. An unset price is sent as 0, which means none.
2. The limit and stop-limit branches (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:486, :490) send the same two fields. The precompile attaches them when the order fills.
3. Validation before signing:
   - A long requires stopLossPrice < reference price < takeProfitPrice. A short requires the mirror.
   - The reference price is the quote entryPrice for market orders (QuoterFacet output at Hyperpax:src/core/network/abi/QuoterFacet.ts:163) and the trigger price for limit orders.
   - A failing pair blocks submit with the TRADE_TP_SL_INVALID message (Hyperpax:src/core/errors/classify.ts:251-253).
4. SetTpSlModal keeps placeTakeProfitOrder and placeStopLossOrder for an open position (Hyperpax:src/widgets/trading/modals/SetTpSlModal.tsx:84-91), with 18-decimal prices.
5. After a successful submit, the chart order lines from the form are cleared together with the inputs.

## Price bound and deadline

1. Market openPosition and addSize:
   - acceptablePrice = entryPrice * (10000 + bps) / 10000 for a long, and entryPrice * (10000 - bps) / 10000 for a short.
   - bps is `DEFAULT_SLIPPAGE_BPS` (Hyperpax:src/core/perps/config.ts:354), which nothing reads today.
2. closePosition and partialClose use the exitPrice of quoteClosePosition / quotePartialClose with the opposite rounding.
3. deadline = the latest block timestamp plus `DEFAULT_MARKET_DEADLINE_SECONDS` (Hyperpax:src/core/spot/slippage.ts:27), built with `buildDeadline` (:75).
4. The quote expiry check (`QUOTE_MAX_AGE_MS`, Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:178) refreshes the quote instead of blocking submit, so an aged quote is re-fetched and the bound recomputed.
5. Integer arithmetic only. Every bound is a bigint at 18 decimals.

## Order plan

`src/core/perps/orderPlan.ts` (planned path) is a pure function. It takes the form state and returns the ordered call list that `handleSubmit` (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:444-506) submits:
- market: one openPosition;
- limit: one placeLimitOrder;
- stop-limit: one placeStopLimitOrder;
- scaled: N placeLimitOrder calls.

Scaled order rules:
1. The ladder reads `scaledStartPrice`, `scaledEndPrice`, `scaledTotalOrders` and `scaledDistribution` (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:227-231).
2. The prices are N evenly spaced points from start to end inclusive.
3. The flat distribution splits sizeUsd equally. Ascending and descending weight rung i by i+1 and N-i respectively. The integer remainder goes to the last rung.
4. The custom distribution has no input field (Hyperpax:src/widgets/trading/PerpsOrderForm.tsx:29), so the plan returns no calls and the submit stays disabled.
5. 2 to 20 rungs are accepted.

## Data hooks REST

One gateway base URL replaces the indexer, proxy and spot API hosts. It comes from `NEXT_PUBLIC_PERPS_GATEWAY_URL`, and each constant below is derived from it:

| Constant (today) | Use | v6.12.0 |
|---|---|---|
| `INDEXER_BASE_URL` / `REST_API_URL` (Hyperpax:src/core/perps/config.ts:23-25, :33) | `/orderbook/:symbol?depth=` (Hyperpax:src/widgets/trading/Orderbook.tsx:148; Hyperpax:src/widgets/mobile/MobileTradeView.tsx:122), `/open-interest` (Hyperpax:src/hooks/perps/useQuoter.ts:54), `/ticker/:symbol` (Hyperpax:src/hooks/perps/useQuoter.ts:82) | gateway base + `/api`, same paths |
| `SPOT_API_BASE` (Hyperpax:src/core/spot/api.ts:14-16) | `/v1/users/:address/{holdings,positions,orders,trades,trades/stats,closed-legs,pnl/summary,pnl,identity}` and `/v1/leaderboards/:kind` (Hyperpax:src/core/spot/api.ts:283-361) | gateway base, same paths |
| `PERFDB_URL` (Hyperpax:src/core/perfdb/client.ts:12-14) | `/graphql`, `/v2/auth/challenge`, `/v2/auth/verify`, `/v2/auth/refresh` (Hyperpax:src/hooks/perps/usePerfdbAuth.ts:168, :175, :201) | gateway base, same paths |
| data protocol REST of Hyperpax:data_api.md (Symbol Service :92, Markets Service :415, Chain Explorer Service :551) | `/price`, `/stats`, `/perp_stats`, `/trades/:symbol`, `/perp_trades/:symbol`, UDF, `/markets`, `/markets/:symbol`, `/totals`, `/blocks`, `/transactions`, `/health` | gateway base, same paths, every symbol on one host |

Rules:
1. The chart candle feed keeps `FEED_ORIGIN` (Hyperpax:src/core/feeds.ts:2) and `candleFeedUrl` (:8-14). The candle proxy (Hyperpax:src/pages/api/candle/[...path].ts:5) is unchanged.
2. Required change: the quote fallback reads the on-chain `quoteMarket` only after the gateway ticker fails (Hyperpax:src/hooks/perps/useQuoter.ts:82, :116-122). The fallback's fixed `maintenanceMarginBps` (:109) is replaced by the value the precompile returns.

## WebSocket resume

The gateway speaks the shared WebSocket protocol of Hyperpax:data_api.md:24-90.

Client frames:
- `{event: "subscribe", topic}`
- `{event: "unsubscribe", topic}`
- `{event: "ping"}`

Server envelope: `{topic, ts, snapshotId, seq, snapshot, data}`.

| Stream | Topics | Socket constant (today) |
|---|---|---|
| Perpetual order book | `<SYMBOL>@perp_orderbook` | `WS_URL` (Hyperpax:src/core/perps/config.ts:34-36), used by Hyperpax:src/widgets/trading/Orderbook.tsx:217-222 |
| Symbol streams | `<SYMBOL>@orderbook`, `@trade`, `@stats`, `@kline_<interval>`, `@perp_trade`, `@perp_stats` | per-symbol URL (Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:70; Hyperpax:src/hooks/spot/useSpotMarketStream.ts:34) |
| Markets | `markets@all`, `markets@totals`, `<SYMBOL>@market` | `MARKETS_WS_URL` (Hyperpax:src/hooks/perps/useMarketsStream.ts:29) |
| Chain | `chain@blocks`, `chain@transactions` | `EXPLORER_WS_URL` (Hyperpax:src/hooks/chain/useExplorerStream.ts:29) |
| Spot user | `trade`, `position`, `pnl`, `order`, `closed-leg` per address | `SPOT_USER_WS_URL` (Hyperpax:src/hooks/spot/useSpotUserStream.ts:32-34) |

Resume rules:
1. Every stream passes each frame through `sequenceGuard` (Hyperpax:src/core/net/realtime.ts:29-47). A snapshot frame resets the per-topic baseline. A delta with seq greater than last + 1, or with a new snapshotId, returns false (:42).
2. On false, the stream closes the socket with code 4000 (the pattern at Hyperpax:src/hooks/perps/useSidPaxMarketStream.ts:294), fetches the matching REST snapshot, reconnects and resubscribes. Deltas with seq at or below the snapshot's are dropped.
3. A changed snapshotId drops local state for every topic of that socket and resubscribes (Hyperpax:data_api.md:70-82).
4. Reconnect uses `scheduleActive` (Hyperpax:src/core/net/realtime.ts:8-22) with the retry-after and 1013 handling of `watchSocket` (:84-85).
5. Required change: the perpetual order book sends `{action: "subscribe", symbol, channel: "orderbook"}` (Hyperpax:src/widgets/trading/Orderbook.tsx:231-235) and applies no sequence check. It sends `{event: "subscribe", topic: "<SYMBOL>@perp_orderbook"}` and passes frames through `sequenceGuard`.
6. All socket URLs derive from the gateway base, and every symbol is served on one socket.

## GraphQL operations

The gateway GraphQL endpoint serves the schema of Hyperpax:src/core/perps/perpetual.graphql (`type Query` at :569, root fields :571-832) and the subscriptions the application issues. `INDEXER_GRAPHQL` (Hyperpax:src/core/perps/config.ts:47-50) points HTTP at gateway `/graphql` and its `wsUrl` at the gateway's graphql-transport-ws path. The client keeps POST queries (Hyperpax:src/core/graphql/client.ts:92-93), the `graphql-transport-ws` subprotocol (:336), `connection_init` (:347) and `subscribe` (:366).

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
- PositionChanged / positionChanged (:163-164; also Hyperpax:src/hooks/perps/usePortfolioStats.ts:177);
- OrderChanged / orderChanged (:196-197; also Hyperpax:src/widgets/trading/PerpsPositionsPanel.tsx:798);
- TradeCreated / tradeCreated (:227-228);
- Liquidations / liquidationCreated (:258-259).

Rules:
1. Operation names, variables and selected fields are unchanged.
2. A query that selects a field the gateway schema lacks fails the operation golden test, not the page.
3. GetLeaderboard selects a root field that is absent from the `type Query` of perpetual.graphql. The gateway schema of contracts/data-services.md supplies it.

## PerfDB operations

The PerfDB operations are served by the same gateway GraphQL endpoint through `PERFDB_GRAPHQL` (Hyperpax:src/core/perfdb/client.ts:117-120).

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
2. Addresses: transactions target the addresses of the address table, and nothing else changes in the payload.
3. Session detection:
   - The auth client is created with flowType "pkce", in place of the default flow left by the options at Hyperpax:src/core/wallet/embedded/sdk/index.ts:47-53.
   - URL session detection runs only on the callback route: detectSessionInUrl is false in the client (:51), and Hyperpax:src/pages/auth/callback.tsx calls the code exchange explicitly.
   - A URL fragment carrying an access token on any route, the callback included, creates no session.

## Errors

The error classifier (`classifyError`, Hyperpax:src/core/errors/classify.ts:266) decodes custom error names from precompile revert data and maps them to the existing codes, ahead of the string rules at :239-255:

| Custom error | Source | Code |
|---|---|---|
| InsufficientOutput | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:78 | TRADE_SLIPPAGE_EXCEEDED |
| InsufficientLiquidity | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:949 | TRADE_INSUFFICIENT_LIQUIDITY |
| Expired | Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:77 | RPC_REVERT, with the deadline message |
| PriceAboveTarget / PriceBelowTarget | Hyperpax:src/core/network/spot_abi/PECOROrders.ts:1036, :1041 | TRADE_SLIPPAGE_EXCEEDED |
| LimitMustBeAboveStopBuy / LimitMustBeBelowStopSell | Hyperpax:src/core/network/spot_abi/PECORStopOrders.ts:1309, :1314 | TRADE_TP_SL_INVALID |
| each error of contracts/perps.md#errors | contracts/perps.md | the code named beside it in that section |

Rules:
1. An undecodable revert keeps today's RPC_REVERT path (Hyperpax:src/core/errors/classify.ts:153-160).

## End-to-end test

`tests/e2e-local-node.test.ts` (planned path) runs with `node --test --import tsx` against a local node running v6.12.0. It reads the node's JSON-RPC URL and a funded test key from the environment, and it skips with a message only when either is unset.

On the real precompiles, through the call builders and hooks, it runs these steps:
1. deposit 100 USDL (100000000 base units) after an exact approval to 0x1015;
2. open a 20x market long on market 0 with TP and SL;
3. read the position back and assert its take-profit and stop-loss;
4. place and cancel a limit order;
5. add collateral of 10 USDL (10000000 base units);
6. partially close half the size;
7. close the rest;
8. read a funding payment through the gateway;
9. withdraw the available balance.

The test asserts every receipt status 1 and the USDL balance arithmetic to the base unit, net of the fees reported in the receipts' events.

## Invariants

1. No call site in `src/` names an address other than those of the address table or an ERC20 token of the token tables.
2. No on-chain amount in `src/widgets` or `src/hooks/perps` is produced by `parseEther` except leverage and 18-decimal prices.
3. No approval requests more than the amount of the call it precedes.
4. Every perpetuals write that can fill at a market price carries acceptablePrice and deadline.
5. Every WebSocket stream applies `sequenceGuard` and resynchronises from REST on a gap.
6. The GraphQL operation names of the application are a subset of the gateway's operation set.
