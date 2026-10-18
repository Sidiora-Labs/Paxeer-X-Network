import { OrderSide, OrderType } from "@paxeer-network/types";
import { BracketLimitOrderCreator } from "../bracketLimitOrderCreator";
import { LimitOrderCreator } from "../limitOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("BracketLimitOrderCreator", () => {
  let bracketLimitOrderCreator: BracketLimitOrderCreator;

  beforeEach(() => {
    bracketLimitOrderCreator = new BracketLimitOrderCreator();
  });

  describe("inheritance", () => {
    test("should be an instance of LimitOrderCreator", () => {
      expect(bracketLimitOrderCreator).toBeInstanceOf(LimitOrderCreator);
    });
  });

  describe("create()", () => {
    test("should create a bracket limit order with quantity, type, price fields", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const result = bracketLimitOrderCreator.create(values, config);

      expect(result.quantity).toBe("0.5");
      expect(result.type).toBe(OrderType.LIMIT);
      expect(result.price).toBe("4000");
      expect(result.symbol).toBe("PERP_ETH_USDC");
    });

    test("should inherit LimitOrderCreator behavior", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        reduce_only: true,
      });

      const config = createMockConfig();

      const result = bracketLimitOrderCreator.create(values, config);

      expect(result.reduce_only).toBe(true);
      expect(result.side).toBe(OrderSide.BUY);
    });
  });

  describe("validate()", () => {
    test("should inherit LimitOrderCreator validation", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: undefined, // Missing price
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("required");
    });

    test("should add bracket order validation for TP/SL prices", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "3900", // Invalid: TP should be > order_price for BUY
        sl_trigger_price: "4100", // Invalid: SL should be < order_price for BUY
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      // Should have bracket order validation errors
      expect(errors.tp_trigger_price || errors.sl_trigger_price).toBeDefined();
    });

    test("should validate TP trigger price for BUY order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "3900", // Invalid: should be > 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for BUY order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.BUY,
        sl_trigger_price: "4100", // Invalid: should be < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should validate TP trigger price for SELL order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.SELL,
        tp_trigger_price: "4100", // Invalid: should be < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for SELL order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.SELL,
        sl_trigger_price: "3900", // Invalid: should be > 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should pass validation for valid bracket limit order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.LIMIT,
        order_price: "4000",
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "4100", // Valid: > 4000
        sl_trigger_price: "3900", // Valid: < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketLimitOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeUndefined();
      expect(errors.sl_trigger_price).toBeUndefined();
    });
  });
});
