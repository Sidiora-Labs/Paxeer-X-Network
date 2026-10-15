# Perpetuals parameter sheet

The economic parameters of the perpetuals venue at 0x1015. They follow the structure of the Hyperliquid venue (decision.4) with the deviations named in each section. Every figure here is a default of `modules/perps/types/params.go` `DefaultParams()` or of the market configs written by `SeedV6120`; changes after activation are made by an upgrade handler through `Keeper.UpdateParams` and `Keeper.SetMarket`, both signed by the governance module account. Units: USD amounts and prices in 18-decimal fixed point, collateral in uusdl (6 decimals), rates in parts per million (ppm, 1 ppm = 0.0001 percent = 0.01 bp) unless a section says otherwise. Every intermediate product is computed in 256-bit integers and rounded as each formula states.

## 1. Trading fees

Fee tier of an account = the highest tier whose threshold its rolling 14-day perpetuals volume exceeds. Volume is kept in 14 daily buckets keyed by `floor(blockTime / 86400)`; both sides of a fill count the fill notional. Taker fee = fill notional x taker rate, rounded up to the uusdl unit. Maker fee = fill notional x maker rate, rounded up; a negative maker rate is a rebate paid from the fee recipient account, rounded down.

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

- The fee recipient is the module account `perps_fees`; it receives every taker and maker fee and pays every rebate. The insurance share of trading fees is 0 ppm; the insurance fund is funded by the borrowing-fee share and by backstop margin (sections 4 and 6).
- Liquidation fee: 0. A market liquidation is an ordinary taker fill at the account's taker rate; the caller of `liquidate` receives nothing.
- Deviation: the Hyperliquid volume weights spot volume twice; the perpetuals venue counts perpetuals volume only, because spot volume lives in the spot module (contracts/spot.md).

## 2. Margin

- Margin is cross: one USDL collateral balance per account backs every position and resting order of that account.
- Initial margin of a position = notional x 1 / leverage, where leverage is the leverage the order requested and is bounded by the tier maximum for the resulting notional (contracts/perps-markets.md, section 4). Initial margin of a resting order = order notional at its limit price / leverage.
- Maintenance margin of a position = notional at mark x MMR(tier) - deduction(tier), with MMR = 0.5 / tier max leverage and the deduction recurrence of contracts/perps-markets.md section 1.
- Account equity = collateral + sum of unrealised PnL at mark - funding owed - borrowing fee accrued.
- An order or position increase is accepted only if equity after the opening fee >= initial margin of all positions and resting orders after the change.
- Withdrawable = equity - max(initial margin of positions and orders, 10 percent of total position notional at mark), floored at 0. Withdraw, removeCollateral and transferCollateral are bounded by it.
- Leverage is a 1e18-scaled argument with a minimum of 1e18 (1x).

## 3. Funding

- Interest component I = 0.01 percent per 8 hours = 0.00125 percent per hour.
- Premium sample, every 5 seconds of block time per market: premium = (max(impactBid - oracle, 0) - max(oracle - impactAsk, 0)) / oracle, where impactBid and impactAsk are the average execution prices of selling and buying the impact notional (20,000 USD for BTC and ETH, 6,000 USD otherwise) against the book; a side with less depth than the impact notional uses the oracle price for that side. Oracle = the pricefeed index price.
- 8-hour rate F = P + clamp(I - P, -0.0005, 0.0005), where P is the arithmetic mean of the hour's premium samples.
- Hourly rate = F / 8, clamped to +-4 percent per hour (the funding cap, 40,000 ppm).
- Settlement at each whole hour of block time (`floor(blockTime / 3600)` changes): payment of a position = signed size x oracle x hourly rate; longs pay shorts when the rate is positive. The cumulative funding index per unit of base advances by oracle x hourly rate; a position's funding owed = size x (index now - index at last settlement of the position).
- Rounding: payments owed are rounded up, payments received are rounded down, so the venue never pays out more than it collects; the residue accrues to the insurance fund.

## 4. Borrowing fee on open interest

- Utilisation U of a market = max(long OI, short OI) / OI cap of the market, capped at 1.
- Annual rate APY(U) = 0.05 + 4.75 x max(0, U - 0.8). At U = 0.8 the rate is 5 percent, at U = 1 it is 100 percent.
- Hourly rate = APY(U) / 8760, applied at each hourly settlement to every open position's notional at mark through a cumulative borrowing index per market; a position's accrued fee = notional x (index now - index at open or last settlement).
- Split: 10 percent to the fee recipient `perps_fees`, 90 percent to the insurance fund `perps_insurance`.
- Deviation: the Hyperliquid venue charges this curve on borrowed spot balances under portfolio margin; the perpetuals venue charges it on open interest as the programme requires (head req.2 ac_3).

## 5. Mark price

