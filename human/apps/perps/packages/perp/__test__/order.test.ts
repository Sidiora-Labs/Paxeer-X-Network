import { describe, expect, it, test } from "@jest/globals";
import { OrderSide, OrderType } from "@paxeer-network/types";
import {
  estLeverage,
  estLiqPrice,
  estLiqPriceIsolated,
  getOrderReferencePrice,
  orderFee,
} from "../src/order";

describe("order", () => {
  describe("estLeverage", () => {
    it("should calculate the estimated leverage correctly: Market", () => {
      const inputs = {
        totalCollateral: 1981.66,
        positions: [
          {
            symbol: "BTC",
            position_qty: 0.2,
            mark_price: 25986.2,
          },
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
          },
        ],
        newOrder: {
          symbol: "BTC",
          qty: 0.1,
          price: 26000,
        },
      };
      expect(estLeverage(inputs)).toBe(6.42);
    });

    it("should calculate the estimated leverage correctly: Limit order 1", () => {
      const inputs = {
        totalCollateral: 1981.66,
        positions: [
          {
            symbol: "BTC",
            position_qty: 0.2,
            mark_price: 25986.2,
          },
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
          },
        ],
        newOrder: {
          symbol: "BTC",
          qty: 0.1,
          price: 25000,
        },
      };
      expect(estLeverage(inputs)).toBe(6.36);
    });

    it(" estimated leverage correctly: ====>>>>>", () => {
      const inputs = {
        totalCollateral: 1981.66,
        positions: [
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
          },
        ],
        newOrder: {
          symbol: "ETH",
          qty: -3,
          price: 1638.41,
        },
      };
      expect(estLeverage(inputs)).toBe(4.96);
    });
  });

  describe("order fee", () => {
    it("should calculate the order fee correctly", () => {
      const inputs = {
        qty: 0.1,
        price: 26000,
        futuresTakeFeeRate: 0.0006,
      };
      expect(orderFee(inputs)).toBe(1.56);
    });
  });

  describe("estLiqPrice", () => {
    it("should calculate the estimated liquidation price correctly:  Market(BTC)", () => {
      const inputs = {
        positions: [
          {
            symbol: "BTC",
            position_qty: 0.2,
            mark_price: 25986.2,
            mmr: 0.05,
          },
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
            mmr: 0.05,
          },
        ],
        newOrder: {
          symbol: "BTC",
          qty: 0.1,
          price: 26000, // if market order, price is the best ask price
        },
        totalCollateral: 1981.66,
        markPrice: 25986.2,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0.0000002512,
        orderFee: 1.56,
      };
      expect(estLiqPrice(inputs)).toBe(21268.73859649123);
    });

    // test("estLiqPrice: limit order 1", () => {
    //   const data = {
    //     positions: [],
    //     newOrder: {
    //       symbol: "PERP_BTC_USDC",
    //       qty: 0.0002909,
    //       price: 68705.9,
    //     },
    //     totalCollateral: 5.4,
    //     markPrice: 68705.9,
    //     baseMMR: 0.0275,
    //     baseIMR: 0.5478,
    //     IMR_Factor: 0.0000002512,
    //     orderFee: 0.1199,
    //   };

    //   expect(estLiqPrice(data)).toBe(51560.74026092236);
    // });

    test("estLiqPrice: limit order 1", () => {
      const inputs = {
        positions: [
          {
            symbol: "BTC",
            position_qty: 0.2,
            mark_price: 25986.2,
            mmr: 0.05,
          },
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
            mmr: 0.05,
          },
        ],
        newOrder: {
          symbol: "BTC",
          qty: 0.1,
          price: 25000,
        },
        totalCollateral: 1981.66,
        markPrice: 25986.2,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0.0000002512,
        orderFee: 1.5,
      };
      expect(estLiqPrice(inputs)).toBe(21250.984210526316);
    });

    test("estLiqPrice: limit order: 2", () => {
      const inputs = {
        positions: [
          {
            symbol: "BTC",
            position_qty: 0.2,
            mark_price: 25986.2,
            mmr: 0.05,
          },
          {
            symbol: "ETH",
            position_qty: -3,
            mark_price: 1638.41,
            mmr: 0.05,
          },
        ],
        newOrder: {
          symbol: "BTC",
          qty: -0.1,
          price: 25900,
        },
        totalCollateral: 1981.66,
        markPrice: 25986.2,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0.0000002512,
        orderFee: 1.554,
      };
      expect(estLiqPrice(inputs)).toBe(9102.173684210526);
    });

    test("estLiqPrice: no positions", () => {
      const inputs = {
        positions: [],
        newOrder: {
          symbol: "BTC",
          qty: 0.1,
          price: 25000,
        },
        totalCollateral: 1981.66,
        markPrice: 25986.2,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0.0000002512,
        orderFee: 1.5,
      };
      expect(estLiqPrice(inputs)).toBe(5472);
    });

    // test("estLiqPrice: ====>>>>>>", () => {
    //   const inputs = {
    //     positions: [
    //       { symbol: "ETH", position_qty: 0.01, mark_price: 3137.4, mmr: 0.05 },
    //     ],
    //     newOrder: {
    //       symbol: "ETH",
    //       qty: 1,
    //       price: 3139.2,
    //     },
    //     totalCollateral: 993.163404,
    //     markPrice: 3139.2,
    //     baseMMR: 0.025,
    //     baseIMR: 0.05,
    //     IMR_Factor: 0.0000003754,
    //     orderFee: 1.5,
    //   };
    //   expect(estLiqPrice(inputs)).toBe(5472);
    // });
  });

  describe("getOrderReferencePrice", () => {
    const ask = 100.2;
    const bid = 100.1;

    it("prices MARKET BUY from mark + priceRange instead of Ask1", () => {
      const price = getOrderReferencePrice(
        {
          orderType: OrderType.MARKET,
          side: OrderSide.BUY,
          markPrice: 100,
          priceRange: 0.05,
          pricePrecision: 2,
        },
        ask,
        bid,
      );
      // floor(100 * 1.05)
      expect(price).toBe(105);
    });

    it("prices MARKET SELL from mark - priceRange instead of Bid1", () => {
      const price = getOrderReferencePrice(
        {
          orderType: OrderType.MARKET,
          side: OrderSide.SELL,
          markPrice: 100,
          priceRange: 0.05,
          pricePrecision: 2,
        },
        ask,
        bid,
      );
      // ceil(100 * 0.95)
      expect(price).toBe(95);
    });

    it("rounds MARKET BUY down and MARKET SELL up at pricePrecision", () => {
      const buy = getOrderReferencePrice(
        {
          orderType: OrderType.MARKET,
          side: OrderSide.BUY,
          markPrice: 100.123,
          priceRange: 0.05,
          pricePrecision: 2,
        },
        ask,
        bid,
      );
      // 105.12915 -> floor 105.12
      expect(buy).toBe(105.12);

      const sell = getOrderReferencePrice(
        {
          orderType: OrderType.MARKET,
          side: OrderSide.SELL,
          markPrice: 100.123,
          priceRange: 0.05,
          pricePrecision: 2,
        },
        ask,
        bid,
      );
      // 95.11685 -> ceil 95.12
      expect(sell).toBe(95.12);
    });

    it("falls back to Ask1/Bid1 when mark price or price range is missing", () => {
      expect(
        getOrderReferencePrice(
          { orderType: OrderType.MARKET, side: OrderSide.BUY },
          ask,
          bid,
        ),
      ).toBe(ask);
      expect(
        getOrderReferencePrice(
          { orderType: OrderType.MARKET, side: OrderSide.SELL },
          ask,
          bid,
        ),
      ).toBe(bid);
      // mark price without a positive price range still falls back
      expect(
        getOrderReferencePrice(
          {
            orderType: OrderType.MARKET,
            side: OrderSide.BUY,
            markPrice: 100,
            priceRange: 0,
          },
          ask,
          bid,
        ),
      ).toBe(ask);
    });

    it("keeps LIMIT BUY at the submitted price and LIMIT SELL at max(limit, Bid1)", () => {
      expect(
        getOrderReferencePrice(
          {
            orderType: OrderType.LIMIT,
            side: OrderSide.BUY,
            limitPrice: 99,
          },
          ask,
          bid,
        ),
      ).toBe(99);
      // crossing SELL (limit below Bid1) uses Bid1
      expect(
        getOrderReferencePrice(
          {
            orderType: OrderType.LIMIT,
            side: OrderSide.SELL,
            limitPrice: 99,
          },
          ask,
          bid,
        ),
      ).toBe(bid);
    });
  });

  describe("estLiqPriceIsolated", () => {
    it("should include fee buffer when estimating opening order margin", () => {
      const liqPrice = estLiqPriceIsolated({
        isolatedPositionMargin: 0,
        costPosition: 0,
        positionQty: 0,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
        markPrice: 100,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0,
        leverage: 10,
        newOrder: {
          symbol: "PERP_BTC_USDC",
          qty: 1,
          price: 100,
        },
      });

      expect(liqPrice).toBeCloseTo(94.67368421052632, 12);
    });

    it("should include fee buffer when estimating flip order margin", () => {
      const liqPrice = estLiqPriceIsolated({
        isolatedPositionMargin: 10,
        costPosition: -100,
        positionQty: -1,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
        markPrice: 100,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMR_Factor: 0,
        leverage: 10,
        newOrder: {
          symbol: "PERP_BTC_USDC",
          qty: 2,
          price: 100,
        },
      });

      expect(liqPrice).toBeCloseTo(94.67368421052632, 12);
    });
  });
});
