# Perpetuals parameter sheet

The economic parameters of the perpetuals venue at 0x1015. They follow the structure of the Hyperliquid venue (decision.4) with the deviations named in each section. Every figure here is a default of `modules/perps/types/params.go` `DefaultParams()` or of the market configs written by `SeedV6120`; changes after activation are made by an upgrade handler through `Keeper.UpdateParams` and `Keeper.SetMarket`, both signed by the governance module account. Units: USD amounts and prices in 18-decimal fixed point, collateral in USDL units (6 decimals, the USDL ERC20 held by the perps module EVM address, contracts/perps.md section 2), rates in parts per million (ppm, 1 ppm = 0.0001 percent = 0.01 bp) unless a section says otherwise. Every intermediate product is computed in 256-bit integers and rounded as each formula states; the remainder of every rounding is booked to `residueUsd` at 0x1015 (contracts/perps.md, section 24).

## 1. Trading fees

Fee tier of an account = the highest tier whose threshold its 14-day perpetuals volume exceeds. Volume is kept in daily buckets keyed by the UTC day `d = floor(blockTime / 86400)`; the 14-day volume on day d is the sum of the buckets of days d-13 to d inclusive, so a fill counts toward the tier of the account's next order on the same day, and buckets older than d-13 are deleted when day d first writes. Both sides of a fill count the fill notional. Taker fee = fill notional x taker rate, rounded up to the USDL unit. Maker fee = fill notional x maker rate, rounded up; a negative maker rate is a rebate paid from the fee recipient account, rounded down.

| tier | 14-day volume (USD) | taker (bp) | taker (ppm) | maker (bp) | maker (ppm) |
|---|---|---|---|---|---|
| 0 | <= 5,000,000 | 4.5 | 450 | 1.5 | 150 |
| 1 | > 5,000,000 | 4.0 | 400 | 1.2 | 120 |
| 2 | > 25,000,000 | 3.5 | 350 | 0.8 | 80 |
| 3 | > 100,000,000 | 3.0 | 300 | 0.4 | 40 |
| 4 | > 500,000,000 | 2.8 | 280 | 0.0 | 0 |
| 5 | > 2,000,000,000 | 2.6 | 260 | 0.0 | 0 |
| 6 | > 7,000,000,000 | 2.4 | 240 | 0.0 | 0 |

Maker rebate tiers replace the maker rate when the account's 14-day maker volume is above a share of the venue's 14-day volume:

| rebate tier | maker share of venue volume | maker rate (bp) | maker rate (ppm) |
|---|---|---|---|
| 1 | > 0.5% | -0.1 | -10 |
| 2 | > 1.5% | -0.2 | -20 |
| 3 | > 3.0% | -0.3 | -30 |

- The fee recipient is the store account `perps_fees`; it receives every taker and maker fee and pays every rebate. The insurance share of trading fees is `Params.InsuranceSharePpm`, 0 ppm at activation; the insurance fund is funded by backstop margin and backstop position profit (sections 6 and 7).
- Liquidation fee: 0. A market liquidation is an ordinary taker fill at the account's taker rate; the caller of `liquidate` receives nothing.
- Deviation: the Hyperliquid volume weights spot volume twice; the perpetuals venue counts perpetuals volume only, because spot volume lives in the spot module (contracts/spot.md).

## 2. Margin

- Margin is cross: one USDL collateral balance per account backs every position and resting order of that account.
- Initial margin of a position = notional x 1 / leverage, where leverage is the leverage the order requested and is bounded by the tier maximum for the resulting notional (contracts/perps-markets.md, section 4). Initial margin of a resting order = order notional at its limit price / leverage.
- Maintenance margin of a position = notional at mark x MMR(tier) - deduction(tier), with MMR = 0.5 / tier max leverage and the deduction recurrence of contracts/perps-markets.md section 1.
- Account equity = collateral x 1e12 + sum over its positions of side x (size x mark / 1e18 - cost basis) + sum over its positions of funding received since the last settlement (negative when paid), in USD; the deficit of an account (contracts/perps.md, section 8) counts against equity.
- An order or position increase is accepted only if equity - the taker fee on the order notional >= initial margin of all positions and resting orders after the change. The fee is reserved at the account's taker rate whether the order later fills as taker or maker.
- Sizing of the compatibility opens `openPosition` and `addSize` with collateral c and leverage L at taker rate f: notional N = c x L / (1 + L x f), rounded down, sized in base at the highest price the order can fill at, so N / L + N x f <= c and the check above passes for an account that holds nothing else (contracts/perps.md, section 8). `placeLimitOrder` and `placeStopLimitOrder` use notional min(sizeUsd, c x L / (1 + L x f)) at the limit price (contracts/perps.md, section 5).
- Withdrawable = equity - max(initial margin of positions and orders, 10 percent of total position notional at mark), floored at 0. Withdraw, removeCollateral and transferCollateral are bounded by it; withdraw of the whole withdrawable amount settles every position of the account first.
- Leverage is a 1e18-scaled argument with a minimum of 1e18 (1x); `maxLeverage` fields are plain integers (contracts/perps.md, section 2).

