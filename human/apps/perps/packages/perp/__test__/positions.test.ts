import { describe, expect, it } from "@jest/globals";
import * as positions from "../src/positions";
import type { LiqPriceInputs, MMRInputs } from "../src/positions";

describe("positions formula", () => {
  describe("liqPrice", () => {
    it("should return null when positionQty is 0", () => {
      const inputs: LiqPriceInputs = {
        markPrice: 100,
        symbol: "BTC",
        totalCollateral: 1000,
        positions: [],
        positionQty: 0,
        MMR: 0.05,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMRFactor: 0.0000002512,
        costPosition: 0,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
      };
      expect(positions.liqPrice(inputs)).toBeNull();
    });

    it("should calculate the liqPrice correctly: BTC LONG", () => {
      const inputs: LiqPriceInputs = {
        markPrice: 90000,
        totalCollateral: 200000,
        symbol: "BTC",
        positions: [
          // BTC
          {
            position_qty: 50,
            mark_price: 90000,
            mmr: 0.01261017929,
            symbol: "BTC",
          },
          // ETH
          {
            position_qty: 100,
            mark_price: 3000,
            mmr: 0.006,
            symbol: "ETH",
          },
        ],
        positionQty: 50,
        MMR: 0.01261017929,
        baseMMR: 0.006,
        baseIMR: 0.01,
        IMRFactor: 0.0000001,
        costPosition: 0,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
      };
      const result = positions.liqPrice(inputs);
      // Round to 5 decimal places before assertion
      const roundedResult =
        result !== null ? Math.round(result * 100000) / 100000 : null;
      expect(roundedResult).toBe(87107.62339);
    });

    it("should calculate the liqPrice correctly: BTC SHORT", () => {
      const inputs: LiqPriceInputs = {
        markPrice: 90000,
        totalCollateral: 200000,
        symbol: "BTC",
        positions: [
          // BTC
          {
            position_qty: -50,
            mark_price: 90000,
            mmr: 0.01261017929,
            symbol: "BTC",
          },
          // ETH
          {
            position_qty: 100,
            mark_price: 3000,
            mmr: 0.006,
            symbol: "ETH",
          },
        ],
        positionQty: -50,
        MMR: 0.01261017929,
        baseMMR: 0.006,
        baseIMR: 0.01,
        IMRFactor: 0.0000001,
        costPosition: 0,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
      };
      const result = positions.liqPrice(inputs);
      // Round to 5 decimal places before assertion
      const roundedResult =
        result !== null ? Math.round(result * 100000) / 100000 : null;
      expect(roundedResult).toBe(92765.25186);
    });
  });

  describe("liqPrice stability", () => {
    it("should produce stable liqPrice for single LONG position with fixed MMR when totalCollateral includes markPrice-dependent PnL", () => {
      // DEX-2906: When totalCollateral = USDC + qty * markPrice - costPosition,
      // the markPrice terms cancel in the formula and liqPrice should be invariant.
      const positionQty = 2.4099;
      const avgOpenPrice = 2074.25;
      const costPosition = positionQty * avgOpenPrice; // ~4998.70
      const usdcHolding = 1000;
      const baseMMR = 0.006;
      const baseIMR = 0.01;
      const imrFactor = 1.724e-7;

      // Calculate totalCollateral at two different mark prices
      const markPrice1 = 2241.17;
      const markPrice2 = 2245.0;

      const totalCollateral1 =
        usdcHolding + positionQty * markPrice1 - costPosition;
      const totalCollateral2 =
        usdcHolding + positionQty * markPrice2 - costPosition;

      const liqPrice1 = positions.liqPrice({
        markPrice: markPrice1,
        symbol: "PERP_ETH_USDC",
        totalCollateral: totalCollateral1,
        positionQty,
        positions: [], // single position, no other positions
        MMR: baseMMR,
        baseMMR,
        baseIMR,
        IMRFactor: imrFactor,
        costPosition,
      });

      const liqPrice2 = positions.liqPrice({
        markPrice: markPrice2,
        symbol: "PERP_ETH_USDC",
        totalCollateral: totalCollateral2,
        positionQty,
        positions: [],
        MMR: baseMMR,
        baseMMR,
        baseIMR,
        IMRFactor: imrFactor,
        costPosition,
      });

      expect(liqPrice1).not.toBeNull();
      expect(liqPrice2).not.toBeNull();

      // liqPrice should be virtually identical (< 0.01 difference)
      // when totalCollateral correctly includes markPrice-dependent PnL
      const diff = Math.abs(liqPrice2! - liqPrice1!);
      expect(diff).toBeLessThan(0.01);
    });

    it("should produce stable liqPrice for single SHORT position with fixed MMR", () => {
      const positionQty = -2.4099;
      const avgOpenPrice = 2074.25;
      const costPosition = positionQty * avgOpenPrice; // negative for short
      const usdcHolding = 1000;
      const baseMMR = 0.006;
      const baseIMR = 0.01;
      const imrFactor = 1.724e-7;

      const markPrice1 = 2241.17;
      const markPrice2 = 2245.0;

      const totalCollateral1 =
        usdcHolding + positionQty * markPrice1 - costPosition;
      const totalCollateral2 =
        usdcHolding + positionQty * markPrice2 - costPosition;

      const liqPrice1 = positions.liqPrice({
        markPrice: markPrice1,
        symbol: "PERP_ETH_USDC",
        totalCollateral: totalCollateral1,
        positionQty,
        positions: [],
        MMR: baseMMR,
        baseMMR,
        baseIMR,
        IMRFactor: imrFactor,
        costPosition,
      });

      const liqPrice2 = positions.liqPrice({
        markPrice: markPrice2,
        symbol: "PERP_ETH_USDC",
        totalCollateral: totalCollateral2,
        positionQty,
        positions: [],
        MMR: baseMMR,
        baseMMR,
        baseIMR,
        IMRFactor: imrFactor,
        costPosition,
      });

      expect(liqPrice1).not.toBeNull();
      expect(liqPrice2).not.toBeNull();

      const diff = Math.abs(liqPrice2! - liqPrice1!);
      expect(diff).toBeLessThan(0.01);
    });
  });

  describe("liquidationPriceIsolated", () => {
    it("should include fee buffer when estimating opening order margin", () => {
      const liqPrice = positions.liquidationPriceIsolated({
        isolatedPositionMargin: 0,
        costPosition: 0,
        positionQty: 0,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMRFactor: 0,
        referencePrice: 100,
        orderSide: "BUY",
        orderQty: 1,
        leverage: 10,
      });

      expect(liqPrice).toBeCloseTo(94.67368421052632, 12);
    });

    it("should include fee buffer when estimating flip order margin", () => {
      const liqPrice = positions.liquidationPriceIsolated({
        isolatedPositionMargin: 10,
        costPosition: -100,
        positionQty: -1,
        sumUnitaryFunding: 0,
        lastSumUnitaryFunding: 0,
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMRFactor: 0,
        referencePrice: 100,
        orderSide: "BUY",
        orderQty: 2,
        leverage: 10,
      });

      expect(liqPrice).toBeCloseTo(94.67368421052632, 12);
    });
  });

  describe("MMR", () => {
    it("should calculate the MMR correctly: BTC", () => {
      // BTC
      const inputs: MMRInputs = {
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMRFactor: 0.0000002512,
        positionNotional: 5197.2,
        IMR_factor_power: 4 / 5,
      };
      expect(positions.MMR(inputs)).toBe(0.05);
    });

    it("should calculate the MMR correctly: ETH", () => {
      //ETH
      const inputs: MMRInputs = {
        baseMMR: 0.05,
        baseIMR: 0.1,
        IMRFactor: 0.0000003754,
        positionNotional: 4915.23,
        IMR_factor_power: 4 / 5,
      };
      expect(positions.MMR(inputs)).toBe(0.05);
    });

    it("should calculate the MMR correctly: case 1", () => {
      //ETH
      const inputs: MMRInputs = {
        baseMMR: 0.025,
        baseIMR: 0.05,
        IMRFactor: 0.0000003517,
        positionNotional: 255000,
        IMR_factor_power: 4 / 5,
      };
      expect(positions.MMR(inputs)).toBe(0.025);
    });
  });
});
