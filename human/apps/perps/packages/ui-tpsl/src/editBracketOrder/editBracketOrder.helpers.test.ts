import {
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  OrderType,
} from "@paxeer-network/types";
import {
  getBracketTPSLPriceInfo,
  hasBracketTPSLPriceChanged,
} from "./editBracketOrder.helpers";

const createTPSLParent = (overrides: Partial<API.AlgoOrder> = {}) =>
  ({
    algo_order_id: 10,
    algo_type: AlgoOrderRootType.POSITIONAL_TP_SL,
    symbol: "PERP_ETH_USDC",
    child_orders: [
      {
        algo_order_id: 11,
        algo_type: AlgoOrderType.TAKE_PROFIT,
        type: OrderType.MARKET,
        trigger_price: 0,
        is_activated: false,
      },
      {
        algo_order_id: 12,
        algo_type: AlgoOrderType.STOP_LOSS,
        type: OrderType.LIMIT,
        trigger_price: 3900,
        price: 3890,
        is_activated: true,
      },
    ],
    ...overrides,
  }) as API.AlgoOrder;

describe("Bracket TP/SL edit helpers", () => {
  it("keeps an inactive MARKET placeholder empty", () => {
    expect(getBracketTPSLPriceInfo(createTPSLParent())).toEqual({
      tp_trigger_price: "",
      tp_order_type: OrderType.MARKET,
      tp_order_price: "",
      sl_trigger_price: "3900",
      sl_order_type: OrderType.LIMIT,
      sl_order_price: "3890",
    });
  });

  it("keeps the configured LIMIT type without restoring inactive prices", () => {
    const parent = createTPSLParent();
    parent.child_orders[0] = {
      ...parent.child_orders[0],
      type: OrderType.LIMIT,
      trigger_price: 4100,
      price: 4110,
      is_activated: false,
    };

    expect(getBracketTPSLPriceInfo(parent)).toMatchObject({
      tp_trigger_price: "",
      tp_order_type: OrderType.LIMIT,
      tp_order_price: "",
    });
  });

  it("detects activating and clearing a leg", () => {
    const initial = getBracketTPSLPriceInfo(createTPSLParent());

    expect(hasBracketTPSLPriceChanged(initial, initial)).toBe(false);
    expect(
      hasBracketTPSLPriceChanged(initial, {
        ...initial,
        tp_trigger_price: "4100",
      }),
    ).toBe(true);
    expect(
      hasBracketTPSLPriceChanged(initial, {
        ...initial,
        sl_trigger_price: "",
      }),
    ).toBe(true);
  });

  it("treats equivalent numeric representations and empty values as unchanged", () => {
    const initial = getBracketTPSLPriceInfo(createTPSLParent());

    expect(
      hasBracketTPSLPriceChanged(initial, {
        ...initial,
        tp_trigger_price: undefined,
        tp_order_price: undefined,
        sl_trigger_price: 3900,
        sl_order_price: 3890,
      }),
    ).toBe(false);
  });
});
