import { API } from "@paxeer-network/types";
import { mergeHoldingBalance } from "../mergeHoldingBalance";

const holding = {
  token: "USDC",
  holding: 100,
  frozen: 1,
  pending_short: -2,
  isolated_margin: 3,
  isolated_order_frozen: 4,
  updated_time: 0,
} as API.Holding;

describe("mergeHoldingBalance", () => {
  it("should normalize camelCase WebSocket balance fields", () => {
    expect(
      mergeHoldingBalance(holding, {
        holding: 90,
        frozen: 5,
        pendingShort: -6,
        isolatedMargin: 7,
        isolatedOrderFrozen: 8,
      }),
    ).toMatchObject({
      holding: 90,
      frozen: 5,
      pending_short: -6,
      isolated_margin: 7,
      isolated_order_frozen: 8,
    });
  });

  it("should normalize snake_case REST balance fields", () => {
    expect(
      mergeHoldingBalance(holding, {
        pending_short: -9,
        isolated_margin: 10,
        isolated_order_frozen: 11,
      }),
    ).toMatchObject({
      holding: 100,
      frozen: 1,
      pending_short: -9,
      isolated_margin: 10,
      isolated_order_frozen: 11,
    });
  });

  it("should accept the legacy WebSocket pendingShortQty field", () => {
    expect(
      mergeHoldingBalance(holding, {
        pendingShortQty: -12,
      }),
    ).toMatchObject({
      pending_short: -12,
    });
  });
});
