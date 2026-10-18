import { OrderSide, OrderType } from "@paxeer-network/types";
import { MarketOrderCreator } from "../marketOrderCreator";
import { createMockConfig, createMockOrderEntity } from "./testHelpers";

describe("MarketOrderCreator", () => {
  let marketOrderCreator: MarketOrderCreator;

  beforeEach(() => {
    marketOrderCreator = new MarketOrderCreator();
  });

  describe("create()", () => {
    test("should create a market order without order_price", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.MARKET,
        order_price: "4000",
        order_quantity: "0.5",
        total: "2000",
        trigger_price: "4100",
        isStopOrder: true,
      });

      const result = marketOrderCreator.create(values);

      expect(result.order_price).toBeUndefined();
      expect(result.total).toBeUndefined();
      expect(result.trigger_price).toBeUndefined();
      expect(result.isStopOrder).toBeUndefined();
      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.order_type).toBe(OrderType.MARKET);
      expect(result.order_quantity).toBe("0.5");
      expect(result.side).toBe(OrderSide.BUY);
    });

    test("should include slippage when provided", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        slippage: 0.5, // 0.5%
      });

      const result = marketOrderCreator.create(values);

      expect(result.slippage).toBe(0.005); // Converted to decimal (0.5 / 100)
    });

    test("should handle visible_quantity", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.MARKET,
        visible_quantity: 0,
      });

      const result = marketOrderCreator.create(values);

      expect(result.visible_quantity).toBe(0);
    });

    test("should preserve reduce_only flag", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.MARKET,
        reduce_only: true,
      });

      const result = marketOrderCreator.create(values);

      expect(result.reduce_only).toBe(true);
    });

    test("should preserve margin_mode", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.MARKET,
        margin_mode: "ISOLATED",
      });

      const result = marketOrderCreator.create(values);

      expect(result.margin_mode).toBe("ISOLATED");
    });
  });

  describe("validate()", () => {
    test("should pass validation when estSlippage is less than slippage", async () => {
      const values = {
        order_quantity: "0.5",
        slippage: 1.0, // 1%
      };

      const config = createMockConfig({
        estSlippage: 0.5, // 0.5% (will be converted to 50% in validation)
      });

      const errors = await marketOrderCreator.validate(values, config);

      // estSlippage (0.5 * 100 = 50) > slippage (1.0) should fail
      expect(errors.slippage).toBeDefined();
      expect(errors.slippage?.type).toBe("max");
    });

    test("should pass validation when estSlippage is equal to slippage", async () => {
      const values = {
        order_quantity: "0.5",
        slippage: 1.0, // 1%
      };

      const config = createMockConfig({
        estSlippage: 0.01, // 0.01 * 100 = 1.0
      });

      const errors = await marketOrderCreator.validate(values, config);

      expect(errors.slippage).toBeUndefined();
    });

    test("should pass validation when estSlippage is null", async () => {
      const values = {
        order_quantity: "0.5",
        slippage: 1.0,
      };

      const config = createMockConfig({
        estSlippage: null,
      });

      const errors = await marketOrderCreator.validate(values, config);

      expect(errors.slippage).toBeUndefined();
    });

    test("should pass validation when slippage is NaN", async () => {
      const values = {
        order_quantity: "0.5",
        slippage: "invalid" as any,
      };

      const config = createMockConfig({
        estSlippage: 0.5,
      });

      const errors = await marketOrderCreator.validate(values, config);

      expect(errors.slippage).toBeUndefined();
    });

    test("should inherit baseValidate errors for order_quantity", async () => {
      const values = {
        order_quantity: undefined,
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await marketOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });

    test("should inherit baseValidate errors for max quantity", async () => {
      const values = {
        order_quantity: "10", // Exceeds maxQty
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await marketOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("max");
    });

    test("should validate min notional from baseValidate", async () => {
      const values = {
        order_type: OrderType.MARKET, // Ensure order_type is MARKET for price calculation
        order_quantity: "0.001", // Very small quantity (0.001 * 4000 = 4 < min_notional 10)
        reduce_only: false,
      };

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await marketOrderCreator.validate(values, config);

      // Should have min notional error if order value < min_notional
      expect(errors.total).toBeDefined();
      expect(errors.total?.type).toBe("min");
    });
  });
});
