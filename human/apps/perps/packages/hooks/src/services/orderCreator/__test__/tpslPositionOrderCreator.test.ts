import {
  AlgoOrderRootType,
  AlgoOrderType,
  OrderSide,
  OrderType,
  PositionType,
  TriggerPriceType,
} from "@paxeer-network/types";
import { TPSLPositionOrderCreator } from "../tpslPositionOrderCreator";
import { createMockConfig } from "./testHelpers";

describe("tpslPositionOrderCreator", () => {
  let orderCreator: TPSLPositionOrderCreator;

  beforeEach(() => {
    orderCreator = new TPSLPositionOrderCreator();
  });

  describe("create()", () => {
    test("should create positional TP/SL order with only TP", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        sl_trigger_price: undefined,
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.child_orders).toBeDefined();
      expect(result.child_orders.length).toBe(1);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.TAKE_PROFIT);
      expect(result.child_orders[0].trigger_price_type).toBe(
        TriggerPriceType.MARK_PRICE,
      );
    });

    test("should create positional TP/SL order with only SL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: undefined,
        sl_trigger_price: "3900",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.child_orders.length).toBe(1);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.STOP_LOSS);
    });

    test("should create positional TP/SL order with both TP and SL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        sl_trigger_price: "3900",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.child_orders.length).toBe(2);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.TAKE_PROFIT);
      expect(result.child_orders[1].algo_type).toBe(AlgoOrderType.STOP_LOSS);
    });

    test("should use CLOSE_POSITION type when tp_order_price is not provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        tp_order_price: undefined,
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const tpOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
      );
      expect(tpOrder?.type).toBe(OrderType.CLOSE_POSITION);
    });

    test("should use LIMIT type when tp_order_price is provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        tp_order_price: "4150",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const tpOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
      );
      expect(tpOrder?.type).toBe(OrderType.LIMIT);
      expect(tpOrder?.price).toBe(4150);
    });

    test("should use CLOSE_POSITION type when sl_order_price is not provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        sl_trigger_price: "3900",
        sl_order_price: undefined,
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const slOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.STOP_LOSS,
      );
      expect(slOrder?.type).toBe(OrderType.CLOSE_POSITION);
    });

    test("should use LIMIT type when sl_order_price is provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        sl_trigger_price: "3900",
        sl_order_price: "3850",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const slOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.STOP_LOSS,
      );
      expect(slOrder?.type).toBe(OrderType.LIMIT);
      expect(slOrder?.price).toBe(3850);
    });

    test("should set algo_type to POSITIONAL_TP_SL when position_type is FULL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        position_type: PositionType.FULL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.algo_type).toBe(AlgoOrderRootType.POSITIONAL_TP_SL);
    });

    test("should set algo_type to TP_SL when position_type is PARTIAL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.algo_type).toBe(AlgoOrderRootType.TP_SL);
    });

    test("should handle undefined tp_trigger_price", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: undefined,
        sl_trigger_price: "3900",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.child_orders.length).toBe(1);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.STOP_LOSS);
    });

    test("should format trigger prices with correct decimal places", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100.123456",
        sl_trigger_price: "3900.987654",
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const tpOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
      );
      const slOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.STOP_LOSS,
      );

      // Prices should be formatted to quote_dp (2 decimal places)
      expect(typeof tpOrder?.trigger_price).toBe("number");
      expect(typeof slOrder?.trigger_price).toBe("number");
    });
  });

  describe("validate()", () => {
    test("should inherit BaseAlgoOrderCreator validation", async () => {
      const values = {
        side: OrderSide.BUY,
        quantity: "10", // Exceeds maxQty
      };

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.quantity).toBeDefined();
      expect(errors.quantity?.type).toBe("max");
    });

    test("should validate TP trigger price for BUY order", async () => {
      const values = {
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "3900", // Invalid: should be > markPrice
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for BUY order", async () => {
      const values = {
        side: OrderSide.BUY,
        quantity: "1.0",
        sl_trigger_price: "4100", // Invalid: should be < markPrice
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should validate TP trigger price for SELL order", async () => {
      const values = {
        side: OrderSide.SELL,
        quantity: "1.0",
        tp_trigger_price: "4100", // Invalid: should be < markPrice
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.tp_trigger_price).toBeDefined();
    });

    test("should validate SL trigger price for SELL order", async () => {
      const values = {
        side: OrderSide.SELL,
        quantity: "1.0",
        sl_trigger_price: "3900", // Invalid: should be > markPrice
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      expect(errors.sl_trigger_price).toBeDefined();
    });

    test("should pass validation for valid positional TP/SL order", async () => {
      const values = {
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100", // Valid: > markPrice
        sl_trigger_price: "3900", // Valid: < markPrice
        position_type: PositionType.PARTIAL,
      };

      const config = createMockConfig({
        maxQty: 3.5,
        markPrice: 4000,
      });

      const errors = await orderCreator.validate(values, config);

      // validate returns null when there are no errors
      expect(errors?.tp_trigger_price).toBeUndefined();
      expect(errors?.sl_trigger_price).toBeUndefined();
    });
  });
});
