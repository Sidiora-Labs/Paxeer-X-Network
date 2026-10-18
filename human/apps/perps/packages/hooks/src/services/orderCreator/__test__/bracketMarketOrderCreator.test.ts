import { OrderSide, OrderType } from "@paxeer-network/types";
import { BracketMarketOrderCreator } from "../bracketMarketOrderCreator";
import { MarketOrderCreator } from "../marketOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("BracketMarketOrderCreator", () => {
  let bracketMarketOrderCreator: BracketMarketOrderCreator;

  beforeEach(() => {
    bracketMarketOrderCreator = new BracketMarketOrderCreator();
  });

  describe("inheritance", () => {
    test("should be an instance of MarketOrderCreator", () => {
      expect(bracketMarketOrderCreator).toBeInstanceOf(MarketOrderCreator);
    });
  });

  describe("create()", () => {
    test("should create a bracket market order with quantity, type, price fields", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
      });

      const result = bracketMarketOrderCreator.create(values);

      expect(result.quantity).toBe("0.5");
      expect(result.type).toBe(OrderType.MARKET);
      expect(result.price).toBeUndefined(); // Market orders don't have price
      expect(result.symbol).toBe("PERP_ETH_USDC");
    });

    test("should inherit MarketOrderCreator behavior", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        reduce_only: true,
      });

      const result = bracketMarketOrderCreator.create(values);

      expect(result.reduce_only).toBe(true);
      expect(result.side).toBe(OrderSide.BUY);
      expect(result.order_price).toBeUndefined();
    });
  });

  describe("validate()", () => {
    test("should inherit MarketOrderCreator validation", async () => {
      const values = {
        order_quantity: undefined,
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });

    test("should add bracket order validation for TP/SL prices", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "3900", // Invalid: TP should be > markPrice for BUY
        sl_trigger_price: "4100", // Invalid: SL should be < markPrice for BUY
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      // Should have bracket order validation errors
      expect(errors.tp_trigger_price || errors.sl_trigger_price).toBeDefined();
    });

    test("should validate TP trigger price for BUY order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "3900", // Invalid: should be > 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for BUY order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.BUY,
        sl_trigger_price: "4100", // Invalid: should be < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should validate TP trigger price for SELL order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.SELL,
        tp_trigger_price: "4100", // Invalid: should be < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for SELL order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.SELL,
        sl_trigger_price: "3900", // Invalid: should be > 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should pass validation for valid bracket market order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.MARKET,
        order_quantity: "0.5",
        side: OrderSide.BUY,
        tp_trigger_price: "4100", // Valid: > 4000
        sl_trigger_price: "3900", // Valid: < 4000
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await bracketMarketOrderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeUndefined();
      expect(errors.sl_trigger_price).toBeUndefined();
    });
  });
});
