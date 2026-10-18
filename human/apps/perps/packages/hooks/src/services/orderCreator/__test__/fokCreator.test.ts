import { OrderType } from "@paxeer-network/types";
import { FOKOrderCreator } from "../fokCreator";
import { LimitOrderCreator } from "../limitOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

describe("FOKOrderCreator", () => {
  let fokOrderCreator: FOKOrderCreator;

  beforeEach(() => {
    fokOrderCreator = new FOKOrderCreator();
  });

  describe("inheritance", () => {
    test("should be an instance of LimitOrderCreator", () => {
      expect(fokOrderCreator).toBeInstanceOf(LimitOrderCreator);
    });

    test("should have correct orderType", () => {
      expect(fokOrderCreator.orderType).toBe(OrderType.LIMIT);
    });
  });

  describe("create()", () => {
    test("should create a FOK order with same behavior as LimitOrderCreator", () => {
      const values = createMockPaxeerNetworkOrder({
        order_price: "4000",
        order_quantity: "0.5",
      });

      const config = createMockConfig();

      const result = fokOrderCreator.create(values, config);

      // FOK orders behave like limit orders
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

      const errors = await fokOrderCreator.validate(values, config);

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

      const errors = await fokOrderCreator.validate(values, config);

      expect(errors.order_price).toBeDefined();
      expect(errors.order_price?.type).toBe("max");
    });
  });
});
