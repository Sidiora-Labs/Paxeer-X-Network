import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import * as config from "../src/core/perps/config";
import {
  FEE_TYPE_LABELS,
  ORDER_TYPE_COLORS,
  ORDER_TYPE_LABELS,
  PERPS_MARKETS,
  POSITION_ACTION_COPY,
  TRADE_TYPE_LABELS,
} from "../src/core/perps/config";

describe("perps configuration", () => {
  it("leverageIsOneThousandForAll", () => {
    assert.equal(PERPS_MARKETS.length, 18);
    assert.deepEqual(
      PERPS_MARKETS.map((m) => m.marketId),
      Array.from({ length: 18 }, (_, i) => i),
    );
    for (const market of PERPS_MARKETS) {
      assert.equal(market.maxLeverage, 1000, `${market.symbol} maxLeverage`);
    }
    const link = PERPS_MARKETS.find((m) => m.symbol === "LINK");
    assert.equal(link?.marketId, 4);
    assert.equal(link?.maxLeverage, 1000);
  });

  it("enumLabelsFollowPerpsNumbering", () => {
    assert.deepEqual(ORDER_TYPE_LABELS, {
      0: "Limit",
      1: "Stop-Limit",
      2: "Take Profit",
      3: "Stop Loss",
      4: "Market",
      5: "Stop Market",
    });
    assert.deepEqual(
      Object.keys(ORDER_TYPE_COLORS),
      Object.keys(ORDER_TYPE_LABELS),
    );
    assert.equal(ORDER_TYPE_COLORS[0], "text-blue-400");
    assert.equal(ORDER_TYPE_COLORS[1], "text-amber-400");
    assert.equal(ORDER_TYPE_COLORS[2], "text-emerald-500");
    assert.equal(ORDER_TYPE_COLORS[3], "text-red-500");
    assert.equal(ORDER_TYPE_COLORS[4], "text-zinc-300");
    assert.equal(ORDER_TYPE_COLORS[5], "text-orange-400");
    assert.deepEqual(TRADE_TYPE_LABELS, {
      0: "Open",
      1: "Close",
      2: "Liquidation",
      3: "PartialClose",
      4: "Increase",
      5: "ADL",
      6: "Backstop",
    });
    assert.deepEqual(FEE_TYPE_LABELS, {
      0: "Trading",
      1: "Borrowing",
      2: "Funding",
      3: "Liquidation",
      4: "Maker Rebate",
    });
  });

  it("diamondSpotTablesRemoved", () => {
    const exported = Object.keys(config);
    for (const name of [
      "SPOT_TOKENS",
      "SPOT_MARKETS_META",
      "getPerpsMarketIdForSpot",
      "getSpotMarketMeta",
    ]) {
      assert.equal(exported.includes(name), false, `${name} is exported`);
    }
    const copyKeys = Object.keys(POSITION_ACTION_COPY);
    for (const key of [
      "transferMargin",
      "successTransferMargin",
      "errorTransferMargin",
    ]) {
      assert.equal(copyKeys.includes(key), false, `${key} copy remains`);
    }
  });
});
