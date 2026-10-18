import {
  AlgoOrderRootType,
  AlgoOrderType,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { TPSLOrderCreator } from "../tpslOrderCreator";
import { createMockConfig } from "./testHelpers";

describe("tpslOrderCreator", () => {
  let orderCreator: TPSLOrderCreator;

  beforeEach(() => {
    orderCreator = new TPSLOrderCreator();
  });

  describe("create()", () => {
    test("should create TP/SL order with only TP", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        sl_trigger_price: undefined,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.algo_type).toBe(AlgoOrderRootType.TP_SL);
      expect(result.quantity).toBe("1.0");
      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.child_orders).toBeDefined();
      expect(result.child_orders.length).toBe(1);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.TAKE_PROFIT);
      expect(result.child_orders[0].trigger_price).toBe(4100);
      expect(result.child_orders[0].type).toBe(OrderType.MARKET);
      expect(result.child_orders[0].side).toBe(OrderSide.SELL); // Opposite side
    });

    test("should create TP/SL order with only SL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: undefined,
        sl_trigger_price: "3900",
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.child_orders).toBeDefined();
      expect(result.child_orders.length).toBe(1);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.STOP_LOSS);
      expect(result.child_orders[0].trigger_price).toBe(3900);
      expect(result.child_orders[0].type).toBe(OrderType.MARKET);
    });

    test("should create TP/SL order with both TP and SL", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        sl_trigger_price: "3900",
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      expect(result.child_orders).toBeDefined();
      expect(result.child_orders.length).toBe(2);
      expect(result.child_orders[0].algo_type).toBe(AlgoOrderType.TAKE_PROFIT);
      expect(result.child_orders[1].algo_type).toBe(AlgoOrderType.STOP_LOSS);
    });

    test("should create TP order with LIMIT type when tp_order_price is provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        tp_order_price: "4150",
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const tpOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
      );
      expect(tpOrder).toBeDefined();
      expect(tpOrder?.type).toBe(OrderType.LIMIT);
      expect(tpOrder?.price).toBe(4150);
    });

    test("should create SL order with LIMIT type when sl_order_price is provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        sl_trigger_price: "3900",
        sl_order_price: "3850",
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const slOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.STOP_LOSS,
      );
      expect(slOrder).toBeDefined();
      expect(slOrder?.type).toBe(OrderType.LIMIT);
      expect(slOrder?.price).toBe(3850);
    });

    test("should create TP order with MARKET type when tp_order_price is not provided", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        tp_order_price: undefined,
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      const tpOrder = result.child_orders.find(
        (order) => order.algo_type === AlgoOrderType.TAKE_PROFIT,
      );
      expect(tpOrder).toBeDefined();
      expect(tpOrder?.type).toBe(OrderType.MARKET);
      expect(tpOrder?.price).toBeUndefined();
    });

    test("should set reduce_only to true for all child orders", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100",
        sl_trigger_price: "3900",
      };

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = orderCreator.create(values, config);

      result.child_orders.forEach((order) => {
        expect(order.reduce_only).toBe(true);
      });
    });

    test("should format trigger prices with correct decimal places", () => {
      const values = {
        symbol: "PERP_ETH_USDC",
        side: OrderSide.BUY,
        quantity: "1.0",
        tp_trigger_price: "4100.123456",
        sl_trigger_price: "3900.987654",
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
    test("no values need to validate", async () => {
      const values = {
        side: OrderSide.BUY,
      };
      const config = {
        markPrice: 60,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toBeNull();
    });
    test('no values need to validate, value is "" ', async () => {
      const values = {
        side: OrderSide.BUY,
        tp_trigger_price: "",
        sl_trigger_price: "",
      };
      const config = {
        markPrice: 60,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toBeNull();
    });

    test("buy/stop price is less than sell/stop price", async () => {
      const values = {
        tp_trigger_price: 100,
        sl_trigger_price: 70,
        side: OrderSide.BUY,
      };
      const config = {
        markPrice: 60,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        sl_trigger_price: {
          type: "max",
          message: "SL price must be less than 60",
          value: 60,
        },
      });
    });

    test("position side = “BUY” AND algo_type == “TAKE_PROFIT“, trigger_price <= mark_price", async () => {
      const values = {
        tp_trigger_price: 90,
        sl_trigger_price: 70,
        side: OrderSide.BUY,
      };
      const config = {
        markPrice: 100,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        tp_trigger_price: {
          type: "min",
          message: "TP price must be greater than 100",
          value: 100,
        },
      });
    });

    test("position side =”SELL”  AND algo_type == “STOP_LOSS“, trigger_price <= mark_price", async () => {
      const values = {
        // tp_trigger_price: 90,
        sl_trigger_price: 70,
        side: OrderSide.SELL,
      };
      const config = {
        markPrice: 100,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        sl_trigger_price: {
          type: "min",
          message: "SL price must be greater than 100",
          value: 100,
        },
      });
    });
    test("position side =”SELL”  AND algo_type == “STOP_LOSS“, trigger_price <= mark_price", async () => {
      const values = {
        tp_trigger_price: 110,
        // sl_trigger_price: 70,
        side: OrderSide.SELL,
      };
      const config = {
        markPrice: 100,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        tp_trigger_price: {
          type: "max",
          message: "TP price must be less than 100",
          value: 100,
        },
      });
    });

    test("All validate faild: BUY", async () => {
      const values = {
        tp_trigger_price: 90,
        sl_trigger_price: 110,
        side: OrderSide.BUY,
      };
      const config = {
        markPrice: 100,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        tp_trigger_price: {
          type: "min",
          message: "TP price must be greater than 100",
          value: 100,
        },
        sl_trigger_price: {
          type: "max",
          message: "SL price must be less than 100",
          value: 100,
        },
      });
    });

    test("All validate faild: SELL", async () => {
      const values = {
        tp_trigger_price: 110,
        sl_trigger_price: 90,
        side: OrderSide.SELL,
      };
      const config = {
        markPrice: 100,
        symbol: {} as any,
        maxQty: 0,
      };
      const result = await orderCreator.validate(values, config);

      expect(result).toEqual({
        tp_trigger_price: {
          type: "max",
          message: "TP price must be less than 100",
          value: 100,
        },
        sl_trigger_price: {
          type: "min",
          message: "SL price must be greater than 100",
          value: 100,
        },
      });
    });
  });
});
