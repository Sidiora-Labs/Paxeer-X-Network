import { OrderSide, PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderType } from "@paxeer-network/types";
import { OrderEntity } from "@paxeer-network/types";
import { ValuesDepConfig } from "../interface";
import { LimitOrderCreator } from "../limitOrderCreator";
import {
  createMockConfig,
  createMockOrderEntity,
  createMockPaxeerNetworkOrder,
} from "./testHelpers";

describe("LimitOrderCreator", () => {
  let limitOrderCreator: LimitOrderCreator;

  beforeEach(() => {
    limitOrderCreator = new LimitOrderCreator();
  });

  describe("create()", () => {
    test("should create a limit order with the provided values", () => {
      const values = createMockOrderEntity({
        order_price: "4015.41",
        order_quantity: "0.2936",
      });

      const config = createMockConfig({
        maxQty: 3.397012059831987,
        markPrice: 4019.75,
      });

      const expectedOrder = {
        symbol: "PERP_ETH_USDC",
        order_type: "LIMIT",
        side: "BUY",
        reduce_only: false,
        order_quantity: "0.2936",
        order_price: "4015.41",
        margin_mode: "CROSS",
      };

      const createdOrder = limitOrderCreator.create(values, config);

      expect(createdOrder).toEqual(expectedOrder);
    });

    test("should convert total to order_quantity when order_quantity is missing", () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "4000",
        order_quantity: undefined,
        total: "1000",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const result = limitOrderCreator.create(values, config);

      expect(result.order_quantity).toBeDefined();
      expect(result.order_price).toBe("4000");
    });
  });

  describe("validate()", () => {
    test("should return error when order_price is missing", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: undefined,
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("required");
    });

    test("should return error when order_price exceeds quote_max", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "150000", // Exceeds quote_max of 100000
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should return error when order_price is below quote_min", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "-100", // Below quote_min of 0
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("min");
    });

    test("should return error when BUY order_price exceeds price_range max", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.BUY,
        order_price: "4200", // Exceeds maxPrice (4000 * 1.03 = 4120)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should return error when BUY order_price is below price_scope min", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.BUY,
        order_price: "2200", // Below scopePrice (4000 * 0.6 = 2400)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("min");
    });

    test("should return error when SELL order_price is below price_range min", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.SELL,
        order_price: "3800", // Below minPrice (4000 * 0.97 = 3880)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("min");
    });

    test("should return error when SELL order_price exceeds price_scope max", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.SELL,
        order_price: "5700", // Exceeds scopePrice (4000 * 1.4 = 5600)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });

    test("should pass validation for valid BUY order_price within range", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.BUY,
        order_price: "2500", // Within valid range (2400-4120)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeUndefined();
    });

    test("should pass validation for valid SELL order_price within range", async () => {
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.SELL,
        order_price: "4500", // Within valid range (3880-5600)
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_price).toBeUndefined();
    });

    test("should pass validation at boundary values", async () => {
      // Test at exact maxPrice for BUY
      const maxPrice = 4000 * 1.03; // 4120
      const values = createMockPaxeerNetworkOrder({
        side: OrderSide.BUY,
        order_price: maxPrice.toString(),
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
        symbol: createMockConfig().symbol,
      });

      const errors = await limitOrderCreator.validate(values, config);

      // Should pass as it's at the boundary
      expect(errors.order_price).toBeUndefined();
    });

    test("should validate order_quantity from baseValidate", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "4000",
        order_quantity: undefined, // Missing quantity
        total: undefined, // Also remove total to trigger required error
      });

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("required");
    });

    test("should validate order_quantity exceeds maxQty", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "4000",
        order_quantity: "10", // Exceeds maxQty of 3.5
      });

      const config = createMockConfig({
        maxQty: 3.5,
      });

      const errors = await limitOrderCreator.validate(values, config);

      expect(errors.order_quantity).toBeDefined();
      expect(errors.order_quantity?.type).toBe("max");
    });
  });
});
