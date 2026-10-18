import { OrderType } from "@paxeer-network/types";
import { BBOOrderCreator } from "../bboOrderCreator";
import { createMockConfig, createMockOrderEntity } from "./testHelpers";

describe("BBOOrderCreator", () => {
  let bboOrderCreator: BBOOrderCreator;

  beforeEach(() => {
    bboOrderCreator = new BBOOrderCreator();
  });

  describe("create()", () => {
    test("should create a BBO order with level field", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.ASK,
        order_quantity: "0.5",
        level: 1,
      });

      const result = bboOrderCreator.create(values);

      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.order_type).toBe(OrderType.ASK); // BBO preserves the original order_type
      expect(result.order_quantity).toBe("0.5");
      expect(result.level).toBe(1);
      expect(result.order_price).toBeUndefined(); // BBO orders don't have order_price
    });

    test("should include all required fields", () => {
      const values = createMockOrderEntity({
        order_type: OrderType.BID,
        order_quantity: "1.0",
        visible_quantity: 0,
        reduce_only: true,
        level: 2,
        margin_mode: "ISOLATED",
      });

      const result = bboOrderCreator.create(values);

      expect(result.symbol).toBeDefined();
      expect(result.order_quantity).toBeDefined();
      expect(result.visible_quantity).toBeDefined();
      expect(result.reduce_only).toBeDefined();
      expect(result.side).toBeDefined();
      expect(result.order_type).toBeDefined();
      expect(result.margin_mode).toBeDefined();
      expect(result.level).toBeDefined();
    });
  });

  describe("validate()", () => {
    test("should validate order_quantity from baseValidate", async () => {
      const values = {
        order_quantity: undefined,
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await bboOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });

    test("should validate max quantity from baseValidate", async () => {
      const values = {
        order_quantity: "10", // Exceeds maxQty
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await bboOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("max");
    });

    test("should validate min notional from baseValidate", async () => {
      const values = {
        order_type: OrderType.MARKET, // Use MARKET to trigger markPrice usage in baseValidate
        order_quantity: "0.001", // Very small quantity (0.001 * 4000 = 4 < min_notional 10)
        reduce_only: false,
      };

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await bboOrderCreator.validate(values, config);

      // Should have min notional error if order value < min_notional
      expect(errors.total).toBeDefined();
      expect(errors.total?.type).toBe("min");
    });

    test("should pass validation for valid order", async () => {
      const values = {
        order_quantity: "0.5",
        reduce_only: false,
      };

      const config = createMockConfig({
        maxQty: 3.5,
        markPrice: 4000,
      });

      const errors = await bboOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeUndefined();
    });
  });
});
