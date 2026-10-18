// Import AlgoOrderType enum
import { AlgoOrderType } from "@paxeer-network/types";
// Import functions to be tested
import { OrderSide } from "@paxeer-network/types";
import {
  offsetToPrice,
  priceToOffset,
  offsetPercentageToPrice,
  priceToOffsetPercentage,
  pnlToPrice,
  priceToPnl,
  tpslCalculateHelper,
  UpdateOrderKey,
} from "../tp_slUtils";

describe("TP/SL Utils function", () => {
  describe("offsetToPrice", () => {
    test("Calculates price correctly for LONG/TAKE_PROFIT type with positive offset", () => {
      const inputs = {
        qty: 0.1,
        offset: 5000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };
      expect(offsetToPrice(inputs)).toBe(71000);
    });
    test("Calculates price correctly for LONG/STOP_LESS type with positive offset", () => {
      const inputs = {
        qty: 0.1,
        offset: 10000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.STOP_LOSS,
      };
      expect(offsetToPrice(inputs)).toBe(56000);
    });

    test("Calculates price correctly for SHORT/TAKE_PROFIT type with negative offset", () => {
      const inputs = {
        qty: -0.1,
        offset: 5000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };
      expect(offsetToPrice(inputs)).toBe(61000);
    });

    test("Calculates price correctly for SHORT/STOP_LOSS type with negative offset", () => {
      const inputs = {
        qty: -0.1,
        offset: 10000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };
      expect(offsetToPrice(inputs)).toBe(76000);
    });
  });

  describe("priceToOffset", () => {
    test("Calculates offset correctly for LONG/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToOffset(inputs)).toBe(1000);
    });

    test("Calculates offset correctly for LONG/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToOffset(inputs)).toBe(1000);
    });

    // test("Calculates offset correctly *******", () => {
    //   const inputs = {
    //     qty: 0.1,
    //     price: 1.4774,
    //     entryPrice: 5.4774043,
    //     orderSide: OrderSide.BUY,
    //     orderType: AlgoOrderType.STOP_LOSS,
    //   };

    //   expect(
    //     priceToOffset(inputs, {
    //       symbol: {
    //         quote_dp: 4,
    //       },
    //     })
    //   ).toBe(3000);
    // });

    test("Calculates offset correctly for SHORT/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 63000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToOffset(inputs)).toBe(3000);
    });

    test("Calculates offset correctly for SHORT/STOP_LOSS type with target price above mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(priceToOffset(inputs)).toBe(1000);
    });
  });

  describe("offsetPercentageToPrice", () => {
    test("Calculates price correctly for LONG/TAKE_PROFIT type with positive percentage offset", () => {
      const inputs = {
        qty: 0.1,
        percentage: 0.15,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(offsetPercentageToPrice(inputs)).toBe(75900);
    });

    test("Calculates price correctly for LONG/STOP_LOSS type with positive percentage offset", () => {
      const inputs = {
        qty: 0.1,
        percentage: 0.2,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(offsetPercentageToPrice(inputs)).toBe(52800);
    });

    test("Calculates price correctly for SHORT/STOP_LOSS type with positive percentage offset", () => {
      const inputs = {
        qty: -0.1,
        percentage: 0.15,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(offsetPercentageToPrice(inputs)).toBe(56100);
    });

    test("Calculates price correctly for SHORT/STOP_LOSS type with positive percentage offset", () => {
      const inputs = {
        qty: -0.1,
        percentage: 0.2,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(offsetPercentageToPrice(inputs)).toBe(79200);
    });
  });

  describe("priceToOffsetPercentage", () => {
    test("Calculates offset percentage correctly for LONG/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToOffsetPercentage(inputs)).toBe(0.015151515151515152);
    });
    test("Calculates offset percentage correctly for LONG/STOP_LOSS type with target price above mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 63000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(priceToOffsetPercentage(inputs)).toBe(0.045454545454545456);
    });

    test("Calculates offset percentage correctly for SHORT/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 63000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToOffsetPercentage(inputs)).toBe(0.045454545454545456);
    });

    test("Calculates offset percentage correctly for SHORT/STOP_LOSS type with target price above mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(priceToOffsetPercentage(inputs)).toBe(0.015151515151515152);
    });
  });

  describe("pnlToPrice", () => {
    test("Calculates price correctly for LONG/TAKE_PROFIT type with positive pnl", () => {
      const inputs = {
        qty: 0.1,
        pnl: 200,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(pnlToPrice(inputs)).toBe(68000);
    });

    test("Calculates price correctly for LONG/STOP_LOSS type with negative pnl", () => {
      const inputs = {
        qty: 0.1,
        pnl: -500,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(pnlToPrice(inputs)).toBe(61000);
    });

    test("Calculates price correctly for SHORT/TAKE_PROFIT type with positive pnl", () => {
      const inputs = {
        qty: -0.1,
        pnl: 200,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(pnlToPrice(inputs)).toBe(64000);
    });

    test("Calculates price correctly for SHORT/STOP_LOSS type with negative pnl", () => {
      const inputs = {
        qty: -0.1,
        pnl: -500,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(pnlToPrice(inputs)).toBe(71000);
    });
  });

  describe("priceToPnl", () => {
    test("Calculates pnl correctly for LONG/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToPnl(inputs)).toBe(100);
    });

    test("Calculates pnl correctly for LONG/STOP_LOSS type with target price below mark price", () => {
      const inputs = {
        qty: 0.1,
        price: 65000,
        entryPrice: 66000,
        orderSide: OrderSide.BUY,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(priceToPnl(inputs)).toBe(-100);
    });

    test("Calculates pnl correctly for SHORT/TAKE_PROFIT type with target price above mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 65000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.TAKE_PROFIT,
      };

      expect(priceToPnl(inputs)).toBe(100);
    });

    test("Calculates pnl correctly for SHORT/STOP_LOSS type with target price below mark price", () => {
      const inputs = {
        qty: -0.1,
        price: 67000,
        entryPrice: 66000,
        orderSide: OrderSide.SELL,
        orderType: AlgoOrderType.STOP_LOSS,
      };

      expect(priceToPnl(inputs)).toBe(-100);
    });
  });

  describe("calculateHelper: Side = OrderSide.BUY", () => {
    const inputs = {
      key: "tp_trigger_price" as UpdateOrderKey,
      value: 67000,
      entryPrice: 66000,
      qty: 0.1,
      orderSide: OrderSide.BUY,
      orderType: AlgoOrderType.TAKE_PROFIT,
    };

    test("Calculates trigger price correctly for tp_trigger_price key", () => {
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        tp_trigger_price: 67000,
        tp_offset: 1000,
        tp_offset_percentage: 0.015151515151515152,
        tp_pnl: 100,
      });
    });

    test("Calculates trigger price correctly for sl_trigger_price key", () => {
      inputs.key = "sl_trigger_price";
      inputs.value = 65000;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        sl_trigger_price: 65000,
        sl_offset: 1000,
        sl_offset_percentage: 0.015151515151515152,
        sl_pnl: -100,
      });
    });

    test("Calculates trigger price correctly for tp_pnl key", () => {
      inputs.key = "tp_pnl";
      inputs.value = 200;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        tp_trigger_price: 68000,
        tp_offset: 2000,
        tp_offset_percentage: 0.030303030303030304,
        tp_pnl: 200,
      });
    });

    test("Calculates trigger price correctly for sl_pnl key", () => {
      inputs.key = "sl_pnl";
      inputs.value = -500;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        sl_trigger_price: 61000,
        sl_offset: 5000,
        sl_offset_percentage: 0.07575757575757576,
        sl_pnl: -500,
      });
    });

    test("Calculates trigger price correctly for tp_offset key", () => {
      inputs.key = "tp_offset";
      inputs.value = 5000;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        tp_trigger_price: 71000,
        tp_offset: 5000,
        tp_offset_percentage: 0.07575757575757576,
        tp_pnl: 500,
      });
    });

    test("Calculates trigger price correctly for sl_offset key", () => {
      inputs.key = "sl_offset";
      inputs.value = 1000;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        sl_trigger_price: 65000,
        sl_offset: inputs.value,
        sl_offset_percentage: 0.015151515151515152,
        sl_pnl: -100,
      });
    });

    test("Calculates trigger price correctly for tp_offset_percentage key", () => {
      inputs.key = "tp_offset_percentage";
      inputs.value = 0.15;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        tp_trigger_price: 75900,
        tp_offset: 9900,
        tp_offset_percentage: inputs.value,
        tp_pnl: 990,
      });
    });

    test("Calculates trigger price correctly for sl_offset_percentage key", () => {
      inputs.key = "sl_offset_percentage";
      inputs.value = 0.15;
      expect(tpslCalculateHelper(inputs.key, inputs)).toEqual({
        sl_trigger_price: 56100,
        sl_offset: 9900,
        sl_offset_percentage: inputs.value,
        sl_pnl: -990,
      });
    });
  });
});
