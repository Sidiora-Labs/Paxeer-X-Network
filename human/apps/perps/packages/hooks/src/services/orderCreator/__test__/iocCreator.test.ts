import { OrderType } from "@paxeer-network/types";
import { IOCOrderCreator } from "../iocCreator";
import { LimitOrderCreator } from "../limitOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("IOCOrderCreator", () => {
  let iocOrderCreator: IOCOrderCreator;

  beforeEach(() => {
    iocOrderCreator = new IOCOrderCreator();
  });

  describe("inheritance", () => {
    test("should be an instance of LimitOrderCreator", () => {
      expect(iocOrderCreator).toBeInstanceOf(LimitOrderCreator);
    });

    test("should have correct orderType", () => {
      expect(iocOrderCreator.orderType).toBe(OrderType.LIMIT);
    });
  });

  describe("create()", () => {
    test("should create an IOC order with same behavior as LimitOrderCreator", () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "4000",
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const result = iocOrderCreator.create(values, config);

      // IOC orders behave like limit orders
      expect(result.order_price).toBe("4000");
      expect(result.order_quantity).toBe("0.5");
      expect(result.order_type).toBe(OrderType.LIMIT);
    });
  });

  describe("validate()", () => {
    test("should validate like LimitOrderCreator", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: undefined, // Missing price
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const errors = await iocOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("required");
    });

    test("should validate price range like LimitOrderCreator", async () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "150000", // Exceeds quote_max
        order_quantity: "0.5",
      });

      const config = createMockConfig({
        markPrice: 4000,
      });

      const errors = await iocOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });
  });
});
