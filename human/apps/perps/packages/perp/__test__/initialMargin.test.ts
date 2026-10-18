import { describe, expect, it } from "@jest/globals";
import { API, MarginMode, OrderSide } from "@paxeer-network/types";
import { totalInitialMarginWithQty } from "../src/account/initialMargin";

const symbol = "PERP_BTC_USDC";

const createPosition = (overrides: Partial<API.Position> = {}) =>
  ({
    symbol,
    position_qty: 0,
    pending_long_qty: 0,
    pending_short_qty: 0,
    margin_mode: MarginMode.CROSS,
    leverage: 10,
    ...overrides,
  }) as API.Position;

const commonInputs = {
  markPrices: { [symbol]: 100 },
  IMR_Factors: { [symbol]: 0 },
  maxLeverageBySymbol: { [symbol]: 10 },
  symbolInfo: {
    [symbol]: (key: string, fallback = 0) =>
      key === "base_imr" ? 0.1 : fallback,
  },
};

describe("totalInitialMarginWithQty", () => {
  it("uses pending quantities from cross positions when orders are omitted", () => {
    const result = totalInitialMarginWithQty({
      ...commonInputs,
      positions: [createPosition({ pending_long_qty: 2 })],
    });

    expect(result).toBe(20);
  });

  it("does not count isolated pending quantities", () => {
    const result = totalInitialMarginWithQty({
      ...commonInputs,
      positions: [
        createPosition({
          pending_long_qty: 2,
          margin_mode: MarginMode.ISOLATED,
        }),
      ],
    });

    expect(result).toBe(0);
  });

  it("uses explicit orders without double-counting position pending quantities", () => {
    const result = totalInitialMarginWithQty({
      ...commonInputs,
      positions: [createPosition({ pending_long_qty: 2 })],
      orders: [
        {
          symbol,
          side: OrderSide.BUY,
          quantity: 2,
          margin_mode: MarginMode.CROSS,
        } as API.Order,
      ],
    });

    expect(result).toBe(20);
  });

  it("treats an explicit empty order list as no pending orders", () => {
    const result = totalInitialMarginWithQty({
      ...commonInputs,
      positions: [createPosition({ pending_long_qty: 2 })],
      orders: [],
    });

    expect(result).toBe(0);
  });
});
