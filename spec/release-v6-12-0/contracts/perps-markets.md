# Perpetuals markets at activation

The eighteen markets that `SeedV6120` (modules/perps/keeper/seed_v6120.go) writes at the v6.12.0 activation height. Market ids and names follow the Hyperpax application's market list (Hyperpax:src/core/perps/config.ts:168-185) so the application's market ids address the same markets. The maximum leverage of each market is the application's `maxLeverage` value from the same lines; the venue cap is 1000x (decision.4).

## 1. Common rules

- Collateral of every market is USDL, bank denom `uusdl` (modules/launchpad/types/params.go:31), 6 decimals; the precompile accepts only the USDL ERC20 pointer as a token argument.
- Prices and USD amounts on the precompile ABI are 18-decimal fixed point (1e18 = 1 USD), as the application already encodes them (Hyperpax:src/core/perps/amounts.ts:17-19).
- The oracle symbol string is the key the pricefeed module (modules/pricefeed, precompile 0x1020, contracts/pricefeed.md) publishes; the pricefeed market ids are 0 to 17 in the order of Hyperpax:src/core/perps/config.ts:168-185 (contracts/pricefeed.md#markets-and-chainflow-symbols), the perps market id of each symbol below is the same id, `SeedV6120` writes the same ids, and the perps keeper reads `GetPrice(ctx, marketId)` (contracts/pricefeed.md#freshness-and-read-api).
- `szDecimals` is the number of decimals of the base size; the lot is 10^-szDecimals of the base asset. `pxDecimals` is the number of price decimals and never exceeds 6 - szDecimals; the tick is 10^-pxDecimals USD.
- Every market's funding cap is 4 percent per hour (contracts/perps-parameters.md, section 3); the impact notional is 20,000 USD for BTC and ETH and 6,000 USD for every other market.
- The open-interest cap applies to each side separately and is the denominator of the borrowing-fee utilisation (contracts/perps-parameters.md, section 4).
- The maintenance margin rate of a tier is half of the initial margin rate at the tier's maximum leverage: MMR = 0.5 / maxLeverage. The deduction of tier n is deduction(n-1) + lowerBound(n) x (MMR(n) - MMR(n-1)), so maintenance margin is continuous at every tier boundary.

## 2. Market table

| id | symbol | name | oracle symbol | szDecimals | pxDecimals | tick (USD) | lot (base) | max leverage | tier class | impact notional (USD) | OI cap per side (USD) | funding cap |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | BTC | Bitcoin | `BTC` | 5 | 1 | 0.1 | 0.00001 | 1000x | A | 20,000 | 500,000,000 | 4% per hour |
| 1 | ETH | Ethereum | `ETH` | 4 | 2 | 0.01 | 0.0001 | 1000x | A | 20,000 | 500,000,000 | 4% per hour |
| 2 | SOL | Solana | `SOL` | 2 | 3 | 0.001 | 0.01 | 500x | B | 6,000 | 50,000,000 | 4% per hour |
| 3 | AVAX | Avalanche | `AVAX` | 2 | 3 | 0.001 | 0.01 | 500x | B | 6,000 | 50,000,000 | 4% per hour |
| 4 | LINK | Chainlink | `LINK` | 1 | 3 | 0.001 | 0.1 | 200x | F | 6,000 | 20,000,000 | 4% per hour |
| 5 | TSLA | Tesla | `TSLA` | 3 | 2 | 0.01 | 0.001 | 500x | D | 6,000 | 25,000,000 | 4% per hour |
| 6 | NVDA | NVIDIA | `NVDA` | 3 | 2 | 0.01 | 0.001 | 500x | D | 6,000 | 25,000,000 | 4% per hour |
| 7 | NAS100 | Nasdaq 100 | `NAS100` | 4 | 1 | 0.1 | 0.0001 | 500x | C | 6,000 | 100,000,000 | 4% per hour |
| 8 | XAU | Gold | `XAU` | 4 | 1 | 0.1 | 0.0001 | 500x | C | 6,000 | 100,000,000 | 4% per hour |
| 9 | SPX500 | S&P 500 | `SPX500` | 4 | 1 | 0.1 | 0.0001 | 500x | C | 6,000 | 100,000,000 | 4% per hour |
| 10 | GOOGL | Alphabet | `GOOGL` | 3 | 2 | 0.01 | 0.001 | 500x | D | 6,000 | 25,000,000 | 4% per hour |
| 11 | PAX | Paxeer Network | `PAX` | 0 | 5 | 0.00001 | 1 | 1000x | E | 6,000 | 2,000,000 | 4% per hour |
| 12 | SID | Sidiora Markets | `SID` | 0 | 5 | 0.00001 | 1 | 1000x | E | 6,000 | 2,000,000 | 4% per hour |
| 13 | HYPE | Hyperliquid | `HYPE` | 2 | 3 | 0.001 | 0.01 | 500x | B | 6,000 | 50,000,000 | 4% per hour |
| 14 | XRP | XRP | `XRP` | 0 | 4 | 0.0001 | 1 | 500x | B | 6,000 | 50,000,000 | 4% per hour |
| 15 | ASTER | Aster | `ASTER` | 0 | 5 | 0.00001 | 1 | 500x | E | 6,000 | 2,000,000 | 4% per hour |
| 16 | TRUMP | Trump | `TRUMP` | 1 | 3 | 0.001 | 0.1 | 500x | E | 6,000 | 2,000,000 | 4% per hour |
| 17 | BNB | Binance Smart Chain | `BNB` | 3 | 2 | 0.01 | 0.001 | 500x | B | 6,000 | 50,000,000 | 4% per hour |

## 3. Tier classes

- Class A, BTC and ETH: BTC, ETH.
- Class B, major alternative assets: SOL, AVAX, HYPE, XRP, BNB.
- Class C, indices and gold: NAS100, XAU, SPX500.
- Class D, single equities: TSLA, NVDA, GOOGL.
- Class E, thin-liquidity assets: PAX, SID, ASTER, TRUMP.
- Class F, LINK: LINK.

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
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 750 |
| 2 | 2,000,000 | 50x | 100 | 200 | 15,750 |
| 3 | 10,000,000 | 20x | 250 | 500 | 165,750 |
| 4 | 30,000,000 | 10x | 500 | 1000 | 915,750 |

### 4.4 AVAX (market 3, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 750 |
| 2 | 2,000,000 | 50x | 100 | 200 | 15,750 |
| 3 | 10,000,000 | 20x | 250 | 500 | 165,750 |
| 4 | 30,000,000 | 10x | 500 | 1000 | 915,750 |

### 4.5 LINK (market 4, class F)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 200x | 25 | 50 | 0 |
| 1 | 250,000 | 50x | 100 | 200 | 1,875 |
| 2 | 1,000,000 | 20x | 250 | 500 | 16,875 |
| 3 | 5,000,000 | 10x | 500 | 1000 | 141,875 |

### 4.6 TSLA (market 5, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,000 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,000 |
| 3 | 5,000,000 | 10x | 500 | 1000 | 146,000 |

### 4.7 NVDA (market 6, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,000 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,000 |
| 3 | 5,000,000 | 10x | 500 | 1000 | 146,000 |

### 4.8 NAS100 (market 7, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 1,500 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,000 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,000 |

### 4.9 XAU (market 8, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 1,500 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,000 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,000 |

### 4.10 SPX500 (market 9, class C)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 1,000,000 | 200x | 25 | 50 | 1,500 |
| 2 | 5,000,000 | 50x | 100 | 200 | 39,000 |
| 3 | 20,000,000 | 20x | 250 | 500 | 339,000 |

### 4.11 GOOGL (market 10, class D)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 250,000 | 100x | 50 | 100 | 1,000 |
| 2 | 1,000,000 | 20x | 250 | 500 | 21,000 |
| 3 | 5,000,000 | 10x | 500 | 1000 | 146,000 |

### 4.12 PAX (market 11, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1000 | 7,295 |
| 4 | 1,000,000 | 5x | 1000 | 2000 | 57,295 |

### 4.13 SID (market 12, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 1000x | 5 | 10 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 45 |
| 2 | 50,000 | 20x | 250 | 500 | 1,045 |
| 3 | 250,000 | 10x | 500 | 1000 | 7,295 |
| 4 | 1,000,000 | 5x | 1000 | 2000 | 57,295 |

### 4.14 HYPE (market 13, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 750 |
| 2 | 2,000,000 | 50x | 100 | 200 | 15,750 |
| 3 | 10,000,000 | 20x | 250 | 500 | 165,750 |
| 4 | 30,000,000 | 10x | 500 | 1000 | 915,750 |

### 4.15 XRP (market 14, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 750 |
| 2 | 2,000,000 | 50x | 100 | 200 | 15,750 |
| 3 | 10,000,000 | 20x | 250 | 500 | 165,750 |
| 4 | 30,000,000 | 10x | 500 | 1000 | 915,750 |

### 4.16 ASTER (market 15, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 40 |
| 2 | 50,000 | 20x | 250 | 500 | 1,040 |
| 3 | 250,000 | 10x | 500 | 1000 | 7,290 |
| 4 | 1,000,000 | 5x | 1000 | 2000 | 57,290 |

### 4.17 TRUMP (market 16, class E)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 10,000 | 100x | 50 | 100 | 40 |
| 2 | 50,000 | 20x | 250 | 500 | 1,040 |
| 3 | 250,000 | 10x | 500 | 1000 | 7,290 |
| 4 | 1,000,000 | 5x | 1000 | 2000 | 57,290 |

### 4.18 BNB (market 17, class B)

| tier | lower bound (USD) | max leverage | MMR (bps) | IMR at max leverage (bps) | deduction (USD) |
|---|---|---|---|---|---|
| 0 | 0 | 500x | 10 | 20 | 0 |
| 1 | 500,000 | 200x | 25 | 50 | 750 |
| 2 | 2,000,000 | 50x | 100 | 200 | 15,750 |
| 3 | 10,000,000 | 20x | 250 | 500 | 165,750 |
| 4 | 30,000,000 | 10x | 500 | 1000 | 915,750 |

## 5. Invariants of the table

- Every market's tier 0 maximum leverage equals its listed max leverage, and no market exceeds 1000x.
- At 1000x (BTC, ETH, PAX, SID) the initial margin rate is 10 bps and the maintenance rate 5 bps; at 500x they are 20 and 10 bps; at 200x (LINK) 50 and 25 bps.
- Maximum leverage strictly decreases and MMR strictly increases with the tier index; lower bounds strictly increase from 0.
- The worked check of the recurrence: BTC tier 1 deduction = 0 + 1,000,000 x (0.0010 - 0.0005) = 500 USD, so at 1,000,000 USD notional tier 0 gives 500 USD and tier 1 gives 1,000 - 500 = 500 USD.
- `getMarginTiers(marketId)` on 0x1015 returns these rows exactly (contracts/perps.md, section 4); `getMarket(marketId).maintenanceMarginBps` returns the tier 0 MMR.
