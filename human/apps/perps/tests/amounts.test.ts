// Sample tests for F-02 money math (safeParseUnits + deriveImpliedCollateral).
// Run via `pnpm test` — uses node:test + tsx loader (no Jest/Vitest required).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import {
  safeParseUnits,
  deriveImpliedCollateral,
  getCollateralDecimals,
} from "../src/core/perps/amounts";
import { COLLATERAL_OPTIONS, COLLATERAL_TOKENS, MARGIN_MODE_LABELS } from "../src/core/perps/config";

const USDL = "0x85FcD13735F4309833A503EE804ea32395851479";

describe("safeParseUnits", () => {
  it("returns 0n for empty / null / undefined / whitespace", () => {
    assert.equal(safeParseUnits(undefined, 6), 0n);
    assert.equal(safeParseUnits(null, 6), 0n);
    assert.equal(safeParseUnits("", 6), 0n);
    assert.equal(safeParseUnits("   ", 6), 0n);
    assert.equal(safeParseUnits(".", 6), 0n);
  });

  it("rejects non-numeric input safely without throwing", () => {
    assert.equal(safeParseUnits("abc", 6), 0n);
    assert.equal(safeParseUnits("1e3", 6), 0n); // no scientific notation
    assert.equal(safeParseUnits("-1.5", 6), 0n); // no negatives
  });

  it("converts exact-precision USDC (6dp) without float drift", () => {
    assert.equal(safeParseUnits("1.123456", 6), 1_123_456n);
    assert.equal(safeParseUnits("0.000001", 6), 1n);
    assert.equal(safeParseUnits("9999999.999999", 6), 9_999_999_999_999n);
  });

  it("truncates excess fractional digits (does not throw)", () => {
    // 6-decimal token; user types extra digits — drop them.
    assert.equal(safeParseUnits("1.1234567890", 6), 1_123_456n);
  });

  it("matches integer-scaled values for 18dp tokens at JS-float-breaking magnitudes", () => {
    // At 1e21 + 1, this overflows safe-integer representation in float.
    const big = "1234567890.123456789012345678";
    const got = safeParseUnits(big, 18);
    assert.equal(got, 1_234_567_890_123_456_789_012_345_678n);
  });

  it("treats `.5` and `5.` as 0.5 and 5", () => {
    assert.equal(safeParseUnits(".5", 6), 500_000n);
    assert.equal(safeParseUnits("5.", 6), 5_000_000n);
  });
});

describe("deriveImpliedCollateral", () => {
  // 1e18 = 1.0 in USD-scaled fixed-point.
  const ONE = 10n ** 18n;

  it("returns 0n for non-positive size or leverage", () => {
    assert.equal(deriveImpliedCollateral(0n, ONE, 6), 0n);
    assert.equal(deriveImpliedCollateral(ONE, 0n, 6), 0n);
    assert.equal(deriveImpliedCollateral(-1n, ONE, 6), 0n);
  });

  it("scales notional / leverage to native decimals correctly", () => {
    // 1000 USD notional at 10x leverage → 100 USDC collateral (6dp).
    const size = 1000n * ONE;
    const lev = 10n * ONE;
    assert.equal(deriveImpliedCollateral(size, lev, 6), 100_000_000n);
  });

  it("floors instead of rounding (collateral never over-debits)", () => {
    // 1 USD / 3x leverage = 0.333… USD → 333_333 micro-USDC, NOT 333_334.
    const size = ONE;
    const lev = 3n * ONE;
    assert.equal(deriveImpliedCollateral(size, lev, 6), 333_333n);
  });

  it("handles 18-decimal USID with 100x leverage cleanly", () => {
    const size = 1_234n * ONE;
    const lev = 100n * ONE;
    // 1234 / 100 = 12.34 → 12_340_000_000_000_000_000n at 18dp
    assert.equal(deriveImpliedCollateral(size, lev, 18), 12_340_000_000_000_000_000n);
  });
});

describe("collateral", () => {
  it("collateralIsUsdlOnly", () => {
    assert.deepEqual(COLLATERAL_OPTIONS, [{ label: "USDL", value: USDL }]);
    assert.deepEqual(Object.values(COLLATERAL_TOKENS), [
      { address: USDL, decimals: 6, symbol: "USDL" },
    ]);
    assert.deepEqual(MARGIN_MODE_LABELS, { 0: "Cross" });
    assert.equal(getCollateralDecimals(USDL), 6);
    assert.equal(getCollateralDecimals(USDL.toLowerCase()), 6);
  });

  it("depositAmountIsExact", () => {
    const decimals = getCollateralDecimals(USDL);
    assert.equal(safeParseUnits("0.29", decimals), 290_000n);
    assert.equal(safeParseUnits("8.2", decimals), 8_200_000n);
    assert.equal(BigInt(Math.floor(Number("8.2") * 10 ** decimals)), 8_199_999n);
  });

  it("unknownCollateralThrows", () => {
    const unknown = "0x00000000000000000000000000000000000000fF";
    assert.throws(
      () => getCollateralDecimals(unknown),
      (err: unknown) => err instanceof Error && err.message === `unknown collateral token ${unknown}`,
    );
  });
});
