import { getTPSLQuantity, isEntirePositionTPSL } from "@paxeer-network/utils";
import { findTPSLOrderPriceFromOrder } from "../../../paxeer-network/usePositionStream/utils";
import { AlgoOrderMergeHandler } from "../algoOrderMergeHandler";

describe("Positional Limit websocket grouping", () => {
  it("keeps full-position Limit prices after an edit event", () => {
    const result = AlgoOrderMergeHandler.groupOrders([
      {
        algoOrderId: 1,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 0,
        algoType: "POSITIONAL_TP_SL",
        symbol: "PERP_ETH_USDC",
      },
      {
        algoOrderId: 2,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 1,
        algoType: "TAKE_PROFIT",
        type: "LIMIT_ORDER",
        triggerPrice: 2550,
        price: 2575,
      },
      {
        algoOrderId: 3,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 1,
        algoType: "STOP_LOSS",
        type: "LIMIT_ORDER",
        triggerPrice: 2400,
        price: 2108,
      },
    ] as any);

    expect(findTPSLOrderPriceFromOrder(result)).toEqual({
      tp_order_price: 2575,
      sl_order_price: 2108,
    });
  });

  it("preserves direct Bracket parent and margin mode regardless of WS leaf order", () => {
    const result = AlgoOrderMergeHandler.groupOrders([
      {
        algoOrderId: 1,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 0,
        algoType: "BRACKET",
        marginMode: "ISOLATED",
        symbol: "PERP_ETH_USDC",
      },
      {
        algoOrderId: 3,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 2,
        algoType: "TAKE_PROFIT",
        type: "LIMIT_ORDER",
        triggerPrice: 4100,
        price: 4110,
        quantity: 2,
        triggered: true,
      },
      {
        algoOrderId: 2,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 1,
        algoType: "POSITIONAL_TP_SL",
      },
      {
        algoOrderId: 4,
        rootAlgoOrderId: 1,
        parentAlgoOrderId: 2,
        algoType: "STOP_LOSS",
        type: "CLOSE_POSITION",
        isTriggered: false,
        quantity: 0,
      },
    ] as any);
    const parent = result.child_orders[0];
    expect(parent.algo_type).toBe("POSITIONAL_TP_SL");
    expect(parent.child_orders[0]).toMatchObject({
      parent_algo_type: "POSITIONAL_TP_SL",
      margin_mode: "ISOLATED",
      type: "LIMIT",
      price: 4110,
      is_triggered: true,
    });
    expect(findTPSLOrderPriceFromOrder(parent).tp_order_price).toBe(4110);
    expect(getTPSLQuantity(parent.child_orders[0], 10)).toBe(2);
    expect(isEntirePositionTPSL(parent.child_orders[1])).toBe(true);
    expect(getTPSLQuantity(parent.child_orders[1], 10)).toBe(10);
  });
});
