import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import path from "node:path";
import { decodeFunctionData, encodeFunctionData, getAddress, type Address } from "viem";

import {
  WEI_PER_UHPX,
  buildCancelLimitOrder,
  buildCancelStopLimitOrder,
  buildCancelStopLossOrder,
  buildGetLimitOrder,
  buildGetPaxPointer,
  buildGetStopLimitOrder,
  buildGetStopLossOrder,
  buildGetUserLimitOrders,
  buildGetUserStopLimitOrders,
  buildGetUserStopLossOrders,
  buildMarketBuy,
  buildMarketSell,
  buildModifyLimitOrder,
  buildPlaceLimitBuy,
  buildPlaceLimitSell,
  buildPlaceStopLimitBuy,
  buildPlaceStopLimitSell,
  buildPlaceStopLoss,
  buildSwapExactInNative,
} from "../src/core/spot/calls";
import { SPOT_SPENDERS } from "../src/core/network/approvals";

const ROOT = path.resolve(__dirname, "..");
const MARKET: Address = "0x0000000000000000000000000000000000001021";
const ORDERS: Address = "0x0000000000000000000000000000000000001022";
const STOPS: Address = "0x0000000000000000000000000000000000001023";
const USDL: Address = "0x85FcD13735F4309833A503EE804ea32395851479";
/** A uhpx pointer address as a fixture: contracts/address-table.md records no hex value yet. */
const PAX_POINTER: Address = getAddress("0xaa00000000000000000000000000000000000001");
const USER: Address = getAddress("0x00000000000000000000000000000000000000a1");
const E18 = 10n ** 18n;
const REFERENCE_SECONDS = 1_000_000n;
const DEADLINE = REFERENCE_SECONDS + 60n;
const EXPIRY = REFERENCE_SECONDS + 86_400n;

function refusedWith(code: string): (err: unknown) => boolean {
  return (err) => {
    assert.equal((err as { code?: unknown }).code, code);
    return true;
  };
}

function roundTrips(call: { abi: readonly unknown[]; functionName: string; args: readonly unknown[] }): void {
  const data = encodeFunctionData(call as Parameters<typeof encodeFunctionData>[0]);
  const decoded = decodeFunctionData({ abi: call.abi as Parameters<typeof decodeFunctionData>[0]["abi"], data });
  assert.equal(decoded.functionName, call.functionName);
  assert.deepEqual(decoded.args ?? [], [...call.args]);
}

