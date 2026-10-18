import { AlgoOrderRootType, API, OrderType } from "@paxeer-network/types";
import {
  getChangedTPSLEditableOrderValues,
  getTPSLEditOrderType,
  getTPSLEditableOrderValues,
  isTPSLOrderTypeLocked,
} from "./tpslOrderSync";

const createOrder = (overrides: Partial<API.AlgoOrder> = {}) =>
  ({
    algo_order_id: 1,
    algo_type: AlgoOrderRootType.TP_SL,
    symbol: "PERP_ETH_USDC",
    quantity: 2,
    child_orders: [
      {
        algo_order_id: 2,
        algo_type: "TAKE_PROFIT",
        type: OrderType.LIMIT,
        trigger_price: 4200,
        price: 4190,
      },
      {
        algo_order_id: 3,
        algo_type: "STOP_LOSS",
        type: OrderType.MARKET,
        trigger_price: 3800,
      },
    ],
    ...overrides,
  }) as API.AlgoOrder;

describe("TP/SL external order synchronization", () => {
  it("locks editing and defaults a missing child to MARKET", () => {
    const tpOnly = createOrder({
      child_orders: [createOrder().child_orders[0]],
    });

    expect(getTPSLEditOrderType(tpOnly, "tp")).toBe(OrderType.LIMIT);
    expect(getTPSLEditOrderType(tpOnly, "sl")).toBe(OrderType.MARKET);
    expect(isTPSLOrderTypeLocked(tpOnly, "tp")).toBe(true);
    expect(isTPSLOrderTypeLocked(tpOnly, "sl")).toBe(true);
    expect(getTPSLEditableOrderValues(tpOnly)).toMatchObject({
      sl_trigger_price: "",
      sl_order_type: OrderType.MARKET,
      sl_order_price: "",
    });
  });

  it("unlocks an inactive server placeholder and keeps its configured type", () => {
    const order = createOrder({
      child_orders: [
        createOrder().child_orders[0],
        {
          ...createOrder().child_orders[1],
          type: OrderType.MARKET,
          trigger_price: 3800,
          price: 3790,
          is_activated: false,
        },
      ],
    });

    expect(isTPSLOrderTypeLocked(order, "sl")).toBe(false);
    expect(getTPSLEditableOrderValues(order)).toMatchObject({
      sl_trigger_price: "",
      sl_order_type: OrderType.MARKET,
      sl_order_price: "",
    });
  });

  it("keeps a triggered inactive leg locked", () => {
    const order = createOrder();
    order.child_orders[1] = {
      ...order.child_orders[1],
      is_activated: false,
      is_triggered: true,
    };

    expect(isTPSLOrderTypeLocked(order, "sl")).toBe(true);
  });

  it("extracts every editable field from an order", () => {
    expect(getTPSLEditableOrderValues(createOrder())).toEqual({
      quantity: 2,
      tp_trigger_price: "4200",
      tp_order_type: OrderType.LIMIT,
      tp_order_price: "4190",
      sl_trigger_price: "3800",
      sl_order_type: OrderType.MARKET,
      sl_order_price: "",
    });
  });

  it("returns only fields changed by the server", () => {
    const previous = getTPSLEditableOrderValues(createOrder());
    const next = getTPSLEditableOrderValues(
      createOrder({
        quantity: 3,
        child_orders: [
          {
            algo_order_id: 2,
            algo_type: "TAKE_PROFIT",
            type: OrderType.LIMIT,
            trigger_price: 4300,
            price: 4290,
          },
          createOrder().child_orders[1],
        ],
      }),
    );

    expect(getChangedTPSLEditableOrderValues(previous, next)).toEqual({
      quantity: 3,
      tp_trigger_price: "4300",
      tp_order_price: "4290",
    });
  });

  it("clears removed legs and reflects order type changes", () => {
    const previous = getTPSLEditableOrderValues(createOrder());
    const next = getTPSLEditableOrderValues(
      createOrder({
        child_orders: [
          {
            algo_order_id: 2,
            algo_type: "TAKE_PROFIT",
            type: OrderType.MARKET,
            trigger_price: 4250,
          },
        ],
      }),
    );

    expect(getChangedTPSLEditableOrderValues(previous, next)).toEqual({
      tp_trigger_price: "4250",
      tp_order_type: OrderType.MARKET,
      tp_order_price: "",
      sl_trigger_price: "",
    });
  });

  it("preserves local drafts for fields unchanged by the server", () => {
    const previous = getTPSLEditableOrderValues(createOrder());
    const localDraft = {
      ...previous,
      sl_trigger_price: "3750",
    };
    const next = getTPSLEditableOrderValues(
      createOrder({
        child_orders: [
          {
            algo_order_id: 2,
            algo_type: "TAKE_PROFIT",
            type: OrderType.LIMIT,
            trigger_price: 4300,
            price: 4190,
          },
          createOrder().child_orders[1],
        ],
      }),
    );

    const merged = {
      ...localDraft,
      ...getChangedTPSLEditableOrderValues(previous, next),
    };

    expect(merged.tp_trigger_price).toBe("4300");
    expect(merged.sl_trigger_price).toBe("3750");
  });
});
