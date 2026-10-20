// Quote mapping tests: the 0x1015 quoteMarket fallback and the four-output quotePartialClose read.
// Run via `node --test --import tsx tests/quoter.test.ts`.

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import { decodeFunctionResult, encodeFunctionResult } from "viem";

import { PerpsPrecompileABI } from "../src/core/network/abi/PerpsPrecompile";
import { marketQuoteFromPrecompile, partialCloseQuoteFromPrecompile } from "../src/core/perps/quoteFallback";

const E18 = 10n ** 18n;

describe("marketQuoteFromPrecompile", () => {
  const fixture = {
    indexPrice: 95_000n * E18,
    markPrice: 95_012n * E18,
    oracleTWAP: 94_990n * E18,
    fundingRatePerSecond: -125_000_000n,
    fundingRate24h: -10_800_000_000_000n,
    longOI: 1_250_000n * E18,
    shortOI: 980_000n * E18,
    maxLeverage: 1000n,
    maintenanceMarginBps: 50n,
    enabled: true,
    priceStale: false,
  } as const;

  const decoded = decodeFunctionResult({
    abi: PerpsPrecompileABI,
    functionName: "quoteMarket",
    data: encodeFunctionResult({ abi: PerpsPrecompileABI, functionName: "quoteMarket", result: fixture }),
  });

  it("carries every field of the decoded tuple unchanged", () => {
    assert.deepEqual(marketQuoteFromPrecompile(decoded), {
      indexPrice: 95_000n * E18,
      markPrice: 95_012n * E18,
      oracleTWAP: 94_990n * E18,
      fundingRatePerSecond: -125_000_000n,
      fundingRate24h: -10_800_000_000_000n,
      longOI: 1_250_000n * E18,
      shortOI: 980_000n * E18,
      maxLeverage: 1000n,
      maintenanceMarginBps: 50n,
      enabled: true,
      priceStale: false,
    });
  });

  it("keeps the returned leverage cap and margin, not the fixed 1000 bps", () => {
    const quote = marketQuoteFromPrecompile(decoded);
    assert.equal(quote.maxLeverage, 1000n);
    assert.equal(quote.maintenanceMarginBps, 50n);
    assert.notEqual(quote.maintenanceMarginBps, 1000n);
  });

  it("passes disabled and stale flags through", () => {
    const stale = decodeFunctionResult({
      abi: PerpsPrecompileABI,
      functionName: "quoteMarket",
      data: encodeFunctionResult({
        abi: PerpsPrecompileABI,
        functionName: "quoteMarket",
        result: { ...fixture, enabled: false, priceStale: true },
      }),
    });
    const quote = marketQuoteFromPrecompile(stale);
    assert.equal(quote.enabled, false);
    assert.equal(quote.priceStale, true);
  });
});

describe("partialCloseQuoteFromPrecompile", () => {
  it("decodes the four outputs of quotePartialClose by position", () => {
    const data = encodeFunctionResult({
      abi: PerpsPrecompileABI,
      functionName: "quotePartialClose",
      result: [96_500n * E18, -15n * E18, 600_000n, 984_400_000n],
    });
    const decoded = decodeFunctionResult({ abi: PerpsPrecompileABI, functionName: "quotePartialClose", data });
    assert.equal(decoded.length, 4);
    assert.deepEqual(partialCloseQuoteFromPrecompile(decoded), {
      exitPrice: 96_500n * E18,
      closedPnl: -15n * E18,
      fee: 600_000n,
      estimatedPayout: 984_400_000n,
    });
  });

  it("encodes the call as quotePartialClose(uint256,uint256)", () => {
    const fn = PerpsPrecompileABI.find((item) => item.type === "function" && item.name === "quotePartialClose");
    assert.ok(fn && fn.type === "function");
    assert.deepEqual(
      fn.inputs.map((input) => [input.name, input.type]),
      [
        ["positionId", "uint256"],
        ["closeSizeUsd", "uint256"],
      ],
    );
    assert.deepEqual(
      fn.outputs.map((output) => [output.name, output.type]),
      [
        ["exitPrice", "uint256"],
        ["closedPnl", "int256"],
        ["fee", "uint256"],
        ["estimatedPayout", "uint256"],
      ],
    );
  });
});
