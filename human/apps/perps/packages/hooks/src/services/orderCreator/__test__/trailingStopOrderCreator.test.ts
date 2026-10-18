import {
  AlgoOrderRootType,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { TrailingStopOrderCreator } from "../trailingStopOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("TrailingStopOrderCreator", () => {
  let trailingStopOrderCreator: TrailingStopOrderCreator;

  beforeEach(() => {
    trailingStopOrderCreator = new TrailingStopOrderCreator();
  });

  describe("create()", () => {
    test("should create a trailing stop order with correct structure", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_value: "100",
        callback_rate: "1.5", // 1.5%
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = trailingStopOrderCreator.create(values, config);

      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.algo_type).toBe(AlgoOrderRootType.TRAILING_STOP);
      expect(result.type).toBe(OrderType.MARKET);
      expect(result.trigger_price_type).toBe("MARK_PRICE");
      expect(result.quantity).toBe("1.0");
      expect(result.activated_price).toBe("4100");
      expect(result.callback_value).toBe("100");
      expect(result.callback_rate).toBe("0.015"); // Converted from 1.5% to 0.015
    });

    test("should convert callback_rate from percentage to decimal", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        callback_rate: "2.5", // 2.5%
      });

      const config = createMockConfig();

      const result = trailingStopOrderCreator.create(values, config);

      expect(result.callback_rate).toBe("0.025"); // 2.5 / 100 = 0.025
    });

    test("should handle undefined callback_rate", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        callback_rate: undefined,
      });

      const config = createMockConfig();

      const result = trailingStopOrderCreator.create(values, config);

      expect(result.callback_rate).toBeUndefined();
    });

    test("should include all required fields", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_value: "100",
        reduce_only: true,
        margin_mode: "ISOLATED",
        visible_quantity: 0,
      });

      const config = createMockConfig();

      const result = trailingStopOrderCreator.create(values, config);

      expect(result.side).toBeDefined();
      expect(result.reduce_only).toBeDefined();
      expect(result.margin_mode).toBeDefined();
      expect(result.visible_quantity).toBeDefined();
    });
  });

  describe("validate()", () => {
    test("should return error when activated_price is below min price for BUY", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        activated_price: "0", // Below min price (0 triggers min error)
        callback_value: "50",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeDefined();
      expect(errors.activated_price?.type).toBe("min");
    });

    test("should return error when activated_price exceeds max price for BUY", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        activated_price: "5000", // Exceeds markPrice (max for BUY)
        callback_value: "50",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeDefined();
      expect(errors.activated_price?.type).toBe("max");
    });

    test("should return error when activated_price is below min price for SELL", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        side: OrderSide.SELL,
        order_quantity: "1.0",
        activated_price: "3000", // Below markPrice (min for SELL)
        callback_value: "50",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeDefined();
      expect(errors.activated_price?.type).toBe("min");
    });

    test("should return error when activated_price is 0", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "0",
        callback_value: "50",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeDefined();
      expect(errors.activated_price?.type).toBe("min");
    });

    test("should return error when both callback_value and callback_rate are missing", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_value: undefined,
        callback_rate: undefined,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.callback_value).toBeDefined();
      expect(errors.callback_value?.type).toBe("required");
      expect(errors.callback_rate).toBeDefined();
      expect(errors.callback_rate?.type).toBe("required");
    });

    test("should return error when callback_value is 0", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_value: "0",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.callback_value).toBeDefined();
      expect(errors.callback_value?.type).toBe("min");
    });

    test("should return error when callback_value exceeds markPrice", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_value: "5000", // Exceeds markPrice
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.callback_value).toBeDefined();
      expect(errors.callback_value?.type).toBe("range");
    });

    test("should return error when callback_rate is less than 0.1%", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_rate: "0.05", // 0.05% < 0.1%
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.callback_rate).toBeDefined();
      expect(errors.callback_rate?.type).toBe("range");
    });

    test("should return error when callback_rate exceeds 5%", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: "1.0",
        activated_price: "4100",
        callback_rate: "6", // 6% > 5%
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.callback_rate).toBeDefined();
      expect(errors.callback_rate?.type).toBe("range");
    });

    test("should pass validation for valid trailing stop order with callback_value", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        activated_price: "3900", // Valid for BUY
        callback_value: "100", // Valid (0 < 100 < 4000)
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeUndefined();
      expect(errors.callback_value).toBeUndefined();
    });

    test("should pass validation for valid trailing stop order with callback_rate", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        side: OrderSide.SELL,
        order_quantity: "1.0",
        activated_price: "4100", // Valid for SELL
        callback_rate: "2", // Valid (0.1% < 2% < 5%)
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.activated_price).toBeUndefined();
      expect(errors.callback_rate).toBeUndefined();
    });

    test("should inherit baseValidate errors for order_quantity", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.TRAILING_STOP,
        order_quantity: undefined,
        total: undefined, // Also remove total to trigger required error
        activated_price: "4100",
        callback_value: "100",
      });

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await trailingStopOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });
  });
});
