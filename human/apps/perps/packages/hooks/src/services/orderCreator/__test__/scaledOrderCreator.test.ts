import { DistributionType, OrderSide, OrderType } from "@paxeer-network/types";
import { ScaledOrderCreator } from "../scaledOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("ScaledOrderCreator", () => {
  let scaledOrderCreator: ScaledOrderCreator;

  beforeEach(() => {
    scaledOrderCreator = new ScaledOrderCreator();
  });

  describe("create()", () => {
    test("should create a scaled order with orders array", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
        skew: undefined,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = scaledOrderCreator.create(values, config);

      expect(result.symbol).toBe("PERP_ETH_USDC");
      expect(result.order_type).toBe(OrderType.SCALED);
      expect(result.order_quantity).toBe("1.0");
      expect(result.total_orders).toBe("5");
      expect(result.distribution_type).toBe(DistributionType.LINEAR);
      expect(result.orders).toBeDefined();
      expect(Array.isArray(result.orders)).toBe(true);
    });

    test("should include skew when distribution_type is CUSTOM", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.CUSTOM,
        skew: "50",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = scaledOrderCreator.create(values, config);

      expect(result.skew).toBe("50");
      expect(result.distribution_type).toBe(DistributionType.CUSTOM);
    });

    test("should generate orders with correct structure", () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "3",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = scaledOrderCreator.create(values, config);

      expect(result.orders).toBeDefined();
      if (result.orders && result.orders.length > 0) {
        const firstOrder = result.orders[0];
        expect(firstOrder.symbol).toBe("PERP_ETH_USDC");
        expect(firstOrder.order_type).toBe(OrderType.LIMIT);
        expect(firstOrder.order_quantity).toBeDefined();
        expect(firstOrder.order_price).toBeDefined();
      }
    });
  });

  describe("validate()", () => {
    test("should return error when total_orders is missing", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: undefined,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.total_orders).toBeDefined();
      expect(errors.total_orders?.type).toBe("required");
    });

    test("should return error when total_orders is less than 2", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "1",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.total_orders).toBeDefined();
      expect(errors.total_orders?.type).toBe("range");
    });

    test("should return error when total_orders is greater than 20", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "21",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.total_orders).toBeDefined();
      expect(errors.total_orders?.type).toBe("range");
    });

    test("should return error when skew is missing for CUSTOM distribution", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.CUSTOM,
        skew: undefined,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.skew).toBeDefined();
      expect(errors.skew?.type).toBe("required");
    });

    test("should return error when skew is less than or equal to 0", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.CUSTOM,
        skew: "0",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.skew).toBeDefined();
      expect(errors.skew?.type).toBe("min");
    });

    test("should return error when skew is greater than 100", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.CUSTOM,
        skew: "101",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.skew).toBeDefined();
      expect(errors.skew?.type).toBe("max");
    });

    test("should return error when start_price is below min price", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        start_price: "2000", // Below min price
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.start_price).toBeDefined();
      expect(errors.start_price?.type).toBe("min");
    });

    test("should return error when start_price exceeds max price", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        start_price: "5000", // Exceeds max price
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.start_price).toBeDefined();
      expect(errors.start_price?.type).toBe("max");
    });

    test("should return error when end_price is below min price", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "2000", // Below min price
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.end_price).toBeDefined();
      expect(errors.end_price?.type).toBe("min");
    });

    test("should return error when end_price exceeds max price", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        side: OrderSide.BUY,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "5000", // Exceeds max price
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.end_price).toBeDefined();
      expect(errors.end_price?.type).toBe("max");
    });

    test("should return error when order_quantity is missing", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: undefined,
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });

    test("should return error when order_quantity exceeds maxQty", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "10", // Exceeds maxQty
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        maxQty: 3.5,
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("max");
      expect(errors.total).toBeDefined();
      expect(errors.total?.type).toBe("max");
    });

    test("should return error when total is missing", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
        total: undefined,
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.total).toBeDefined();
      expect(errors.total?.type).toBe("required");
    });

    test("should skip price validation when markPrice is empty", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "2000", // Would normally be invalid
        end_price: "5000", // Would normally be invalid
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
      });

      const config = createMockConfig({
        markPrice: undefined as any,
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.start_price).toBeUndefined();
      expect(errors.end_price).toBeUndefined();
    });

    test("should pass validation for valid scaled order", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_type: OrderType.SCALED,
        order_quantity: "1.0",
        start_price: "3900",
        end_price: "4100",
        total_orders: "5",
        distribution_type: DistributionType.LINEAR,
        total: "4000",
      });

      const config = createMockConfig({
        maxQty: 3.5,
        markPrice: 4000,
        askAndBid: [4100, 3900],
      });

      const errors = await scaledOrderCreator.validate(values, config);

      expect(errors.total_orders).toBeUndefined();
      expect(errors.skew).toBeUndefined();
      expect(errors.start_price).toBeUndefined();
      expect(errors.end_price).toBeUndefined();
    });
  });
});
