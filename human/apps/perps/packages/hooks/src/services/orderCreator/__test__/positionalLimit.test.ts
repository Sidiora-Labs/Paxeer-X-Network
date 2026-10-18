import {
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { BracketLimitOrderCreator } from "../bracketLimitOrderCreator";
import { BracketMarketOrderCreator } from "../bracketMarketOrderCreator";
import { BracketOrderBuilder } from "../builders/BracketOrderBuilder";
import { TPSLOrderCreator } from "../tpslOrderCreator";
import {
  createTPSLOrderUpdates,
  normalizeTPSLChildType,
  sanitizeTPSLChildUpdates,
} from "../tpslOrderUpdates";
import { TPSLPositionOrderCreator } from "../tpslPositionOrderCreator";
import { createMockConfig, createMockPaxeerNetworkOrder } from "./testHelpers";

const creator = new TPSLPositionOrderCreator();
const config = createMockConfig();
const values: any = {
  symbol: "PERP_ETH_USDC",
  side: OrderSide.BUY,
  position_type: PositionType.FULL,
  quantity: "2",
  tp_trigger_price: "4100",
  tp_order_price: "4110",
  tp_order_type: OrderType.LIMIT,
  sl_trigger_price: "3900",
  sl_order_type: OrderType.MARKET,
};
const oldOrder = () =>
  ({
    ...creator.create(values, config),
    algo_order_id: 10,
    child_orders: creator
      .create(values, config)
      .child_orders.map((child, index) => ({
        ...child,
        algo_order_id: index + 11,
      })),
  }) as API.AlgoOrder;

describe("Positional limit creation and compatibility", () => {
  it("normalizes market child types for full and partial TP/SL", () => {
    expect(normalizeTPSLChildType(OrderType.MARKET, true)).toBe(
      OrderType.CLOSE_POSITION,
    );
    expect(normalizeTPSLChildType(OrderType.CLOSE_POSITION, false)).toBe(
      OrderType.MARKET,
    );
    expect(normalizeTPSLChildType(OrderType.LIMIT, true)).toBe(OrderType.LIMIT);
  });

  it.each([OrderType.MARKET, OrderType.LIMIT])(
    "serializes independent %s legs without quantities",
    (type) => {
      const result = creator.create(
        { ...values, sl_order_type: type, sl_order_price: "3890" },
        config,
      );
      expect(result).not.toHaveProperty("quantity");
      expect(result.child_orders[0]).toMatchObject({
        type: OrderType.LIMIT,
        price: 4110,
      });
      expect(result.child_orders[1].type).toBe(
        type === OrderType.LIMIT ? OrderType.LIMIT : OrderType.CLOSE_POSITION,
      );
      for (const child of result.child_orders)
        expect(child).not.toHaveProperty("quantity");
      if (type === OrderType.MARKET)
        expect(result.child_orders[1]).not.toHaveProperty("price");
    },
  );
  it("supports legacy price-only calls and ignores explicit market stale prices", async () => {
    expect(
      creator.create({ ...values, tp_order_type: undefined }, config)
        .child_orders[0].type,
    ).toBe(OrderType.LIMIT);
    const market = {
      ...values,
      tp_order_type: OrderType.MARKET,
      tp_order_price: "-100",
    };
    expect(creator.create(market, config).child_orders[0]).toMatchObject({
      type: OrderType.CLOSE_POSITION,
    });
    expect(creator.create(market, config).child_orders[0]).not.toHaveProperty(
      "price",
    );
    expect(await creator.validate(market, config)).toBeNull();
    expect(
      new TPSLOrderCreator().create(
        { ...market, position_type: PositionType.PARTIAL },
        config,
      ).child_orders[0].type,
    ).toBe(OrderType.MARKET);
  });
  it.each([new BracketLimitOrderCreator(), new BracketMarketOrderCreator()])(
    "keeps only Bracket opening quantity",
    (bracket) => {
      const result: any = bracket.create(
        createMockPaxeerNetworkOrder({ ...values, order_quantity: "2" }),
        config,
      );
      expect(result.quantity).toBe("2");
      const parent = result.child_orders[0];
      expect(parent.algo_type).toBe(AlgoOrderRootType.POSITIONAL_TP_SL);
      expect(parent).not.toHaveProperty("quantity");
      expect(parent.child_orders[0]).toMatchObject({
        type: OrderType.LIMIT,
        price: "4110",
      });
      expect(parent.child_orders[1]).not.toHaveProperty("price");
      for (const child of parent.child_orders)
        expect(child).not.toHaveProperty("quantity");
    },
  );
  it("fixes the alternative Bracket builder and default full-position market type", () => {
    const builder = new BracketOrderBuilder()
      .withSymbol(values.symbol)
      .withSide(OrderSide.BUY)
      .withQuantity("2")
      .withPositionType(PositionType.FULL)
      .withTPTrigger(4100)
      .withTPOrderPrice(4110)
      .withSLTrigger(3900);
    const parent = builder.build().child_orders[0];
    expect(parent.child_orders.map((child: any) => child.type)).toEqual([
      OrderType.LIMIT,
      OrderType.CLOSE_POSITION,
    ]);
    expect(parent.child_orders[1]).not.toHaveProperty("price");
  });
  it.each(["", "0", "-1", "NaN", "Infinity", "0.001"])(
    "rejects invalid enabled limit price %s",
    async (price) => {
      const result = await creator.validate(
        { ...values, tp_order_price: price },
        config,
      );
      expect(result?.tp_order_price).toBeDefined();
      expect(() =>
        creator.create({ ...values, tp_order_price: price }, config),
      ).toThrow();
    },
  );
  it("ignores inactive limit legs and does not validate full-position estimate quantity", async () => {
    expect(
      await creator.validate(
        {
          ...values,
          tp_trigger_price: "",
          tp_order_price: "",
          quantity: "99999",
        },
        config,
      ),
    ).toBeNull();
  });
  it("requires a trigger price when an inactive LIMIT leg has an order price", async () => {
    const result = await creator.validate(
      {
        ...values,
        tp_trigger_price: "",
        tp_order_price: "4210",
      },
      config,
    );

    expect(result?.tp_trigger_price).toMatchObject({ type: "required" });
  });
  it.each([OrderType.MARKET, OrderType.LIMIT])(
    "rejects non-positive and non-finite %s trigger prices",
    async (orderType) => {
      const configWithoutMarkPrice = createMockConfig({
        markPrice: undefined as any,
      });
      for (const validationConfig of [config, configWithoutMarkPrice]) {
        for (const triggerPrice of [
          0,
          "0",
          -1,
          "-1",
          Number.NaN,
          "NaN",
          Number.POSITIVE_INFINITY,
          "Infinity",
        ]) {
          const result = await creator.validate(
            {
              ...values,
              tp_trigger_price: triggerPrice,
              tp_order_type: orderType,
              tp_order_price:
                orderType === OrderType.LIMIT ? "4110" : undefined,
            },
            validationConfig,
          );
          expect(result?.tp_trigger_price).toBeDefined();
        }
      }
    },
  );
  it("preserves the existing price relation, hint direction and protection range", async () => {
    // Long TP: order price below the trigger must hint "set trigger lower"
    expect(
      (await creator.validate({ ...values, tp_order_price: "4099" }, config))
        ?.tp_trigger_price,
    ).toMatchObject({ type: "priceErrorMin" });
    expect(
      (await creator.validate({ ...values, tp_order_price: "9000" }, config))
        ?.tp_order_price,
    ).toBeDefined();
    // Long SL: order price above the trigger must hint "set trigger higher"
    expect(
      (
        await creator.validate(
          {
            ...values,
            tp_trigger_price: undefined,
            sl_order_type: OrderType.LIMIT,
            sl_order_price: "3910",
          },
          config,
        )
      )?.sl_trigger_price,
    ).toMatchObject({ type: "priceErrorMax" });
    // Short TP: trigger below order price keeps the "set trigger higher" hint
    expect(
      (
        await creator.validate(
          {
            ...values,
            side: OrderSide.SELL,
            tp_trigger_price: "3900",
            tp_order_price: "3910",
            sl_trigger_price: undefined,
          },
          config,
        )
      )?.tp_trigger_price,
    ).toMatchObject({ type: "priceErrorMax" });
    // Short SL: order price below the trigger keeps the "set trigger lower" hint
    expect(
      (
        await creator.validate(
          {
            ...values,
            side: OrderSide.SELL,
            tp_trigger_price: undefined,
            sl_trigger_price: "4100",
            sl_order_type: OrderType.LIMIT,
            sl_order_price: "4090",
          },
          config,
        )
      )?.sl_trigger_price,
    ).toMatchObject({ type: "priceErrorMin" });
  });
});

describe("Bracket LIMIT payload boundary", () => {
  const bracketCreators = [
    ["limit", () => new BracketLimitOrderCreator()],
    ["market", () => new BracketMarketOrderCreator()],
  ] as const;
  const legs = ["tp", "sl"] as const;

  const createBracketValues = (
    leg: (typeof legs)[number],
    price: string | number | undefined,
    orderType = OrderType.LIMIT,
  ) =>
    createMockPaxeerNetworkOrder({
      position_type: PositionType.FULL,
      order_quantity: "2",
      tp_trigger_price: leg === "tp" ? "4100" : undefined,
      tp_order_type: leg === "tp" ? orderType : undefined,
      tp_order_price: leg === "tp" ? price : undefined,
      sl_trigger_price: leg === "sl" ? "3900" : undefined,
      sl_order_type: leg === "sl" ? orderType : undefined,
      sl_order_price: leg === "sl" ? price : undefined,
    } as any);

  it.each(bracketCreators)(
    "rejects invalid %s Bracket LIMIT prices for TP and SL",
    (_name, createCreator) => {
      for (const leg of legs) {
        for (const price of [
          undefined,
          "",
          0,
          -1,
          Number.NaN,
          Number.POSITIVE_INFINITY,
          "0.001",
        ]) {
          expect(() =>
            createCreator().create(createBracketValues(leg, price), config),
          ).toThrow(/positive finite price/);
        }
      }
    },
  );

  it.each(bracketCreators)(
    "requires config for enabled %s Bracket LIMIT children",
    (_name, createCreator) => {
      expect(() =>
        createCreator().create(createBracketValues("tp", "4110")),
      ).toThrow(/configuration is required/);
    },
  );

  it.each(bracketCreators)(
    "preserves valid %s Bracket LIMIT price values and types",
    (_name, createCreator) => {
      for (const price of ["4110", 4110]) {
        const result: any = createCreator().create(
          createBracketValues("tp", price),
          config,
        );
        expect(result.child_orders[0].child_orders[0]).toMatchObject({
          type: OrderType.LIMIT,
          price,
        });
      }
    },
  );

  it.each(bracketCreators)(
    "ignores stale prices on explicit %s Bracket MARKET children",
    (_name, createCreator) => {
      const result: any = createCreator().create(
        createBracketValues("tp", "-1", OrderType.MARKET),
        config,
      );
      expect(result.child_orders[0].child_orders[0]).toMatchObject({
        type: OrderType.CLOSE_POSITION,
      });
      expect(result.child_orders[0].child_orders[0]).not.toHaveProperty(
        "price",
      );
    },
  );
});

describe("Positional price editing", () => {
  it("sends price-only and combined updates without type or quantity", () => {
    expect(
      creator.crateUpdateOrder(
        { ...values, tp_order_price: "4120" },
        oldOrder(),
        config,
      )[0],
    ).toEqual({
      child_orders: [{ order_id: 11, price: 4120 }],
    });
    expect(
      creator.crateUpdateOrder(
        {
          ...values,
          tp_trigger_price: "4105",
          tp_order_price: "4120",
          quantity: "999",
        },
        oldOrder(),
        config,
      )[0],
    ).toEqual({
      child_orders: [{ order_id: 11, trigger_price: 4105, price: 4120 }],
    });
  });
  it("skips unchanged legs and deactivates without leftover limit fields", () => {
    expect(
      creator.crateUpdateOrder(values, oldOrder(), config)[0].child_orders,
    ).toEqual([]);
    expect(
      creator.crateUpdateOrder(
        { ...values, tp_trigger_price: "", tp_order_price: "" },
        oldOrder(),
        config,
      )[0].child_orders,
    ).toEqual([{ order_id: 11, is_activated: false }]);
  });
  it("reactivates an inactive leg by updating its trigger price", () => {
    const inactiveOrder = oldOrder();
    inactiveOrder.child_orders[1] = {
      ...inactiveOrder.child_orders[1],
      trigger_price: 0,
      is_activated: false,
    };

    expect(
      creator.crateUpdateOrder(values, inactiveOrder, config)[0].child_orders,
    ).toEqual([{ order_id: 12, trigger_price: 3900 }]);
  });
  it("reactivates an inactive leg when its trigger price is unchanged", () => {
    const inactiveOrder = oldOrder();
    inactiveOrder.child_orders[1] = {
      ...inactiveOrder.child_orders[1],
      type: OrderType.LIMIT,
      trigger_price: 3900,
      price: 3890,
      is_activated: false,
    };

    expect(
      creator.crateUpdateOrder(
        {
          ...values,
          sl_trigger_price: "3900",
          sl_order_type: OrderType.LIMIT,
          sl_order_price: "3890",
        },
        inactiveOrder,
        config,
      )[0].child_orders,
    ).toEqual([{ order_id: 12, trigger_price: 3900 }]);
  });
  it("does not update an inactive placeholder when another leg changes", () => {
    const inactiveOrder = oldOrder();
    inactiveOrder.child_orders[0] = {
      ...inactiveOrder.child_orders[0],
      type: OrderType.CLOSE_POSITION,
      trigger_price: 0,
      is_activated: false,
    };

    expect(
      createTPSLOrderUpdates(
        [
          {
            algo_type: AlgoOrderType.TAKE_PROFIT,
            type: OrderType.CLOSE_POSITION,
            is_activated: false,
          },
          {
            algo_type: AlgoOrderType.STOP_LOSS,
            type: OrderType.CLOSE_POSITION,
            is_activated: true,
            trigger_price: 3895,
          },
        ],
        inactiveOrder,
      ),
    ).toEqual([{ order_id: 12, trigger_price: 3895 }]);
  });
  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    "rejects an invalid enabled trigger price %s during editing",
    (triggerPrice) => {
      expect(() =>
        createTPSLOrderUpdates(
          [
            {
              algo_type: AlgoOrderType.TAKE_PROFIT,
              type: OrderType.CLOSE_POSITION,
              is_activated: true,
              trigger_price: triggerPrice,
            },
          ],
          oldOrder(),
        ),
      ).toThrow(/trigger price/);
    },
  );
  it("reactivates an inactive MARKET leg as LIMIT", () => {
    const inactiveOrder = oldOrder();
    inactiveOrder.child_orders[1] = {
      ...inactiveOrder.child_orders[1],
      type: OrderType.CLOSE_POSITION,
      is_activated: false,
    };

    expect(
      creator.crateUpdateOrder(
        {
          ...values,
          sl_order_type: OrderType.LIMIT,
          sl_order_price: "3890",
        },
        inactiveOrder,
        config,
      )[0].child_orders,
    ).toEqual([
      {
        order_id: 12,
        order_type: OrderType.LIMIT,
        trigger_price: 3900,
        price: 3890,
      },
    ]);
  });
  it("reactivates an inactive full-position LIMIT leg as MARKET", () => {
    const inactiveOrder = oldOrder();
    inactiveOrder.child_orders[0] = {
      ...inactiveOrder.child_orders[0],
      is_activated: false,
    };

    expect(
      creator.crateUpdateOrder(
        {
          ...values,
          tp_order_type: OrderType.MARKET,
          tp_order_price: undefined,
        },
        inactiveOrder,
        config,
      )[0].child_orders,
    ).toEqual([
      {
        order_id: 11,
        order_type: OrderType.CLOSE_POSITION,
        trigger_price: 4100,
      },
    ]);
  });
  it("reactivates an inactive partial LIMIT leg as MARKET", () => {
    const partialCreator = new TPSLOrderCreator();
    const partialValues = {
      ...values,
      position_type: PositionType.PARTIAL,
      quantity: "1",
    };
    const partialOld = {
      ...partialCreator.create(partialValues, config),
      algo_order_id: 20,
      child_orders: partialCreator
        .create(partialValues, config)
        .child_orders.map((child, index) => ({
          ...child,
          algo_order_id: index + 21,
        })),
    } as API.AlgoOrder;
    partialOld.child_orders[0].is_activated = false;

    expect(
      partialCreator.crateUpdateOrder(
        {
          ...partialValues,
          tp_order_type: OrderType.MARKET,
          tp_order_price: undefined,
        },
        partialOld,
        config,
      )[0].child_orders,
    ).toEqual([
      {
        order_id: 21,
        order_type: OrderType.MARKET,
        trigger_price: 4100,
      },
    ]);
  });
  it("rejects adding a child that is missing from the server order", () => {
    const tpOnlyOrder = oldOrder();
    tpOnlyOrder.child_orders = [tpOnlyOrder.child_orders[0]];

    expect(() =>
      creator.crateUpdateOrder(
        {
          ...values,
          sl_order_type: OrderType.LIMIT,
          sl_order_price: "3890",
        },
        tpOnlyOrder,
        config,
      ),
    ).toThrow(/missing TP\/SL child/);
  });
  it("preserves omitted legs and only deactivates explicitly cleared legs", () => {
    expect(
      creator.crateUpdateOrder(
        { ...values, tp_order_price: "4120", sl_trigger_price: undefined },
        oldOrder(),
        config,
      )[0].child_orders,
    ).toEqual([{ order_id: 11, price: 4120 }]);

    const partialCreator = new TPSLOrderCreator();
    const partialValues = {
      ...values,
      position_type: PositionType.PARTIAL,
      quantity: "1",
      tp_order_type: OrderType.LIMIT,
    };
    const partialOld = {
      ...partialCreator.create(partialValues, config),
      algo_order_id: 20,
      child_orders: partialCreator
        .create(partialValues, config)
        .child_orders.map((child, index) => ({
          ...child,
          algo_order_id: index + 21,
        })),
    } as API.AlgoOrder;

    expect(
      partialCreator.crateUpdateOrder(
        { ...partialValues, sl_trigger_price: undefined },
        partialOld,
        config,
      )[0].child_orders,
    ).toEqual([]);
    expect(
      partialCreator.crateUpdateOrder(
        { ...partialValues, sl_trigger_price: "" },
        partialOld,
        config,
      )[0].child_orders,
    ).toEqual([{ order_id: 22, is_activated: false }]);
  });
  it("updates the quantity of omitted active partial legs", () => {
    const partialCreator = new TPSLOrderCreator();
    const partialValues = {
      ...values,
      position_type: PositionType.PARTIAL,
      quantity: "1",
      tp_order_type: OrderType.LIMIT,
    };
    const partialOld = {
      ...partialCreator.create(partialValues, config),
      algo_order_id: 20,
      child_orders: partialCreator
        .create(partialValues, config)
        .child_orders.map((child, index) => ({
          ...child,
          algo_order_id: index + 21,
        })),
    } as API.AlgoOrder;

    expect(
      partialCreator.crateUpdateOrder(
        {
          ...partialValues,
          quantity: "2",
          sl_trigger_price: undefined,
        },
        partialOld,
        config,
      )[0].child_orders,
    ).toEqual([
      { order_id: 21, quantity: 2 },
      { order_id: 22, quantity: 2 },
    ]);
    expect(
      partialCreator.crateUpdateOrder(
        { ...partialValues, quantity: "2", sl_trigger_price: "" },
        partialOld,
        config,
      )[0].child_orders,
    ).toEqual([
      { order_id: 21, quantity: 2 },
      { order_id: 22, is_activated: false },
    ]);
  });
  it("rejects SDK type changes", () => {
    expect(() =>
      creator.crateUpdateOrder(
        { ...values, tp_order_type: OrderType.MARKET },
        oldOrder(),
        config,
      ),
    ).toThrow(/type/);
    expect(() =>
      creator.crateUpdateOrder(
        { ...values, sl_order_type: OrderType.LIMIT, sl_order_price: "3890" },
        oldOrder(),
        config,
      ),
    ).toThrow(/type/);
  });
  it("rejects changing the type of a triggered inactive leg", () => {
    const triggeredOrder = oldOrder();
    triggeredOrder.child_orders[0] = {
      ...triggeredOrder.child_orders[0],
      is_activated: false,
      is_triggered: true,
    };

    expect(() =>
      creator.crateUpdateOrder(
        {
          ...values,
          tp_order_type: OrderType.MARKET,
          tp_order_price: undefined,
        },
        triggeredOrder,
        config,
      ),
    ).toThrow(/type/);
  });
  it("guards low-level nested updates", () => {
    const parent = oldOrder();
    expect(
      sanitizeTPSLChildUpdates(
        [{ order_id: 11, price: 4120, quantity: 99 }],
        parent,
      ),
    ).toEqual([{ order_id: 11, price: 4120 }]);
    expect(
      sanitizeTPSLChildUpdates(
        [{ order_id: 11, is_activated: false, price: 0, quantity: 99 }],
        parent,
      ),
    ).toEqual([{ order_id: 11, is_activated: false }]);
    expect(() =>
      sanitizeTPSLChildUpdates(
        [{ order_id: 11, type: OrderType.MARKET } as any],
        parent,
      ),
    ).toThrow(/type/);
    expect(
      sanitizeTPSLChildUpdates(
        [{ order_id: 11, type: OrderType.LIMIT } as any],
        parent,
      ),
    ).toEqual([]);
    const placeholderParent = oldOrder();
    placeholderParent.child_orders[1] = {
      ...placeholderParent.child_orders[1],
      type: OrderType.CLOSE_POSITION,
      is_activated: false,
    };
    expect(
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 12,
            type: OrderType.LIMIT,
            trigger_price: 3900,
            price: 3890,
          } as any,
        ],
        placeholderParent,
      ),
    ).toEqual([
      {
        order_id: 12,
        order_type: OrderType.LIMIT,
        trigger_price: 3900,
        price: 3890,
      },
    ]);
    expect(
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 12,
            trigger_price: 3900,
            price: 3890,
          },
        ],
        placeholderParent,
      ),
    ).toEqual([{ order_id: 12, trigger_price: 3900 }]);
    const inactiveLimitParent = oldOrder();
    inactiveLimitParent.child_orders[0] = {
      ...inactiveLimitParent.child_orders[0],
      is_activated: false,
    };
    expect(
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 11,
            type: OrderType.MARKET,
            trigger_price: 4100,
            price: 4110,
          },
        ],
        inactiveLimitParent,
      ),
    ).toEqual([
      {
        order_id: 11,
        order_type: OrderType.CLOSE_POSITION,
        trigger_price: 4100,
      },
    ]);
    expect(() =>
      sanitizeTPSLChildUpdates(
        [{ order_id: 12, type: OrderType.LIMIT }],
        placeholderParent,
      ),
    ).toThrow(/trigger price/);
    expect(() =>
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 12,
            type: OrderType.LIMIT,
            trigger_price: 3900,
          },
        ],
        placeholderParent,
      ),
    ).toThrow(/limit price/);
    const bracket = {
      child_orders: [parent],
      algo_type: AlgoOrderRootType.BRACKET,
    } as API.AlgoOrder;
    expect(
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 10,
            child_orders: [{ order_id: 11, price: 4120, quantity: 99 }],
          },
        ],
        bracket,
      ),
    ).toEqual([
      { order_id: 10, child_orders: [{ order_id: 11, price: 4120 }] },
    ]);
    const inactiveBracket = {
      child_orders: [placeholderParent],
      algo_type: AlgoOrderRootType.BRACKET,
    } as API.AlgoOrder;
    expect(
      sanitizeTPSLChildUpdates(
        [
          {
            order_id: 10,
            child_orders: [
              {
                order_id: 12,
                type: OrderType.LIMIT,
                trigger_price: 3900,
                price: 3890,
              },
            ],
          },
        ],
        inactiveBracket,
      ),
    ).toEqual([
      {
        order_id: 10,
        child_orders: [
          {
            order_id: 12,
            order_type: OrderType.LIMIT,
            trigger_price: 3900,
            price: 3890,
          },
        ],
      },
    ]);
    const tpOnlyParent = oldOrder();
    tpOnlyParent.child_orders = [tpOnlyParent.child_orders[0]];
    expect(() =>
      sanitizeTPSLChildUpdates(
        [{ algo_type: AlgoOrderType.STOP_LOSS, trigger_price: 3900 } as any],
        tpOnlyParent,
      ),
    ).toThrow(/order id/);
    expect(() =>
      sanitizeTPSLChildUpdates(
        [{ order_id: 999, trigger_price: 3900 }],
        tpOnlyParent,
      ),
    ).toThrow(/order id/);
  });
});
