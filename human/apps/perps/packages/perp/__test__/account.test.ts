import { describe, expect, it } from "@jest/globals";
import { OrderSide } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import * as account from "../src/account";

describe("account formula", () => {
  describe("totalCollateral", () => {
    const collateral = (overrides: Record<string, unknown> = {}) => ({
      holding: 100,
      pendingShort: 0,
      indexPrice: 1,
      collateralCap: -1,
      collateralRatio: new Decimal(0.9),
      isCollateral: true,
      ...overrides,
    });

    it("should include eligible non-USDC collateral when USDC is zero", () => {
      const result = account.totalCollateral({
        USDCHolding: 0,
        nonUSDCHolding: [collateral()],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(90);
    });

    it("should exclude positive assets that are not collateral", () => {
      const result = account.totalCollateral({
        USDCHolding: 0,
        nonUSDCHolding: [collateral({ isCollateral: false })],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(0);
    });

    it("should include signed pending sells in the effective quantity", () => {
      const result = account.totalCollateral({
        USDCHolding: 0,
        nonUSDCHolding: [collateral({ pendingShort: -20 })],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(72);
    });

    it("should value negative assets as full debt without cap or discount", () => {
      const result = account.totalCollateral({
        USDCHolding: 100,
        nonUSDCHolding: [
          collateral({
            holding: 10,
            pendingShort: -20,
            indexPrice: 2,
            collateralCap: 1,
            collateralRatio: new Decimal(0.5),
          }),
        ],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(80);
    });

    it("should fail closed when non-USDC debt has no valid index price", () => {
      const result = account.totalCollateral({
        USDCHolding: 100,
        nonUSDCHolding: [
          collateral({ holding: -10, indexPrice: 0, isCollateral: false }),
        ],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(0);
    });

    it("should treat a collateral cap of -1 as unlimited", () => {
      const result = account.totalCollateral({
        USDCHolding: 0,
        nonUSDCHolding: [
          collateral({
            holding: 10,
            indexPrice: 2,
            collateralRatio: new Decimal(0.8),
          }),
        ],
        unsettlementPnL: 0,
      });

      expect(result.toNumber()).toBe(16);
    });

    it("should subtract pending USDC and isolated order frozen margin", () => {
      const result = account.totalCollateral({
        USDCHolding: 100,
        nonUSDCHolding: [],
        unsettlementPnL: 0,
        usdcBalancePendingShortQty: -5,
        usdcBalanceIsolatedOrderFrozen: 20,
      });

      expect(result.toNumber()).toBe(75);
    });
  });

  describe("freeCollateralUSDCOnly", () => {
    it("should preserve the legacy metric with an unlimited collateral cap", () => {
      const result = account.freeCollateralUSDCOnly({
        freeCollateral: new Decimal(90),
        nonUSDCHolding: [
          {
            holding: 100,
            indexPrice: 1,
            collateralCap: -1,
            collateralRatio: new Decimal(0.9),
            isCollateral: true,
          },
        ],
      });

      expect(result.toNumber()).toBe(0);
    });
  });

  describe("additionalIsolatedOrderFrozenByOrders", () => {
    const rate = (leverage: number) => 1 / leverage + 0.0006;

    it("freezes only the opening remainder of a reverse order", () => {
      // 1 long position, SELL 1.5 @ 100, 10x: only 0.5 opens a short
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 1,
        newOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 1.5,
            createdTime: 2000,
          },
        ],
        pendingOrders: [],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBeCloseTo(0.5 * 100 * rate(10), 8);
    });

    it("prices closing-priority reallocation of existing reverse orders", () => {
      // 1 long position, resting SELL 1 @ 200, new SELL 1 @ 100: the cheaper
      // new order takes over closing, so the resting order becomes opening
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 1,
        newOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 1,
            createdTime: 2000,
          },
        ],
        pendingOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 200,
            quantity: 1,
            createdTime: 1000,
          },
        ],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBeCloseTo(200 * rate(10), 8);
    });

    it("prioritizes BUY orders by highest price when closing a short", () => {
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: -1,
        newOrders: [
          {
            side: OrderSide.BUY,
            referencePrice: 110,
            quantity: 1,
            createdTime: 2000,
          },
        ],
        pendingOrders: [
          {
            side: OrderSide.BUY,
            referencePrice: 90,
            quantity: 1,
            createdTime: 1000,
          },
        ],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBeCloseTo(90 * rate(10), 8);
    });

    it("breaks price ties in favor of the earlier order", () => {
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 1,
        newOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 1,
            createdTime: 2000,
          },
        ],
        pendingOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 1,
            createdTime: 1000,
          },
        ],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBeCloseTo(100 * rate(10), 8);
    });

    it("returns zero when the new order only closes the position", () => {
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 2,
        newOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 1,
            createdTime: 2000,
          },
        ],
        pendingOrders: [],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBe(0);
    });

    it("does not freeze floating-point dust for an exact close", () => {
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 0.3,
        newOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 0.2,
            createdTime: 2000,
          },
        ],
        pendingOrders: [
          {
            side: OrderSide.SELL,
            referencePrice: 100,
            quantity: 0.1,
            createdTime: 1000,
          },
        ],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBe(0);
    });

    it("freezes same-side orders in full", () => {
      const frozen = account.additionalIsolatedOrderFrozenByOrders({
        positionQty: 1,
        newOrders: [
          {
            side: OrderSide.BUY,
            referencePrice: 100,
            quantity: 2,
            createdTime: 2000,
          },
        ],
        pendingOrders: [],
        leverage: 10,
      });

      expect(frozen.toNumber()).toBeCloseTo(200 * rate(10), 8);
    });
  });

  describe("isolated frozen precision", () => {
    it("rounds the leverage inverse up to 10 decimals before adding the fee buffer", () => {
      // 1/3 → 0.3333333334, then + 0.0006
      expect(account.isolatedMarginRate({ leverage: 3 }).toString()).toBe(
        "0.3339333334",
      );
      // exact division stays unchanged
      expect(account.isolatedMarginRate({ leverage: 10 }).toString()).toBe(
        "0.1006",
      );
    });

    it("rounds each order's frozen amount up to USDC precision", () => {
      const frozen = account.isolatedPendingOrdersFrozen({
        positionQty: 1,
        orders: [
          {
            side: OrderSide.SELL,
            referencePrice: 1.0000001,
            quantity: 2,
            createdTime: 1000,
          },
        ],
        leverage: 100,
      });

      // opening remainder 1: 1.0000001 * 1 * 0.0106 = 0.01060000106 → 0.010601
      expect(frozen.toString()).toBe("0.010601");
    });

    it("rounds per order before summing", () => {
      const frozen = account.isolatedPendingOrdersFrozen({
        positionQty: 0,
        orders: [
          {
            side: OrderSide.BUY,
            referencePrice: 1.0000001,
            quantity: 1,
            createdTime: 1000,
          },
          {
            side: OrderSide.BUY,
            referencePrice: 1.0000001,
            quantity: 1,
            createdTime: 2000,
          },
        ],
        leverage: 100,
      });

      // each order rounds to 0.010601; total 0.021202, not a rounded 0.021201
      expect(frozen.toString()).toBe("0.021202");
    });
  });

  describe("maxQtyForIsolatedMargin", () => {
    it("should reserve fee buffer and safety factor for same-direction orders", () => {
      const maxQty = account.maxQtyForIsolatedMargin({
        symbol: "PERP_BTC_USDC",
        orderSide: OrderSide.BUY,
        currentOrderReferencePrice: 50000,
        availableBalance: 100,
        leverage: 10,
        baseIMR: 0.1,
        IMR_Factor: 0.0000001,
        markPrice: 50000,
        positionQty: 0,
        pendingLongOrders: [],
        pendingSellOrders: [],
        isoOrderFrozenLong: 0,
        isoOrderFrozenShort: 0,
        symbolMaxNotional: 10000000,
      });

      expect(maxQty).toBeCloseTo(0.019781312127236578, 12);
    });

    it("should subtract existing frozen margin in flip binary search", () => {
      const maxQty = account.maxQtyForIsolatedMargin({
        symbol: "PERP_BTC_USDC",
        orderSide: OrderSide.BUY,
        currentOrderReferencePrice: 100,
        availableBalance: 100,
        leverage: 10,
        baseIMR: 0.1,
        IMR_Factor: 0.0000001,
        markPrice: 100,
        positionQty: -1,
        pendingLongOrders: [{ referencePrice: 100, quantity: 2 }],
        pendingSellOrders: [],
        isoOrderFrozenLong: 20.12,
        isoOrderFrozenShort: 0,
        symbolMaxNotional: 1000,
        epsilon: 0.000001,
      });

      expect(maxQty).toBeCloseTo(9.940357852882704, 6);
    });

    it("should only freeze the opening remainder for reverse orders with per-order data", () => {
      const maxQty = account.maxQtyForIsolatedMargin({
        symbol: "PERP_BTC_USDC",
        orderSide: OrderSide.SELL,
        currentOrderReferencePrice: 100,
        availableBalance: 0.5 * 100 * 0.1006,
        leverage: 10,
        baseIMR: 0.1,
        IMR_Factor: 0.0000001,
        markPrice: 100,
        positionQty: 1,
        pendingLongOrders: [],
        pendingSellOrders: [],
        isoOrderFrozenLong: 0,
        isoOrderFrozenShort: 0,
        isolatedPendingOrders: [],
        symbolMaxNotional: 1_000_000,
        epsilon: 0.000001,
      });

      // 1 closes the long position, frozen accrues on the remainder only
      expect(maxQty).toBeCloseTo(1.5, 4);
    });
  });
});
