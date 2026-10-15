# Perpetuals markets at activation

The eighteen markets that `SeedV6120` (modules/perps/keeper/seed_v6120.go) writes at the v6.12.0 activation height. Market ids and names follow the Hyperpax application's market list (Hyperpax:src/core/perps/config.ts:168-185) so the application's market ids address the same markets. The maximum leverage of every market is 1000x, the venue cap (decision.4); the application's `maxLeverage` values at those lines are updated to 1000 for all eighteen markets (contracts/application.md).

## 1. Common rules

- Collateral of every market is USDL, the existing ERC20 contract 0x85FcD13735F4309833A503EE804ea32395851479 with 6 decimals (Hyperpax:src/core/perps/config.ts:75-79; Launch-x:contracts/deployments/paxeer-x-network-addresses.json:6), stored in `Params.CollateralToken`; the venue holds it as the ERC20 balance of the perps module EVM address and accepts only that address as a token argument (contracts/perps.md, section 2). There is no bank denom for USDL.
- Prices and USD amounts on the precompile ABI are 18-decimal fixed point (1e18 = 1 USD), as the application already encodes them (Hyperpax:src/core/perps/amounts.ts:17-19).
- The oracle symbol string is the key the pricefeed module (modules/pricefeed, precompile 0x1020, contracts/pricefeed.md) publishes; the pricefeed market ids are 0 to 17 in the order of Hyperpax:src/core/perps/config.ts:168-185 (contracts/pricefeed.md#markets-and-chainflow-symbols), the perps market id of each symbol below is the same id, `SeedV6120` writes the same ids, and the perps keeper reads `GetPrice(ctx, marketId)` (contracts/pricefeed.md#freshness-and-read-api).
- `szDecimals` is the number of decimals of the base size; the lot is 10^-szDecimals of the base asset. `pxDecimals` is the number of price decimals and never exceeds 6 - szDecimals; the tick is 10^-pxDecimals USD.
- Every market's funding cap is 4 percent per hour (contracts/perps-parameters.md, section 3); the impact notional is 20,000 USD for BTC and ETH and 6,000 USD for every other market.
- The open-interest cap applies to each side separately (contracts/perps-parameters.md, section 4). Long and short open interest of a market are always equal, so the cap bounds both at once. A fixed cap is a market config value; the cap of a class E market follows the listing-age table of section 3.1 from the market's `listedAt` block time, which `SeedV6120` sets to the activation block time.
- The maintenance margin rate of a tier is half of the initial margin rate at the tier's maximum leverage: MMR = 0.5 / maxLeverage. The deduction of tier n is deduction(n-1) + lowerBound(n) x (MMR(n) - MMR(n-1)), so maintenance margin is continuous at every tier boundary.

## 2. Market table

| id | symbol | name | oracle symbol | szDecimals | pxDecimals | tick (USD) | lot (base) | max leverage | tier class | impact notional (USD) | OI cap per side (USD) | funding cap |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | BTC | Bitcoin | `BTC` | 5 | 1 | 0.1 | 0.00001 | 1000x | A | 20,000 | 500,000,000 | 4% per hour |
| 1 | ETH | Ethereum | `ETH` | 4 | 2 | 0.01 | 0.0001 | 1000x | A | 20,000 | 500,000,000 | 4% per hour |
| 2 | SOL | Solana | `SOL` | 2 | 3 | 0.001 | 0.01 | 1000x | B | 6,000 | 50,000,000 | 4% per hour |
| 3 | AVAX | Avalanche | `AVAX` | 2 | 3 | 0.001 | 0.01 | 1000x | B | 6,000 | 50,000,000 | 4% per hour |
| 4 | LINK | Chainlink | `LINK` | 1 | 3 | 0.001 | 0.1 | 1000x | F | 6,000 | 20,000,000 | 4% per hour |
| 5 | TSLA | Tesla | `TSLA` | 3 | 2 | 0.01 | 0.001 | 1000x | D | 6,000 | 25,000,000 | 4% per hour |
| 6 | NVDA | NVIDIA | `NVDA` | 3 | 2 | 0.01 | 0.001 | 1000x | D | 6,000 | 25,000,000 | 4% per hour |
| 7 | NAS100 | Nasdaq 100 | `NAS100` | 4 | 1 | 0.1 | 0.0001 | 1000x | C | 6,000 | 100,000,000 | 4% per hour |
| 8 | XAU | Gold | `XAU` | 4 | 1 | 0.1 | 0.0001 | 1000x | C | 6,000 | 100,000,000 | 4% per hour |
| 9 | SPX500 | S&P 500 | `SPX500` | 4 | 1 | 0.1 | 0.0001 | 1000x | C | 6,000 | 100,000,000 | 4% per hour |
| 10 | GOOGL | Alphabet | `GOOGL` | 3 | 2 | 0.01 | 0.001 | 1000x | D | 6,000 | 25,000,000 | 4% per hour |
| 11 | PAX | Paxeer Network | `PAX` | 0 | 5 | 0.00001 | 1 | 1000x | E | 6,000 | section 3.1 | 4% per hour |
| 12 | SID | Sidiora Markets | `SID` | 0 | 5 | 0.00001 | 1 | 1000x | E | 6,000 | section 3.1 | 4% per hour |
| 13 | HYPE | Hyperliquid | `HYPE` | 2 | 3 | 0.001 | 0.01 | 1000x | B | 6,000 | 50,000,000 | 4% per hour |
| 14 | XRP | XRP | `XRP` | 0 | 4 | 0.0001 | 1 | 1000x | B | 6,000 | 50,000,000 | 4% per hour |
| 15 | ASTER | Aster | `ASTER` | 0 | 5 | 0.00001 | 1 | 1000x | E | 6,000 | section 3.1 | 4% per hour |
| 16 | TRUMP | Trump | `TRUMP` | 1 | 3 | 0.001 | 0.1 | 1000x | E | 6,000 | section 3.1 | 4% per hour |
| 17 | BNB | Binance Smart Chain | `BNB` | 3 | 2 | 0.01 | 0.001 | 1000x | B | 6,000 | 50,000,000 | 4% per hour |

## 3. Tier classes

- Class A, BTC and ETH: BTC, ETH.
- Class B, major alternative assets: SOL, AVAX, HYPE, XRP, BNB.
- Class C, indices and gold: NAS100, XAU, SPX500.
- Class D, single equities: TSLA, NVDA, GOOGL.
- Class E, native and new listings: PAX, SID, ASTER, TRUMP.
- Class F, LINK: LINK.

### 3.1 Class E open-interest cap by listing age

The OI cap per side of a class E market is the row whose age range contains the market's listing age, `floor((blockTime - listedAt) / 86400)` whole UTC days. The table is `Params.ClassEOiCaps` (rows of minimum age in days and cap in USD, ascending); an upgrade handler changes it through `Keeper.UpdateParams`, and `Keeper.SetMarket` resets `listedAt` when a market is listed again. `getMarket(marketId).maxOpenInterest` and `getMarketConfig(marketId).oiCapUsd` return the cap in force at the block of the call.

| listing age (UTC days) | OI cap per side (USD) |
|---|---|
| 0 to 29 | 2,000,000 |
| 30 to 89 | 5,000,000 |
| 90 to 179 | 10,000,000 |
| 180 and above | 25,000,000 |

- A lower cap at a later age never occurs: the caps strictly increase with age, so a cap change never forces a reduce. An opening fill above the cap in force reverts with `PerpsOpenInterestCap()`; reduce-only fills always pass.

## 4. Margin tier tables

Each row: notional lower bound of the tier in USD, maximum leverage inside the tier, maintenance margin rate in basis points (MMR = 0.5 / max leverage), initial margin rate at that leverage, and the maintenance deduction in USD. Maintenance margin of a position = notional x MMR(tier) - deduction(tier).

### 4.1 BTC (market 0, class A)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 1,000,000 | 500x | 10 | 20 | 500 |
| 2 | 5,000,000 | 200x | 25 | 50 | 8,000 |
| 3 | 20,000,000 | 100x | 50 | 100 | 58,000 |
| 4 | 50,000,000 | 40x | 125 | 250 | 433,000 |
| 5 | 150,000,000 | 20x | 250 | 500 | 2,308,000 |

### 4.2 ETH (market 1, class A)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 1,000,000 | 500x | 10 | 20 | 500 |
| 2 | 5,000,000 | 200x | 25 | 50 | 8,000 |
| 3 | 20,000,000 | 100x | 50 | 100 | 58,000 |
| 4 | 50,000,000 | 40x | 125 | 250 | 433,000 |
| 5 | 150,000,000 | 20x | 250 | 500 | 2,308,000 |

### 4.3 SOL (market 2, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 1,000 |
| 2 | 2,000,000 | 50x | 100 | 200 | 16,000 |
| 3 | 10,000,000 | 20x | 250 | 500 | 166,000 |
| 4 | 30,000,000 | 10x | 500 | 1,000 | 916,000 |

### 4.4 AVAX (market 3, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 1,000 |
| 2 | 2,000,000 | 50x | 100 | 200 | 16,000 |
| 3 | 10,000,000 | 20x | 250 | 500 | 166,000 |
| 4 | 30,000,000 | 10x | 500 | 1,000 | 916,000 |

### 4.5 LINK (market 4, class F)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 250,000 | 50x | 100 | 200 | 2,375 |
| 2 | 1,000,000 | 20x | 250 | 500 | 17,375 |
| 3 | 5,000,000 | 10x | 500 | 1,000 | 142,375 |

### 4.6 TSLA (market 5, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,125 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,125 |
| 3 | 5,000,000 | 10x | 500 | 1,000 | 146,125 |

### 4.7 NVDA (market 6, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,125 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,125 |
| 3 | 5,000,000 | 10x | 500 | 1,000 | 146,125 |

### 4.8 NAS100 (market 7, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 2,000 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,500 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,500 |

### 4.9 XAU (market 8, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 2,000 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,500 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,500 |

### 4.10 SPX500 (market 9, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 2,000 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,500 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,500 |

### 4.11 GOOGL (market 10, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,125 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,125 |
| 3 | 5,000,000 | 10x | 500 | 1,000 | 146,125 |

### 4.12 PAX (market 11, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1,000 | 7,295 |
| 4 | 1,000,000 | 5x | 1,000 | 2,000 | 57,295 |

### 4.13 SID (market 12, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1,000 | 7,295 |
| 4 | 1,000,000 | 5x | 1,000 | 2,000 | 57,295 |

### 4.14 HYPE (market 13, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 1,000 |
| 2 | 2,000,000 | 50x | 100 | 200 | 16,000 |
| 3 | 10,000,000 | 20x | 250 | 500 | 166,000 |
| 4 | 30,000,000 | 10x | 500 | 1,000 | 916,000 |

### 4.15 XRP (market 14, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 1,000 |
| 2 | 2,000,000 | 50x | 100 | 200 | 16,000 |
| 3 | 10,000,000 | 20x | 250 | 500 | 166,000 |
| 4 | 30,000,000 | 10x | 500 | 1,000 | 916,000 |

### 4.16 ASTER (market 15, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1,000 | 7,295 |
| 4 | 1,000,000 | 5x | 1,000 | 2,000 | 57,295 |

### 4.17 TRUMP (market 16, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1,000 | 7,295 |
| 4 | 1,000,000 | 5x | 1,000 | 2,000 | 57,295 |

### 4.18 BNB (market 17, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 1,000 |
| 2 | 2,000,000 | 50x | 100 | 200 | 16,000 |
| 3 | 10,000,000 | 20x | 250 | 500 | 166,000 |
| 4 | 30,000,000 | 10x | 500 | 1,000 | 916,000 |

## 5. Invariants of the table

- Every market's tier 0 maximum leverage is 1000x, equal to its listed max leverage and to the venue cap.
- At 1000x the initial margin rate is 10 bps and the maintenance rate 5 bps on every market (contracts/perps-parameters.md, section 9).
- Maximum leverage strictly decreases and MMR strictly increases with the tier index; lower bounds strictly increase from 0.
- The worked check of the recurrence: BTC tier 1 deduction = 0 + 1,000,000 x (0.0010 - 0.0005) = 500 USD, so at 1,000,000 USD notional tier 0 gives 500 USD and tier 1 gives 1,000 - 500 = 500 USD; SOL tier 1 deduction = 500,000 x (0.0025 - 0.0005) = 1,000 USD, so at 500,000 USD both tiers give 250 USD.
- `getMarginTiers(marketId)` on 0x1015 returns these rows exactly (contracts/perps.md, section 4); `getMarket(marketId).maintenanceMarginBps` returns the tier 0 MMR, 5 for every market.
