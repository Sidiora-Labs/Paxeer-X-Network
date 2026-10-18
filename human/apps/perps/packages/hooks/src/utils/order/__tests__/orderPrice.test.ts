import { MarginMode, OrderSide, OrderType } from "@paxeer-network/types";
import { getOrderReferencePriceFromOrder } from "../orderPrice";

describe("getOrderReferencePriceFromOrder", () => {
  it("uses the submitted LIMIT price while the orderbook is unavailable", () => {
    expect(
      getOrderReferencePriceFromOrder(
        {
          order_type: OrderType.LIMIT,
          side: OrderSide.BUY,
          order_price: "99",
          margin_mode: MarginMode.ISOLATED,
        },
        [],
      ),
    ).toBe(99);
  });

  it("still requires the orderbook for a BBO order", () => {
    expect(
      getOrderReferencePriceFromOrder(
        {
          order_type: OrderType.LIMIT,
          order_type_ext: OrderType.ASK,
          side: OrderSide.BUY,
          order_price: "99",
          margin_mode: MarginMode.ISOLATED,
        },
        [],
      ),
    ).toBeNull();
  });

  it("uses mark price and price range while the orderbook is unavailable", () => {
    expect(
      getOrderReferencePriceFromOrder(
        {
          order_type: OrderType.MARKET,
          side: OrderSide.BUY,
          margin_mode: MarginMode.ISOLATED,
        },
        [],
        {
          markPrice: 100.123,
          priceRange: 0.05,
          pricePrecision: 2,
        },
      ),
    ).toBe(105.12);
  });

  it("returns null without an orderbook when market pricing context is missing", () => {
    expect(
      getOrderReferencePriceFromOrder(
        {
          order_type: OrderType.MARKET,
          side: OrderSide.BUY,
          margin_mode: MarginMode.ISOLATED,
        },
        [],
      ),
    ).toBeNull();
  });
});
