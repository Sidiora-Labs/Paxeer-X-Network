import { account } from "@paxeer-network/perp";
import { API, OrderSide } from "@paxeer-network/types";
import { mergeHoldingBalance } from "../../utils/mergeHoldingBalance";
import { parseHolding } from "../../utils/parseHolding";

const createHolding = (
  token: string,
  holding: number,
  isolatedOrderFrozen = 0,
) =>
  ({
    token,
    holding,
    frozen: 0,
    pending_short: 0,
    isolated_margin: 0,
    isolated_order_frozen: isolatedOrderFrozen,
    updated_time: 0,
  }) as API.Holding;

const tokensInfo = [
  {
    token: "USDT",
    base_weight: 0.9,
    discount_factor: 0,
    user_max_qty: -1,
    is_collateral: true,
  },
] as API.Token[];

const calculateSecondSymbolMaxQty = (holding: API.Holding[]) => {
  const indexPrices = { PERP_USDT_USDC: 1 };
  const [USDCHolding, nonUSDCHolding] = parseHolding(
    holding,
    indexPrices,
    tokensInfo,
  );
  const usdc = holding.find((item) => item.token === "USDC");
  const totalCollateral = account.totalCollateral({
    USDCHolding,
    nonUSDCHolding,
    unsettlementPnL: 0,
    usdcBalancePendingShortQty: usdc?.pending_short ?? 0,
    usdcBalanceIsolatedOrderFrozen: usdc?.isolated_order_frozen ?? 0,
  });
  const freeCollateral = account.freeCollateral({
    totalCollateral,
    totalInitialMarginWithOrders: 0,
  });
  const maxQty = account.maxQtyForIsolatedMargin({
    symbol: "PERP_ETH_USDC",
    orderSide: OrderSide.BUY,
    currentOrderReferencePrice: 100,
    availableBalance: freeCollateral.toNumber(),
    leverage: 10,
    baseIMR: 0.1,
    IMR_Factor: 0.0000001,
    markPrice: 100,
    positionQty: 0,
    pendingLongOrders: [],
    pendingSellOrders: [],
    isoOrderFrozenLong: 0,
    isoOrderFrozenShort: 0,
    symbolMaxNotional: 1000000,
  });

  return { freeCollateral, maxQty };
};

describe("isolated collateral across symbols", () => {
  it("should not reuse collateral frozen by an order on another symbol", () => {
    const initialHolding = [
      createHolding("USDC", 0),
      createHolding("USDT", 100),
    ];
    const beforeFirstOrder = calculateSecondSymbolMaxQty(initialHolding);

    // A BTC isolated order freezes 20 USDC of the shared eligible collateral.
    const afterFirstOrderHolding = initialHolding.map((item) =>
      item.token === "USDC"
        ? mergeHoldingBalance(item, { isolatedOrderFrozen: 20 })
        : item,
    );
    const afterFirstOrder = calculateSecondSymbolMaxQty(afterFirstOrderHolding);

    expect(beforeFirstOrder.freeCollateral.toNumber()).toBe(90);
    expect(afterFirstOrder.freeCollateral.toNumber()).toBe(70);
    expect(afterFirstOrder.maxQty).toBeLessThan(beforeFirstOrder.maxQty);
    expect(afterFirstOrder.maxQty).toBeCloseTo(
      beforeFirstOrder.maxQty * (70 / 90),
      12,
    );
  });
});