## 3. Funding

- Interest component I = 0.01 percent per 8 hours = 0.00125 percent per hour.
- Premium sample, every 5 seconds of block time per market: premium = (max(impactBid - oracle, 0) - max(oracle - impactAsk, 0)) / oracle, where impactBid and impactAsk are the average execution prices of selling and buying the impact notional (20,000 USD for BTC and ETH, 6,000 USD otherwise) against the book; a side whose resting size is below the impact notional uses the oracle price for that side. Oracle = the pricefeed index price.
- 8-hour rate F = P + clamp(I - P, -0.0005, 0.0005), where P is the arithmetic mean of the hour's premium samples.
- Hourly rate = F / 8, clamped to +-4 percent per hour (the funding cap, 40,000 ppm).
- Index: at each whole hour of block time (`floor(blockTime / 3600)` changes) the cumulative funding index per unit of base of the market advances by oracle x hourly rate; longs pay shorts when the rate is positive. No position is visited at the hour.
- Lazy settlement: a position's funding since its last settlement = side x size x (index now - index at its last settlement), owed by the position when positive; it is part of equity at every read and is settled into collateral at each settlement of the position (contracts/perps.md, section 8), at the latest by the EndBlock cursor sweep (contracts/perps.md, section 23). On the interface the amount is shown with the opposite sign: negative when the position pays.
- Rounding: funding a position pays is rounded up and funding it receives is rounded down, to 1e-18 USD and to the USDL unit, so the venue never pays out more than it collects; the residue stays in the USDL balance of the perps module EVM address, booked in `residueUsd` (contracts/perps.md, section 20).

## 4. Open interest caps

- Each market has a cap on open interest per side in USD at mark (contracts/perps-markets.md, section 2); a class E market's cap follows the listing-age table of contracts/perps-markets.md section 3.1.
- An opening fill that would take a side above the cap in force reverts with `PerpsOpenInterestCap()`; reduce-only fills and liquidation, backstop and ADL fills always pass.
- Long and short open interest are always equal, because every fill adds or removes the same size on both sides.
- There is no borrowing fee and no borrowing index on this venue; positions pay funding only, as on the Hyperliquid venue.

## 5. Mark price

