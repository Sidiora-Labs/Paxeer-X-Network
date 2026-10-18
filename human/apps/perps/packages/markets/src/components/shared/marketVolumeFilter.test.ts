import assert from "node:assert/strict";
import { test } from "node:test";
import { filterMarketsByLowVolume } from "./marketVolumeFilter.ts";

const markets = [
  { symbol: "LOW", "24h_amount": 999.99 },
  { symbol: "BOUNDARY", "24h_amount": 1000 },
  { symbol: "HIGH", "24h_amount": 1000.01 },
  { symbol: "ZERO", "24h_amount": 0 },
  { symbol: "MISSING" },
  { symbol: "INVALID", "24h_amount": Number.NaN },
];

test("filters the searched category result at the inclusive $1,000 boundary", () => {
  const result = filterMarketsByLowVolume(markets, false);

  assert.deepEqual(
    result.visibleMarkets.map((market) => market.symbol),
    ["BOUNDARY", "HIGH"],
  );
  assert.equal(result.allHidden, false);
});

test("shows every available market when enabled", () => {
  assert.deepEqual(
    filterMarketsByLowVolume(markets, true).visibleMarkets,
    markets,
  );
});

test("distinguishes filtered matches from no matching markets", () => {
  assert.equal(
    filterMarketsByLowVolume(markets.slice(0, 1), false).allHidden,
    true,
  );
  assert.equal(filterMarketsByLowVolume([], false).allHidden, false);
});
