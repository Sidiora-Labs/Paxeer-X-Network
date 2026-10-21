// Spot quoter read builders: every read of useSpotQuoter targets 0x1024 with the selector of contracts/spot.md.
// Run via `node --test --import tsx tests/spot-quoter.test.ts`.

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import { decodeFunctionData, encodeFunctionData, getAddress, slice, type Address } from "viem";

import {
  buildBatchQuote,
  buildGetAllLiquidityInfo,
  buildGetLiquidityInfo,
  buildGetTWAP,
  buildGetTokenPrice,
  buildQuoteExactIn,
  buildQuoteExactOut,
  buildQuoteMarketBuy,
  buildQuoteMarketSell,
} from "../src/core/spot/quoterCalls";

const QUOTER: Address = "0x0000000000000000000000000000000000001024";
const USDL: Address = "0x85FcD13735F4309833A503EE804ea32395851479";
const TOKEN: Address = getAddress("0x00000000000000000000000000000000000000aa");
const OTHER: Address = getAddress("0x00000000000000000000000000000000000000bb");

const cases = [
  {
    name: "quoteMarketBuy",
    selector: "0x6f6ac6a2",
    built: buildQuoteMarketBuy(USDL, TOKEN, 25_000_000n),
    args: [USDL, TOKEN, 25_000_000n],
  },
  {
    name: "quoteMarketSell",
    selector: "0x5323412e",
    built: buildQuoteMarketSell(TOKEN, USDL, 1_000_000n),
    args: [TOKEN, USDL, 1_000_000n],
  },
  {
    name: "quoteExactIn",
    selector: "0x12be4fe0",
    built: buildQuoteExactIn(TOKEN, OTHER, 7n),
    args: [TOKEN, OTHER, 7n],
  },
  {
    name: "quoteExactOut",
    selector: "0x7020df95",
    built: buildQuoteExactOut(TOKEN, OTHER, 9n),
    args: [TOKEN, OTHER, 9n],
  },
  {
    name: "batchQuote",
    selector: "0x3425eb94",
    built: buildBatchQuote([
      { tokenIn: USDL, tokenOut: TOKEN, amount: 25_000_000n, isExactIn: true },
      { tokenIn: TOKEN, tokenOut: USDL, amount: 3n, isExactIn: false },
    ]),
    args: [
      [
        { tokenIn: USDL, tokenOut: TOKEN, amount: 25_000_000n, isExactIn: true },
        { tokenIn: TOKEN, tokenOut: USDL, amount: 3n, isExactIn: false },
      ],
    ],
  },
  { name: "getTokenPrice", selector: "0xd02641a0", built: buildGetTokenPrice(TOKEN), args: [TOKEN] },
  { name: "getLiquidityInfo", selector: "0x7c0f8456", built: buildGetLiquidityInfo(USDL), args: [USDL] },
  { name: "getAllLiquidityInfo", selector: "0xe9505c98", built: buildGetAllLiquidityInfo(), args: [] },
  { name: "getTWAP", selector: "0x6f8c10a8", built: buildGetTWAP(TOKEN, 3600n), args: [TOKEN, 3600n] },
] as const;

describe("spot quoter builders", () => {
  it("cover every quoter read of useSpotQuoter", () => {
    assert.deepEqual(
      cases.map((c) => c.name),
      [
        "quoteMarketBuy",
        "quoteMarketSell",
        "quoteExactIn",
        "quoteExactOut",
        "batchQuote",
        "getTokenPrice",
        "getLiquidityInfo",
        "getAllLiquidityInfo",
        "getTWAP",
      ],
    );
  });

  for (const c of cases) {
    it(`${c.name} targets 0x1024 with selector ${c.selector}`, () => {
      assert.equal(c.built.address.toLowerCase(), QUOTER.toLowerCase());
      assert.equal(c.built.functionName, c.name);
      const data = encodeFunctionData(c.built as Parameters<typeof encodeFunctionData>[0]);
      assert.equal(slice(data, 0, 4), c.selector);
      const decoded = decodeFunctionData({ abi: c.built.abi, data });
      assert.equal(decoded.functionName, c.name);
      assert.deepEqual(decoded.args ?? [], c.args);
    });
  }
});
