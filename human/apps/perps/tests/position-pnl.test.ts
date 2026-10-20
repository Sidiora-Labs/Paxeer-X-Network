import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import {
  WAD,
  computePositionPnl,
  decimalToWad,
  formatPnlPercent,
  formatPnlValue,
  formatWadFixed,
  markPriceWad,
  parsePositionRow,
  wadToDecimal,
  type ParsedPosition,
} from "../src/core/perps/pnlHelpers";

const usd = (n: bigint) => n * WAD;

function position(over: Partial<ParsedPosition>): ParsedPosition {
  return {
    positionId: 1n,
    marketId: 1,
    symbol: "BTC",
    isLong: true,
    sizeUsd: usd(10_000n),
    collateralUsd: usd(1_000n),
    leverage: usd(10n),
    entryPrice: usd(50_000n),
    collateralToken: "",
    collateralAmount: 0n,
    fundingPaidUsd: 0n,
    feesUsd: 0n,
    openedAt: undefined,
    openTxHash: undefined,
    ...over,
  };
}

describe("position pnl", () => {
  it("long 10x BTC up 10% doubles collateral", () => {
    const p = computePositionPnl(position({}), usd(55_000n));
    assert.equal(p.markPrice, usd(55_000n));
    assert.equal(p.initialCollateral, usd(1_000n));
    assert.equal(p.unrealizedPnl, usd(1_000n));
    assert.equal(p.remainingCollateral, usd(2_000n));
    assert.equal(p.netValue, usd(2_000n));
    assert.equal(p.roe, usd(100n));
    assert.equal(p.unrealizedPnlPct, usd(100n));
    assert.equal(p.collateralHealthPct, usd(200n));
    assert.equal(p.marginPct, usd(20n));
    assert.equal(p.liqPrice, usd(45_500n));
    assert.equal(p.liqSafetyBps, 10_000n);
  });

  it("short 10x BTC against a 10% rise is wiped out and past liquidation", () => {
    const p = computePositionPnl(position({ isLong: false }), usd(55_000n));
    assert.equal(p.unrealizedPnl, -usd(1_000n));
    assert.equal(p.remainingCollateral, 0n);
    assert.equal(p.roe, -usd(100n));
    assert.equal(p.collateralHealthPct, 0n);
    assert.equal(p.liqPrice, usd(54_500n));
    assert.equal(p.liqSafetyBps, 0n);
  });

  it("funding and fees reduce remaining collateral, not price pnl", () => {
    const p = computePositionPnl(
      position({ fundingPaidUsd: usd(10n), feesUsd: 5n * WAD + WAD / 2n }),
      usd(55_000n),
    );
    assert.equal(p.unrealizedPnl, usd(1_000n));
    assert.equal(p.remainingCollateral, usd(1_984n) + WAD / 2n);
    assert.equal(p.collateralHealthPct, 198_450_000_000_000_000_000n);
  });

  it("notional above 2^53 wei with a sub-wei remainder truncates toward zero", () => {
    const sizeUsd = 123_456_789_123_456_789_123_456_789n;
    assert.ok(sizeUsd > 2n ** 53n);
    const long = computePositionPnl(
      position({ sizeUsd, leverage: WAD, entryPrice: usd(7n) }),
      usd(8n),
    );
    assert.equal(long.unrealizedPnl, 17_636_684_160_493_827_017_636_684n);
    assert.equal(long.initialCollateral, sizeUsd);
    assert.equal(long.remainingCollateral, sizeUsd + 17_636_684_160_493_827_017_636_684n);
    const short = computePositionPnl(
      position({ sizeUsd, leverage: WAD, entryPrice: usd(7n), isLong: false }),
      usd(8n),
    );
    assert.equal(short.unrealizedPnl, -17_636_684_160_493_827_017_636_684n);
  });

  it("sub-wei pnl drops the fraction on both sides", () => {
    const long = computePositionPnl(position({ sizeUsd: 5n, entryPrice: usd(3n) }), usd(4n));
    assert.equal(long.unrealizedPnl, 1n);
    const short = computePositionPnl(position({ sizeUsd: 5n, entryPrice: usd(3n), isLong: false }), usd(4n));
    assert.equal(short.unrealizedPnl, -1n);
    const tiny = computePositionPnl(position({ sizeUsd: 2n, entryPrice: usd(3n) }), usd(4n));
    assert.equal(tiny.unrealizedPnl, 0n);
  });

  it("liquidation price rounds toward entry", () => {
    const long = computePositionPnl(position({ entryPrice: usd(100n), leverage: usd(7n) }), usd(100n));
    assert.equal(long.liqPrice, 87_142_857_142_857_142_858n);
    const short = computePositionPnl(
      position({ entryPrice: usd(100n), leverage: usd(7n), isLong: false }),
      usd(100n),
    );
    assert.equal(short.liqPrice, 112_857_142_857_142_857_142n);
    assert.equal(long.liqSafetyBps, 10_000n);
    const low = computePositionPnl(position({ leverage: WAD / 2n }), usd(50_000n));
    assert.equal(low.liqPrice, 0n);
  });

  it("no mark or no entry yields the idle state", () => {
    const p = computePositionPnl(position({ feesUsd: usd(100n) }), 0n);
    assert.equal(p.markPrice, 0n);
    assert.equal(p.unrealizedPnl, 0n);
    assert.equal(p.remainingCollateral, usd(900n));
    assert.equal(p.collateralHealthPct, usd(90n));
    assert.equal(p.liqPrice, 0n);
    assert.equal(p.roe, 0n);
    const noEntry = computePositionPnl(position({ entryPrice: 0n }), usd(1n));
    assert.equal(noEntry.unrealizedPnl, 0n);
  });

  it("long and short pnl and roe are exact negatives for the same move", () => {
    let seed = 0x2545f4914f6cdd1dn;
    const next = (mod: bigint) => {
      seed = (seed * 6364136223846793005n + 1442695040888963407n) % 2n ** 64n;
      return seed % mod;
    };
    for (let i = 0; i < 500; i++) {
      const base = {
        sizeUsd: next(10n ** 30n) + 1n,
        leverage: next(100n * WAD) + 1n,
        entryPrice: next(10n ** 24n) + 1n,
      };
      const mark = next(10n ** 24n) + 1n;
      const long = computePositionPnl(position({ ...base, isLong: true }), mark);
      const short = computePositionPnl(position({ ...base, isLong: false }), mark);
      assert.equal(long.unrealizedPnl, -short.unrealizedPnl);
      assert.equal(long.roe, -short.roe);
      assert.equal(long.remainingCollateral + short.remainingCollateral, 2n * long.initialCollateral);
    }
  });

  it("parses raw gateway strings without a float", () => {
    const p = parsePositionRow(
      {
        positionId: "42",
        marketId: "3",
        isLong: false,
        sizeUsd: "123456789123456789123456789",
        collateralUsd: "1000000000000000000001",
        leverage: "5000000000000000000",
        entryPrice: "65000500000000000000000",
        collateralToken: "0xabc",
        collateralAmount: "9007199254740993",
        openedAt: "2025-01-01",
      },
      "ETH",
    );
    assert.equal(p.positionId, 42n);
    assert.equal(p.marketId, 3);
    assert.equal(p.sizeUsd, 123_456_789_123_456_789_123_456_789n);
    assert.equal(p.collateralUsd, 1_000_000_000_000_000_000_001n);
    assert.equal(p.leverage, usd(5n));
    assert.equal(p.entryPrice, 65_000_500_000_000_000_000_000n);
    assert.equal(p.collateralAmount, 9_007_199_254_740_993n);
    assert.equal(p.fundingPaidUsd, 0n);
    assert.equal(p.feesUsd, 0n);
  });

  it("converts ticker mark prices exactly from their decimal text", () => {
    assert.equal(markPriceWad(65000.5), 65_000_500_000_000_000_000_000n);
    assert.equal(markPriceWad(0.1), 100_000_000_000_000_000n);
    assert.equal(markPriceWad(1e-20), 0n);
    assert.equal(markPriceWad(Number.NaN), 0n);
    assert.equal(markPriceWad(-3), 0n);
    assert.equal(markPriceWad(null), 0n);
    assert.equal(decimalToWad("-1.0000000000000000019"), -1_000_000_000_000_000_001n);
  });

  it("formats only at the edge with half-away-from-zero rounding", () => {
    assert.equal(wadToDecimal(-1_500_000_000_000_000_000n), "-1.5");
    assert.equal(wadToDecimal(123_456_789_123_456_789_123_456_789n), "123456789.123456789123456789");
    assert.equal(formatWadFixed(5_000_000_000_000_000n, 2), "0.01");
    assert.equal(formatWadFixed(-4_999_999_999_999_999n, 2), "0.00");
    assert.equal(formatWadFixed(-5_000_000_000_000_000n, 2), "-0.01");
    assert.equal(formatWadFixed(usd(1_234_567n), 0, true), "1,234,567");
    assert.equal(formatPnlValue(12_345_670_050_000_000_000_000_000n), "+$12,345,670.05");
    assert.equal(formatPnlValue(-usd(5n)), "-$5.00");
    assert.equal(formatPnlPercent(-usd(100n)), "-100.00%");
    assert.equal(formatPnlPercent(123_456_000_000_000_000n), "+0.12%");
  });
});