describe("spot call builders", () => {
  it("marketRailTargetsPecor", () => {
    const buy = buildMarketBuy({
      stablecoin: USDL,
      token: PAX_POINTER,
      stablecoinAmount: 25_000_000n,
      minOut: 9_950_000n,
      deadline: DEADLINE,
    });
    assert.equal(buy.address, MARKET);
    assert.equal(buy.functionName, "marketBuy");
    assert.deepEqual(buy.args, [USDL, PAX_POINTER, 25_000_000n, 9_950_000n, DEADLINE]);
    assert.equal("value" in buy, false);
    roundTrips(buy);

    const sell = buildMarketSell({
      token: PAX_POINTER,
      stablecoin: USDL,
      tokenAmount: 10_000_000n,
      minOut: 24_875_000n,
      deadline: DEADLINE,
    });
    assert.equal(sell.address, MARKET);
    assert.equal(sell.functionName, "marketSell");
    assert.deepEqual(sell.args, [PAX_POINTER, USDL, 10_000_000n, 24_875_000n, DEADLINE]);
    roundTrips(sell);

    assert.equal(SPOT_SPENDERS.market, MARKET);
    assert.throws(
      () => buildMarketBuy({ stablecoin: USDL, token: PAX_POINTER, stablecoinAmount: 0n, minOut: 1n, deadline: DEADLINE }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );
    assert.throws(
      () => buildMarketBuy({ stablecoin: USDL, token: PAX_POINTER, stablecoinAmount: 1n, minOut: 0n, deadline: DEADLINE }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );
    assert.throws(
      () => buildMarketSell({ token: PAX_POINTER, stablecoin: USDL, tokenAmount: 1n, minOut: 1n, deadline: 0n }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );
  });

  it("limitRailTargetsOrders", () => {
    const buy = buildPlaceLimitBuy({
      stablecoin: USDL,
      token: PAX_POINTER,
      amount: 25_000_000n,
      targetPrice: 2n * E18,
      expiresAt: EXPIRY,
    });
    assert.equal(buy.address, ORDERS);
    assert.equal(buy.functionName, "placeLimitBuy");
    assert.deepEqual(buy.args, [USDL, PAX_POINTER, 25_000_000n, 2n * E18, EXPIRY]);
    roundTrips(buy);

    const sell = buildPlaceLimitSell({
      token: PAX_POINTER,
      stablecoin: USDL,
      amount: 10_000_000n,
      targetPrice: 3n * E18,
      expiresAt: EXPIRY,
    });
    assert.equal(sell.address, ORDERS);
    assert.equal(sell.functionName, "placeLimitSell");
    assert.deepEqual(sell.args, [PAX_POINTER, USDL, 10_000_000n, 3n * E18, EXPIRY]);
    roundTrips(sell);

    const others = [
      buildCancelLimitOrder(7n),
      buildModifyLimitOrder({ orderId: 7n, newTargetPrice: E18, newExpiresAt: EXPIRY }),
      buildGetUserLimitOrders(USER),
      buildGetLimitOrder(7n),
    ];
    assert.deepEqual(
      others.map((c) => [c.address, c.functionName]),
      [
        [ORDERS, "cancelLimitOrder"],
        [ORDERS, "modifyLimitOrder"],
        [ORDERS, "getUserLimitOrders"],
        [ORDERS, "getLimitOrder"],
      ],
    );
    others.forEach(roundTrips);

    assert.equal(SPOT_SPENDERS.limit, ORDERS);
    assert.throws(
      () => buildPlaceLimitBuy({ stablecoin: USDL, token: PAX_POINTER, amount: 1n, targetPrice: 0n, expiresAt: EXPIRY }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );
    assert.throws(() => buildCancelLimitOrder(-1n), refusedWith("VALIDATION_AMOUNT_INVALID"));
  });

  it("stopRailTargetsStopOrders", () => {
    const loss = buildPlaceStopLoss({
      token: PAX_POINTER,
      stablecoin: USDL,
      tokenAmount: 10_000_000n,
      triggerPrice: (3n * E18) / 2n,
      expiresAt: EXPIRY,
    });
    assert.equal(loss.address, STOPS);
    assert.equal(loss.functionName, "placeStopLoss");
    assert.deepEqual(loss.args, [PAX_POINTER, USDL, 10_000_000n, 1_500_000_000_000_000_000n, EXPIRY]);
    roundTrips(loss);

    const buy = buildPlaceStopLimitBuy({
      stablecoin: USDL,
      token: PAX_POINTER,
      amount: 25_000_000n,
      stopPrice: 2n * E18,
      limitPrice: 2n * E18 + 1n,
      expiresAt: EXPIRY,
    });
    assert.deepEqual(buy.args, [USDL, PAX_POINTER, 25_000_000n, 2n * E18, 2n * E18 + 1n, EXPIRY]);
    const sell = buildPlaceStopLimitSell({
      token: PAX_POINTER,
      stablecoin: USDL,
      amount: 10_000_000n,
      stopPrice: 2n * E18,
      limitPrice: 2n * E18,
      expiresAt: EXPIRY,
    });
    assert.deepEqual(sell.args, [PAX_POINTER, USDL, 10_000_000n, 2n * E18, 2n * E18, EXPIRY]);

    const all = [
      buy,
      sell,
      buildCancelStopLimitOrder(3n),
      buildCancelStopLossOrder(4n),
      buildGetUserStopLimitOrders(USER),
      buildGetUserStopLossOrders(USER),
      buildGetStopLimitOrder(3n),
      buildGetStopLossOrder(4n),
    ];
    assert.deepEqual(
      all.map((c) => [c.address, c.functionName]),
      [
        [STOPS, "placeStopLimitBuy"],
        [STOPS, "placeStopLimitSell"],
        [STOPS, "cancelStopLimitOrder"],
        [STOPS, "cancelStopLossOrder"],
        [STOPS, "getUserStopLimitOrders"],
        [STOPS, "getUserStopLossOrders"],
        [STOPS, "getStopLimitOrder"],
        [STOPS, "getStopLossOrder"],
      ],
    );
    all.forEach(roundTrips);

    assert.equal(SPOT_SPENDERS.stop, STOPS);
    assert.throws(
      () =>
        buildPlaceStopLimitBuy({
          stablecoin: USDL,
          token: PAX_POINTER,
          amount: 1n,
          stopPrice: 2n * E18,
          limitPrice: 2n * E18 - 1n,
          expiresAt: EXPIRY,
        }),
      refusedWith("VALIDATION_BELOW_MIN"),
    );
    assert.throws(
      () =>
        buildPlaceStopLimitSell({
          token: PAX_POINTER,
          stablecoin: USDL,
          amount: 1n,
          stopPrice: 2n * E18,
          limitPrice: 2n * E18 + 1n,
          expiresAt: EXPIRY,
        }),
      refusedWith("VALIDATION_ABOVE_MAX"),
    );
    assert.throws(
      () =>
        buildPlaceStopLoss({ token: PAX_POINTER, stablecoin: USDL, tokenAmount: 1n, triggerPrice: 0n, expiresAt: EXPIRY }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );
  });

  it("nativeSellUsesPointer", () => {
    const sell = buildMarketSell({
      token: PAX_POINTER,
      stablecoin: USDL,
      tokenAmount: 1_000_000n,
      minOut: 1n,
      deadline: DEADLINE,
    });
    assert.equal(sell.functionName, "marketSell");
    assert.equal(sell.args[0], PAX_POINTER);
    assert.equal(sell.args[2], 1_000_000n);
    assert.notEqual(sell.args[4], 0n);
    assert.equal("value" in sell, false);

    const fromValue = buildSwapExactInNative({
      tokenOut: USDL,
      amount: 1_000_000n,
      amountOutMin: 2_487_500n,
      deadline: DEADLINE,
    });
    assert.equal(fromValue.address, MARKET);
    assert.equal(fromValue.functionName, "swapExactInNative");
    assert.deepEqual(fromValue.args, [USDL, 2_487_500n, DEADLINE]);
    assert.equal(fromValue.value, 1_000_000_000_000_000_000n);
    assert.equal(fromValue.value % WEI_PER_UHPX, 0n);
    roundTrips(fromValue);
    assert.throws(
      () => buildSwapExactInNative({ tokenOut: USDL, amount: 0n, amountOutMin: 1n, deadline: DEADLINE }),
      refusedWith("VALIDATION_AMOUNT_INVALID"),
    );

    const pointer = buildGetPaxPointer();
    assert.equal(pointer.address, MARKET);
    assert.equal(pointer.functionName, "WETH");
    assert.deepEqual(pointer.args, []);

    const hook = readFileSync(path.join(ROOT, "src/hooks/spot/useSpotOrders.ts"), "utf8");
    assert.doesNotMatch(hook, /@\/core\/spot\/native/);
    assert.doesNotMatch(hook, /WPAX9|wpax9MinimalAbi|computeWrapShortfall|shouldWrapBeforeSell/);
    assert.doesNotMatch(hook, /functionName:\s*"deposit"/);
    assert.doesNotMatch(hook, /PECOR_CONTRACTS/);
    assert.match(hook, /buildMarketSell\(\{ token: sellToken,/);
    assert.match(hook, /const sellToken = sellsPax \? await getPaxPointer\(\) : token;/);
    assert.match(hook, /buildSwapExactInNative\(/);
    for (const rail of ["market", "limit", "stop"]) {
      assert.match(hook, new RegExp(`spender: SPOT_SPENDERS\\.${rail},`));
    }
  });
});
