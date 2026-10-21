import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import path from "node:path";

import { SPOT_STABLECOIN_LIST, SPOT_TOKENS } from "../src/core/spot/addresses";
import {
  PRICE_SCALE,
  expectedOutFromPrice,
  limitMinOut,
  parsePrice,
  quoteDecimals,
  spotOrderRail,
} from "../src/core/spot/slippage";

const ROOT = path.resolve(__dirname, "..");
const FORM = readFileSync(path.join(ROOT, "src/widgets/spot/SpotOrderForm.tsx"), "utf8");
const E6 = 10n ** 6n;

describe("spot time in force", () => {
  it("iocBuyBoundedByLimit", () => {
    assert.equal(spotOrderRail("limit", "IOC", false), "market");
    assert.equal(spotOrderRail("limit", "FOK", false), "market");
    assert.equal(spotOrderRail("market", "GTC", false), "market");

    // 100 USDL at a 2.5 limit buys at least 40 SID, exactly.
    assert.equal(limitMinOut(100n * E6, parsePrice("2.5"), true, 6, 6), 40n * E6);

    // 100 USDL at a 3 limit: 33.3333333… SID rounds up to 33.333334.
    const amountIn = 100n * E6;
    const limit = parsePrice("3");
    const minOut = limitMinOut(amountIn, limit, true, 6, 6);
    assert.equal(minOut, 33_333_334n);
    // At minOut the price paid per SID is at or below the limit; one unit less pays above it.
    assert.ok(amountIn * PRICE_SCALE <= minOut * limit);
    assert.ok(amountIn * PRICE_SCALE > (minOut - 1n) * limit);

    // An 18-decimal base: 1 USDL at a 3 limit buys 0.333…334 of it.
    assert.equal(limitMinOut(E6, limit, true, 6, 18), 333_333_333_333_333_334n);

    // A sell at the limit receives the stable through expectedOutFromPrice, rounded up.
    const sellIn = 7n * E6;
    const sellLimit = parsePrice("1.234567");
    const floor = expectedOutFromPrice({
      amountIn: sellIn,
      price: sellLimit,
      amountInDecimals: 6,
      amountOutDecimals: 6,
    });
    assert.equal(floor, 8_641_969n);
    assert.equal(limitMinOut(sellIn, sellLimit, false, 6, 6), floor);
    assert.equal(limitMinOut(1n, parsePrice("0.5"), false, 6, 6), 1n);
    assert.equal(limitMinOut(2n * E6, parsePrice("0.5"), false, 6, 6), E6);

    assert.throws(() => limitMinOut(0n, limit, true, 6, 6), /amountIn must be positive/);
    assert.throws(() => limitMinOut(E6, 0n, true, 6, 6), /limitPrice must be positive/);
    assert.throws(() => limitMinOut(E6, limit, true, -1, 6), /decimals out of range/);
    assert.throws(() => limitMinOut(E6, limit, false, 6, 39), /decimals out of range/);

    // The form sends that bound as the market call's minimum output with no slippage.
    assert.match(FORM, /limitMinOut\(\s*stablecoinAmount,\s*parsePrice\(toPlainDecimal\(limitPrice\)\),\s*true,/);
    assert.match(FORM, /limitMinOut\(\s*tokenAmount,\s*parsePrice\(toPlainDecimal\(limitPrice\)\),\s*false,/);
    assert.match(FORM, /slippageBps: atLimit \? 0 : slippageBps/);
  });

  it("postOnlyRestsOnLimitRail", () => {
    assert.equal(spotOrderRail("limit", "GTC", true), "limit");
    assert.equal(spotOrderRail("limit", "IOC", true), "limit");
    assert.equal(spotOrderRail("limit", "FOK", true), "limit");
    assert.equal(spotOrderRail("limit", "GTC", false), "limit");
    assert.equal(spotOrderRail("stop-limit", "GTC", false), "stop");

    assert.match(FORM, /if \(postOnly && tif !== "GTC"\) setTif\("GTC"\);/);
    assert.doesNotMatch(FORM, /reduceOnly|ReduceOnly|Reduce-Only/);
  });

  it("stableDecimalsFromMetadata", () => {
    assert.equal(quoteDecimals(SPOT_STABLECOIN_LIST), SPOT_TOKENS.USDL.decimals);
    assert.equal(quoteDecimals([{ decimals: 18 }]), 18);
    assert.throws(() => quoteDecimals([{ decimals: 6 }, { decimals: 18 }]), /disagree on decimals/);
    assert.throws(() => quoteDecimals([]), /no quote stablecoin/);

    assert.match(FORM, /const decimalsStable = quoteDecimals\(SPOT_STABLECOINS\);/);
    assert.doesNotMatch(FORM, /decimalsStable = 6/);
  });
});