- Mark = median of three values: (a) oracle + EMA(mid - oracle) with a 150-second time constant over block time, where the oracle is the pricefeed index `Index`, (b) median(best bid, best ask, last trade price), (c) the pricefeed mark `Mark`, whose formula is contracts/pricefeed.md#mark-price and is not restated here.
- A missing component (empty book side, no trade yet) is replaced by the oracle price.
- A market trades and liquidates only while `GetPrice` reports `Fresh` (contracts/pricefeed.md#freshness-and-read-api); with a stale price every opening order, market close and liquidation of that market reverts with `LXP_PERPS_PRICE_STALE` and EndBlock skips that market's trigger and liquidation pass for the block. Cancels, deposits and withdrawals that do not depend on the market's price stay open.

## 6. Liquidation

- Trigger: account equity < account maintenance margin, evaluated at mark in EndBlock and on each `liquidate` call.
- Market liquidation: the account's resting orders are cancelled first; then each position is closed by a reduce-only IOC market order against the book. A position with notional above 100,000 USD is closed in slices of 20 percent of its notional, with at most one slice per position per 30 seconds of block time; a position at or below 100,000 USD closes in one order.
- Liquidation price of a position (all other positions held constant) = price - side x marginAvailable / size / (1 - l x side), with side +1 for long and -1 for short, marginAvailable = equity - maintenance margin, and l = MMR of the position's tier.
- Backstop: when equity < 2/3 x maintenance margin, the insurance fund account takes over every position of the account at mark and receives the remaining collateral; the account is left with zero positions and zero collateral.
- No liquidation fee and no penalty is charged; `Liquidation.penalty` is 0.
- Bounds: at most 64 accounts liquidated and 256 slices per block; remaining accounts carry to the next block in ascending order of their margin ratio.

## 7. Insurance fund and auto-deleveraging

- The insurance fund is the module account `perps_insurance`; it holds the backstop positions, receives the borrowing-fee share, backstop margin and funding rounding residue, and pays the negative equity of closed backstop positions.
- The insurance fund closes its backstop positions against the book in EndBlock with the same slice rule as section 6.
- ADL trigger: an account (including the insurance fund) has equity below 0 that the insurance fund balance cannot cover.
- ADL rank of an opposite-side profitable position = (mark / entry for a long, entry / mark for a short) x (position notional / account value). Positions are deleveraged in descending rank, ties resolved by ascending position id.
- ADL executes at the previous block's mark, reducing the deficit position and the ranked counterparties by the same size, until the deficit is closed.

## 8. Order limits

- Order types: MARKET 0, LIMIT 1, STOP_MARKET 2, STOP_LIMIT 3, TAKE_PROFIT 4, STOP_LOSS 5. Time in force: GTC 0, IOC 1, FOK 2, POST_ONLY 3 (the order of include/layerx/lx_perps.h:78-83). MARKET and triggered market orders are IOC.
- Minimum order value: 10 USD. Maximum market order value: 30,000,000 USD for max leverage >= 25, 5,000,000 for 20 to 25, 2,000,000 for 10 to 20, 500,000 below 10. Maximum limit order value: 10 times the market order maximum.
- Default slippage bound of a MARKET order without acceptablePrice (and of the Hyperpax compatibility methods openPosition, addSize, closePosition and partialClose): 8 percent from mark.
- Open orders per account: 1,000. Fills per taker order: 32; the remainder of an IOC order beyond 32 fills is cancelled.
- Self-trade prevention: an incoming order that would match a resting order of the same account cancels that resting order and continues.
- Trigger evaluation: at most 256 triggered orders per block, in ascending order of trigger time then order id.

## 9. Consequences of the 1000x cap

- At 1000x the initial margin rate is 10 bps and the maintenance margin rate is 5 bps, so the buffer between opening and maintenance is 5 bps of notional.
- The tier 0 taker fee is 4.5 bps; an order is accepted only if equity after the fee still covers initial margin, so a 1000x position opened by a taker order needs collateral of at least 14.5 bps of notional.
- The round-trip tier 0 taker fee is 9 bps, which exceeds the 5 bps buffer: a position opened and closed by taker orders at 1000x returns less than its deposited collateral unless the price moves in its favour by more than 9 bps.
- A price move of 5 bps against a 1000x position opened with exactly its initial margin reaches maintenance; the 20 percent slice rule applies only above 100,000 USD notional, so smaller positions close in one order.
- The funding cap of 4 percent per hour is 40 times the 1000x initial margin rate: one capped hourly payment exceeds the margin of a 1000x position, so funding settlement runs before the liquidation pass in the same EndBlock and a position whose payment exceeds its margin is liquidated in that block.
- The borrowing fee at full utilisation (100 percent APY) is about 1.14 bps per hour, so a 1000x position at full utilisation reaches maintenance from the borrowing fee alone in about 4.4 hours absent price moves.
- Every one of these consequences is asserted by a test in modules/perps/keeper/margin_test.go and modules/perps/keeper/funding_test.go (contracts/perps.md, section 24).

## Spot fees

The spot venue (contracts/spot.md, heading "Fees") keeps the PECOR per-trade fee model and does not use the 14-day volume tiers of section 1. The fee in bps of one trade = base + tier1FeeBps when the trade's USD volume is at least 10,000e18 + tier2FeeBps on top when it is at least 100,000e18 (Launch-x:contracts/contracts/meta-ag/engine/PECOR.sol:743-747). The thresholds apply to the single trade; no rolling volume is kept or read, and the spot keeper neither reads nor writes the perpetuals volume buckets of section 1.

| parameter | initial value | rail | bound (Params.Validate) |
|---|---|---|---|
| swapFeeBps | 7 | taker, market and swap rail at 0x1021 | base plus tiers at most 200 |
| orderFeeBps | 4 | maker, resting fills at 0x1022 and 0x1023 | at most MAX_ORDER_FEE_BPS |
| tier1FeeBps | 0 | added from 10,000 USD of trade volume | base plus tiers at most 200 |
| tier2FeeBps | 0 | added on top from 100,000 USD of trade volume | at least tier1FeeBps |
| priceImpactScalarBps | 100 | impact deduction, capped at 500 bps | at most 10000 |

- The fee and the impact deduction stay in the vault as an accrual of the output token (contracts/spot.md, heading "Fees") and are collected by feeCollector; there is no `spot_fees` module account and no maker rebate.
- The opening pairs are PAX/USDL and SID/USDL (contracts/spot.md, heading "Opening pairs").
- The perpetuals venue keeps counting perpetuals volume only (section 1); the Hyperliquid 2 x spot weight is not applied anywhere, because the spot venue has no volume tiers.
