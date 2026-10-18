import { StopMarketOrderCreator } from "../stopMarketOrderCreator";
import { createMockConfig } from "./testHelpers";

describe("StopMarketOrderCreator", () => {
  let orderCreator: StopMarketOrderCreator;

  beforeEach(() => {
    orderCreator = new StopMarketOrderCreator();
  });

  describe("create()", () => {
    it("should create a stop market order", () => {
      const values = {
        reduce_only: false,
        side: "BUY",
        order_type: "STOP_MARKET",
        isStopOrder: true,
        symbol: "PERP_ETH_USDC",
        visible_quantity: 1,
        order_price: "3880.03",
        order_quantity: "0.5007",
        trigger_price: "3879.7",
        total: "1942.73",
      };

      const expectedOrder = {
        symbol: "PERP_ETH_USDC",
        trigger_price: "3879.7",
        algo_type: "STOP",
        type: "MARKET",
        quantity: "0.5007",
        trigger_price_type: "MARK_PRICE",
        side: "BUY",
        reduce_only: false,
        margin_mode: "CROSS",
      };

      const createdOrder = orderCreator.create(values as any);

      expect(createdOrder).toEqual(expectedOrder);
    });
  });

  describe("validate()", () => {
    test("should return error when trigger_price is missing", async () => {
      const values = {
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

    test("should pass validation for valid trigger_price", async () => {
      const values = {
        order_quantity: "0.5",
        trigger_price: 4000,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.trigger_price).toBeUndefined();
    });

    test("should inherit baseValidate errors for order_quantity", async () => {
      const values = {
        order_quantity: undefined,
        trigger_price: 4000,
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });
  });
});