- Mark = median of three values: (a) oracle + EMA(mid - oracle) with a 150-second time constant over block time, where the oracle is the pricefeed index `Index`, (b) median(best bid, best ask, last trade price), (c) the pricefeed mark `Mark`, whose formula is contracts/pricefeed.md#mark-price and is not restated here.
- A component that is undefined, because an order side has no resting order or the market has no trade yet, is set to the oracle price; mid is undefined when either side is empty.
- A market trades and liquidates only while `GetPrice` reports `Fresh` (contracts/pricefeed.md#freshness-and-read-api); with a stale price every opening order, market close and liquidation of that market reverts with `PerpsPriceStale()` and EndBlock skips that market's trigger and liquidation pass for the block. Cancels, deposits and withdrawals that do not depend on the market's price stay open.

## 6. Liquidation

- Trigger: account equity < account maintenance margin, evaluated at mark in EndBlock and on each `liquidate` call.
- Market liquidation: the account's resting orders are cancelled first; then each position is closed by a reduce-only IOC market order against the book. A position with notional above 100,000 USD is closed in slices of 20 percent of its notional, with at most one slice per position per 30 seconds of block time; a position at or below 100,000 USD closes in one order.
- Liquidation price of a position (all other positions held constant) = price - side x marginAvailable / size / (1 - l x side), with side +1 for long and -1 for short, marginAvailable = equity - maintenance margin, and l = MMR of the position's tier; the result is rounded down to 1e-18 USD.
- Backstop: when equity < 2/3 x maintenance margin, the insurance fund account takes over every position of the account at mark and receives the remaining collateral; the account is left with zero positions and zero collateral.
- No liquidation fee and no penalty is charged; `Liquidation.penalty` is 0.
- Candidates come from the margin-ratio index (bucket = floor(100 x equity / maintenance margin) at the account's last settlement), walked from bucket 0 upward; each visited account is recomputed at mark and re-bucketed.
- Bounds per block: at most 1,024 accounts visited (`Params.LiquidationScanPerBlock`), 64 accounts liquidated and 256 slices; the rest carry to the next block in index order. The cursor sweep settles `Params.SweepPositionsPerBlock` positions per block (512 at activation), so a stale bucket is refreshed at the latest once per sweep cycle.

## 7. Insurance fund and auto-deleveraging

- The insurance fund is the store account `perps_insurance` over the USDL balance of the perps module EVM address; it holds the backstop positions, receives backstop margin, backstop position profit and the `Params.InsuranceSharePpm` share of trading fees, and pays account deficits (contracts/perps.md, section 8).
- The insurance fund closes its backstop positions against the book in EndBlock with the same slice rule as section 6.
- ADL trigger: an account (including the insurance fund) has a deficit that the insurance fund balance cannot cover.
- ADL rank of an opposite-side profitable position = (mark / entry for a long, entry / mark for a short) x (position notional / account value). The per-market ADL ranking index keeps every position by side and rank score at its last settlement; positions are deleveraged in descending rank, ties resolved by ascending position id.
- ADL executes at the previous block's mark, reducing the deficit position and the ranked counterparties by the same size, at most 64 counterparty positions per block, until the deficit is closed; the remainder carries to the next block.

## 8. Order limits

- Order types: LIMIT 0, STOP_LIMIT 1, TAKE_PROFIT 2, STOP_LOSS 3, MARKET 4, STOP_MARKET 5 (0 to 3 as Hyperpax:src/core/perps/config.ts:212-217). Time in force: GTC 0, IOC 1, FOK 2, POST_ONLY 3 (the order of include/layerx/lx_perps.h:78-83). MARKET and triggered market orders are IOC.
- Minimum order value: 10 USD. Maximum market order value: 30,000,000 USD for max leverage >= 25, 5,000,000 for 20 to 25, 2,000,000 for 10 to 20, 500,000 below 10. Maximum limit order value: 10 times the market order maximum.
- Default slippage bound of a MARKET order without acceptablePrice and of the Hyperpax compatibility methods openPosition, addSize, closePosition and partialClose: 5,000 ppm (50 bps) from mark, the application's default (Hyperpax:src/core/perps/config.ts:354), rounded to the tick toward mark. Matching stops at the bound and the IOC remainder is cancelled; an empty opposite side reverts with `PerpsNoLiquidity()` and no fill within the bound reverts with `PerpsSlippageExceeded()`.
- Open orders per account: 1,000. Fills per taker order: 32; the remainder of an IOC order beyond 32 fills is cancelled.
- Self-trade prevention: an incoming order that would match a resting order of the same account cancels that resting order and continues.
- Trigger evaluation: at most 256 triggered orders per block, in the trigger key order (market id ascending; per direction in crossing order, buy-side from the lowest trigger price up and sell-side from the highest down; then placement sequence).
- Expiry: at most 256 expired orders per block, in deadline order; the rest carry to the next block.

## 9. Consequences of the 1000x cap

- Every market's tier 0 maximum leverage is 1000x; at 1000x the initial margin rate is 10 bps and the maintenance margin rate is 5 bps, so the buffer between opening and maintenance is 5 bps of notional.
- The tier 0 taker fee is 4.5 bps; an order is accepted only if equity less the reserved taker fee still covers initial margin, so a 1000x position opened by a taker order needs collateral of at least 14.5 bps of notional.
- The round-trip tier 0 taker fee is 9 bps, which exceeds the 5 bps buffer: a position opened and closed by taker orders at 1000x returns less than its deposited collateral unless the price moves in its favour by more than 9 bps.
- A price move of 5 bps against a 1000x position opened with exactly its initial margin reaches maintenance; the 20 percent slice rule applies only above 100,000 USD notional, so smaller positions close in one order.
- The funding cap of 4 percent per hour is 40 times the 1000x initial margin rate: one capped hourly index step exceeds the margin of a 1000x position. Equity includes funding owed by the index at every read, so such a position is liquidatable from the block in which the index advances, without waiting for its funding to settle.
- Every one of these consequences is asserted by a test in modules/perps/keeper/margin_test.go and modules/perps/keeper/funding_test.go (contracts/perps.md, section 24).

## Spot fees

The spot venue (contracts/spot.md, heading "Fees") keeps the PECOR per-trade fee model and does not use the 14-day volume tiers of section 1. The fee in bps of one trade = base + tier1FeeBps when the trade's USD volume is at least 10,000e18 + tier2FeeBps on top when it is at least 100,000e18 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:743-747). The thresholds apply to the single trade; no rolling volume is kept or read, and the spot keeper neither reads nor writes the perpetuals volume buckets of section 1.

| parameter | initial value | rail | bound (Params.Validate) |
|---|---|---|---|
| swapFeeBps | 7 | taker, market and swap rail at 0x1021 | base plus tiers at most 200 |
| orderFeeBps | 4 | maker, resting fills at 0x1022 and 0x1023 | at most MAX_ORDER_FEE_BPS |
| tier1FeeBps | 0 | added from 10,000 USD of trade volume | base plus tiers at most 200 |
| tier2FeeBps | 0 | added on top from 100,000 USD of trade volume | at least tier1FeeBps |
| priceImpactScalarBps | 100 | swap-rail impact deduction, capped at MAX_IMPACT_BPS 500 | at most MAX_IMPACT_BPS 500 (`ScalarTooHigh()`) |

- The fee and the impact deduction stay in the vault as an accrual of the output token (contracts/spot.md, heading "Fees") and are collected by feeCollector; there is no `spot_fees` module account and no maker rebate.
- The opening pairs are PAX/USDL and SID/USDL (contracts/spot.md, heading "Opening pairs").
- The perpetuals venue keeps counting perpetuals volume only (section 1); the Hyperliquid 2 x spot weight is not applied anywhere, because the spot venue has no volume tiers.
