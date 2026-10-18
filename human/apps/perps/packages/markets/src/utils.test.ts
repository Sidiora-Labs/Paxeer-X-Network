import assert from "node:assert/strict";
import { test } from "node:test";
import { filterMarketsByLowVolume } from "./components/shared/marketVolumeFilter";
import { sortList } from "./utils";

const markets = [
  { symbol: "HIGH", "24h_amount": 2000 },
  { symbol: "MISSING" },
  { symbol: "MID", "24h_amount": 1500 },
  { symbol: "INVALID", "24h_amount": Number.NaN },
  { symbol: "LOW", "24h_amount": 500 },
];

test("showing low-volume markets preserves the order of visible symbols", () => {
  const sort = { sortKey: "24h_amount", sortOrder: "desc" as const };
  const hidden = sortList(
    filterMarketsByLowVolume(markets, false).visibleMarkets,
    sort,
  );
  const shown = sortList(
    filterMarketsByLowVolume(markets, true).visibleMarkets,
    sort,
  );

  assert.deepEqual(
    hidden.map((market) => market.symbol),
    ["HIGH", "MID"],
  );
  assert.deepEqual(
    shown.map((market) => market.symbol),
    ["HIGH", "MID", "LOW", "MISSING", "INVALID"],
  );
});

test("missing values stay last when sorting ascending", () => {
  const sorted = sortList(markets, {
    sortKey: "24h_amount",
    sortOrder: "asc",
  });

  assert.deepEqual(
    sorted.map((market) => market.symbol),
    ["LOW", "MID", "HIGH", "MISSING", "INVALID"],
  );
});
