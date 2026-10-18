import { OrderEntity, OrderSide } from "@paxeer-network/types";
import { StopLimitOrderCreator } from "../stopLimitOrderCreator";
import { createMockConfig } from "./testHelpers";

describe("StopLimitOrderCreator", () => {
  let orderCreator: StopLimitOrderCreator;

  beforeEach(() => {
    orderCreator = new StopLimitOrderCreator();
  });

  describe("create()", () => {
    it("should create a stop limit order", () => {
      const values = {
        reduce_only: false,
        side: "BUY",
        order_type: "STOP_LIMIT",
        isStopOrder: false,
        symbol: "PERP_ETH_USDC",
        visible_quantity: 1,
        order_price: "4024.21",
        order_quantity: "0.2294",
        trigger_price: "4010.96",
        total: "982.63",
      } as OrderEntity;
      const config = createMockConfig({
        maxQty: 3.289917425185755,
        markPrice: 4037.84,
      });

      const expectedOrder = {
        symbol: "PERP_ETH_USDC",
        price: "4024.21",
        quantity: "0.2294",
        reduce_only: false,
        side: "BUY",
        type: "LIMIT",
        algo_type: "STOP",
        trigger_price: "4010.96",
        trigger_price_type: "MARK_PRICE",
        margin_mode: "CROSS",
      };

      const createdOrder = orderCreator.create(values as any, config);

      expect(createdOrder).toEqual(expectedOrder);
    });

    it("should create a stop limit order with visible_quantity 0", () => {
      const values = {
        reduce_only: false,
        side: "BUY",
        order_type: "STOP_LIMIT",
        isStopOrder: true,
        symbol: "PERP_ETH_USDC",
        visible_quantity: 0,
        order_price: "3068.09",
        order_quantity: "0.2457",
        trigger_price: "3058.09",
        total: "753.82",
      } as OrderEntity;
      const config = createMockConfig({
        maxQty: 3.017443914363575,
        markPrice: 4033.12,
      });

      const expectedOrder = {
        symbol: "PERP_ETH_USDC",
        price: "3068.09",
        quantity: "0.2457",
        visible_quantity: 0,
        reduce_only: false,
        side: "BUY",
        type: "LIMIT",
        algo_type: "STOP",
        trigger_price: "3058.09",
        trigger_price_type: "MARK_PRICE",
        margin_mode: "CROSS",
      };

      const createdOrder = orderCreator.create(values as any, config);

      expect(createdOrder).toEqual(expectedOrder);
    });
  });

  describe("validate()", () => {
    test("should return error when trigger_price is missing", async () => {
      const values = {
        order_price: "4000",
        order_quantity: "0.5",
        trigger_price: undefined,
      };

      const config = createMockConfig();

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeDefined();
      expect(errors.trigger_price?.type).toBe("required");
    });

    test("should return error when trigger_price exceeds quote_max", async () => {
      const values = {
        order_price: "4000",
        order_quantity: "0.5",
        trigger_price: 150000, // Exceeds quote_max
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeDefined();
      expect(errors.trigger_price?.type).toBe("max");
    });

    test("should return error when trigger_price is below quote_min", async () => {
      const values = {
        order_price: "4000",
        order_quantity: "0.5",
        trigger_price: -100, // Below quote_min
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeDefined();
      expect(errors.trigger_price?.type).toBe("min");
    });

    test("should return error when trigger_price is 0", async () => {
      const values = {
        order_price: "4000",
        order_quantity: "0.5",
        trigger_price: 0,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeDefined();
      expect(errors.trigger_price?.type).toBe("required");
    });

    test("should return error when order_price is missing", async () => {
      const values = {
        order_price: undefined,
        order_quantity: "0.5",
        trigger_price: 4100,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("required");
    });

    test("should return error when BUY order_price exceeds price_range max based on trigger_price", async () => {
      const triggerPrice = 4000;
      const maxPrice = triggerPrice * 1.03; // 4120
      const values = {
        side: OrderSide.BUY,
        order_price: maxPrice + 100, // Exceeds max
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should return error when BUY order_price is below price_scope min based on trigger_price", async () => {
      const triggerPrice = 4000;
      const scopePrice = triggerPrice * 0.6; // 2400
      const values = {
        side: OrderSide.BUY,
        order_price: scopePrice - 100, // Below min
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("min");
    });

    test("should return error when SELL order_price is below price_range min based on trigger_price", async () => {
      const triggerPrice = 4000;
      const minPrice = triggerPrice * 0.97; // 3880
      const values = {
        side: OrderSide.SELL,
        order_price: minPrice - 100, // Below min
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("min");
    });

    test("should return error when SELL order_price exceeds price_scope max based on trigger_price", async () => {
      const triggerPrice = 4000;
      const scopePrice = triggerPrice * 1.4; // 5600
      const values = {
        side: OrderSide.SELL,
        order_price: scopePrice + 100, // Exceeds max
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should return error when order_price exceeds quote_max", async () => {
      const values = {
        order_price: 150000, // Exceeds quote_max
        order_quantity: "0.5",
        trigger_price: 4000,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should pass validation for valid BUY stop limit order", async () => {
      const triggerPrice = 4000;
      const values = {
        side: OrderSide.BUY,
        order_price: 2500, // Within valid range
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeUndefined();
      expect(errors.order_price).toBeUndefined();
    });

    test("should pass validation for valid SELL stop limit order", async () => {
      const triggerPrice = 4000;
      const values = {
        side: OrderSide.SELL,
        order_price: 4500, // Within valid range
        order_quantity: "0.5",
        trigger_price: triggerPrice,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeUndefined();
      expect(errors.order_price).toBeUndefined();
    });
  });
});
