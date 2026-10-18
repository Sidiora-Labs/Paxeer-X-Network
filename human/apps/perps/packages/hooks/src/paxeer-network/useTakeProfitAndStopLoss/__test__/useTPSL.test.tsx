import { act, renderHook } from "@testing-library/react";
import {
  AlgoOrderRootType,
  API,
  OrderSide,
  OrderStatus,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { createMockSymbolConfig } from "../../../services/orderCreator/__test__/testHelpers";
import { useTaskProfitAndStopLossInternal } from "../useTPSL";

const mockCreateOrder = jest.fn().mockResolvedValue({ success: true });
const mockUpdateOrder = jest.fn().mockResolvedValue({ success: true });
const mockDeleteOrder = jest.fn().mockResolvedValue({ success: true });
let mockSymbolInfo = createMockSymbolConfig();
let mockMarkPrice = 4000;

jest.mock("../../../next/useOrderEntry/helper", () => ({
  appendOrderMetadata: (order: unknown) => order,
}));

jest.mock("../../../paxeerNetworkContext", () => ({
  usePaxeerNetworkContext: () => ({ orderMetadata: undefined }),
}));

jest.mock("../../../subAccount", () => ({
  useSubAccountMutation: (_url: string, method = "POST") => [
    method === "PUT" ? mockUpdateOrder : mockCreateOrder,
    { isMutating: false },
  ],
}));

jest.mock("../../../useMutation", () => ({
  useMutation: () => [mockDeleteOrder],
}));

jest.mock("../../useMarkPrice", () => ({
  useMarkPrice: () => ({ data: mockMarkPrice }),
}));

jest.mock("../../useSymbolsInfo", () => ({
  useSymbolsInfo: () => ({
    PERP_ETH_USDC: () => mockSymbolInfo,
  }),
}));

const existingFullOrder = {
  algo_order_id: 10,
  algo_type: AlgoOrderRootType.POSITIONAL_TP_SL,
  symbol: "PERP_ETH_USDC",
  quantity: 0,
  child_orders: [
    {
      algo_order_id: 11,
      algo_type: "TAKE_PROFIT",
      type: OrderType.CLOSE_POSITION,
      trigger_price: 4100,
      is_activated: true,
    },
    {
      algo_order_id: 12,
      algo_type: "STOP_LOSS",
      type: OrderType.CLOSE_POSITION,
      trigger_price: 3900,
      is_activated: true,
    },
  ],
} as API.AlgoOrder;

describe("useTPSLOrder create and edit routing", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockSymbolInfo = createMockSymbolConfig();
    mockMarkPrice = 4000;
  });

  it("creates a new partial order when defaultOrder is provided but isEditing is false", async () => {
    const { result } = renderHook(() =>
      useTaskProfitAndStopLossInternal(
        {
          symbol: "PERP_ETH_USDC",
          position_qty: 2,
          average_open_price: 4000,
        },
        {
          defaultOrder: existingFullOrder,
          isEditing: false,
          positionType: PositionType.PARTIAL,
        },
      ),
    );

    act(() => {
      result.current[1].setValues({
        quantity: 1,
        tp_trigger_price: 4200,
        tp_order_type: OrderType.MARKET,
      });
    });

    await act(async () => {
      await result.current[1].submit();
    });

    expect(mockUpdateOrder).not.toHaveBeenCalled();
    expect(mockCreateOrder).toHaveBeenCalledWith(
      expect.objectContaining({
        algo_type: AlgoOrderRootType.TP_SL,
        quantity: 1,
        child_orders: [
          expect.objectContaining({
            algo_type: "TAKE_PROFIT",
            side: OrderSide.SELL,
            type: OrderType.MARKET,
            trigger_price: 4200,
          }),
        ],
      }),
      {},
      undefined,
    );
  });

  it("rejects reactivating a LIMIT TP without a trigger price", async () => {
    const orderWithInactiveTP = {
      ...existingFullOrder,
      child_orders: [
        {
          ...existingFullOrder.child_orders[0],
          type: OrderType.LIMIT,
          trigger_price: 0,
          is_activated: false,
        },
        {
          ...existingFullOrder.child_orders[1],
          type: OrderType.LIMIT,
          price: 3890,
        },
      ],
    } as API.AlgoOrder;

    const { result } = renderHook(() =>
      useTaskProfitAndStopLossInternal(
        {
          symbol: "PERP_ETH_USDC",
          position_qty: 2,
          average_open_price: 4000,
        },
        {
          defaultOrder: orderWithInactiveTP,
          isEditing: true,
          positionType: PositionType.FULL,
        },
      ),
    );

    act(() => {
      result.current[1].setValues({ tp_order_price: 4210 });
    });

    let validationError: unknown;
    await act(async () => {
      try {
        await result.current[1].validate();
      } catch (error) {
        validationError = error;
      }
    });

    expect(validationError).toMatchObject({
      tp_trigger_price: { type: "required" },
    });
    expect(result.current[1].metaState).toMatchObject({
      validated: true,
      errors: { tp_trigger_price: { type: "required" } },
    });
    expect(mockUpdateOrder).not.toHaveBeenCalled();
  });

  it("reactivates a default-market TP placeholder without changing its type", async () => {
    const orderWithInactiveTP = {
      ...existingFullOrder,
      child_orders: [
        {
          ...existingFullOrder.child_orders[0],
          type: OrderType.CLOSE_POSITION,
          trigger_price: 0,
          is_activated: false,
        },
        {
          ...existingFullOrder.child_orders[1],
          type: OrderType.LIMIT,
          price: 3890,
        },
      ],
    } as API.AlgoOrder;

    const { result } = renderHook(() =>
      useTaskProfitAndStopLossInternal(
        {
          symbol: "PERP_ETH_USDC",
          position_qty: 2,
          average_open_price: 4000,
        },
        {
          defaultOrder: orderWithInactiveTP,
          isEditing: true,
          positionType: PositionType.FULL,
        },
      ),
    );

    act(() => {
      result.current[1].setValues({
        tp_trigger_price: 4100,
        tp_order_price: 4110,
      });
    });

    await act(async () => {
      await result.current[1].submit();
    });

    expect(mockUpdateOrder).toHaveBeenCalledWith(
      {
        order_id: 10,
        child_orders: [{ order_id: 11, trigger_price: 4100 }],
      },
      {},
      undefined,
    );
  });

  it("reactivates an inactive full-position MARKET leg as LIMIT", async () => {
    const orderWithInactiveTP = {
      ...existingFullOrder,
      child_orders: [
        {
          ...existingFullOrder.child_orders[0],
          trigger_price: 0,
          is_activated: false,
        },
        existingFullOrder.child_orders[1],
      ],
    } as API.AlgoOrder;

    const { result } = renderHook(() =>
      useTaskProfitAndStopLossInternal(
        {
          symbol: "PERP_ETH_USDC",
          position_qty: 2,
          average_open_price: 4000,
        },
        {
          defaultOrder: orderWithInactiveTP,
          isEditing: true,
          positionType: PositionType.FULL,
        },
      ),
    );

    act(() => {
      result.current[1].setValues({
        tp_trigger_price: 4100,
        tp_order_type: OrderType.LIMIT,
        tp_order_price: 4110,
      });
    });

    await act(async () => {
      await result.current[1].submit();
    });

    expect(mockUpdateOrder).toHaveBeenCalledWith(
      {
        order_id: 10,
        child_orders: [
          {
            order_id: 11,
            order_type: OrderType.LIMIT,
            trigger_price: 4100,
            price: 4110,
          },
        ],
      },
      {},
      undefined,
    );
  });

  it.each([
    { slActivated: true, emptyTrigger: undefined },
    { slActivated: false, emptyTrigger: undefined },
    { slActivated: false, emptyTrigger: "" },
  ])(
    "allows a price-only edit after triggering with %j",
    async ({ slActivated, emptyTrigger }) => {
      mockMarkPrice = 2471.7;
      const triggeredLimitOrder = {
        ...existingFullOrder,
        is_triggered: true,
        child_orders: [
          {
            ...existingFullOrder.child_orders[0],
            type: OrderType.LIMIT,
            trigger_price: 2470.9,
            price: 2490,
            status: OrderStatus.NEW,
            is_triggered: false,
          },
          {
            ...existingFullOrder.child_orders[1],
            algo_status: OrderStatus.CANCELLED,
            is_activated: slActivated,
          },
        ],
      } as API.AlgoOrder;
      const { result } = renderHook(() =>
        useTaskProfitAndStopLossInternal(
          {
            symbol: "PERP_ETH_USDC",
            position_qty: 2,
            average_open_price: 4000,
          },
          {
            defaultOrder: triggeredLimitOrder,
            isEditing: true,
            positionType: PositionType.FULL,
          },
        ),
      );

      act(() => {
        result.current[1].setValues({ tp_order_price: 2485 });
        if (emptyTrigger !== undefined) {
          result.current[1].setValues({ sl_trigger_price: emptyTrigger });
        }
      });

      await act(async () => {
        await expect(result.current[1].validate()).resolves.toBeDefined();
        await result.current[1].submit();
      });

      expect(mockUpdateOrder).toHaveBeenCalledWith(
        {
          order_id: 10,
          child_orders: [{ order_id: 11, price: 2485 }],
        },
        {},
        undefined,
      );
    },
  );

  it.each([
    ["tp_offset", 200],
    ["tp_offset_percentage", 0.05],
    ["tp_offset_from_mark", 200],
    ["tp_offset_percentage_from_mark", 0.05],
    ["tp_pnl", 400],
  ])(
    "edits triggered Limit price through %s without changing the trigger",
    async (key, value) => {
      const defaultOrder = {
        ...existingFullOrder,
        is_triggered: true,
        child_orders: [
          {
            ...existingFullOrder.child_orders[0],
            type: OrderType.LIMIT,
            price: 4110,
          },
          existingFullOrder.child_orders[1],
        ],
      } as API.AlgoOrder;
      const { result } = renderHook(() =>
        useTaskProfitAndStopLossInternal(
          {
            symbol: "PERP_ETH_USDC",
            position_qty: 2,
            average_open_price: 4000,
          },
          { defaultOrder, isEditing: true, positionType: PositionType.FULL },
        ),
      );

      act(() => {
        result.current[1].setValue(key as string, value);
      });

      expect(Number(result.current[0].tp_trigger_price)).toBe(4100);
      expect(Number(result.current[0].tp_order_price)).toBe(4200);
      await act(async () => {
        await expect(result.current[1].validate()).resolves.toBeDefined();
        await result.current[1].submit();
      });
      expect(mockUpdateOrder).toHaveBeenCalledWith(
        { order_id: 10, child_orders: [{ order_id: 11, price: 4200 }] },
        {},
        undefined,
      );
    },
  );

  it.each([
    { activated: true, trigger: 3901 },
    { activated: true, trigger: "" },
    { activated: false, trigger: 3900 },
  ])(
    "rejects a trigger change after triggering with %j",
    async ({ activated, trigger }) => {
      const triggeredLimitOrder = {
        ...existingFullOrder,
        is_triggered: true,
        child_orders: [
          {
            ...existingFullOrder.child_orders[0],
            type: OrderType.LIMIT,
            trigger_price: 3900,
            price: 3950,
            is_triggered: true,
            is_activated: activated,
          },
          existingFullOrder.child_orders[1],
        ],
      } as API.AlgoOrder;
      const { result } = renderHook(() =>
        useTaskProfitAndStopLossInternal(
          {
            symbol: "PERP_ETH_USDC",
            position_qty: 2,
            average_open_price: 4000,
          },
          {
            defaultOrder: triggeredLimitOrder,
            isEditing: true,
            positionType: PositionType.FULL,
          },
        ),
      );

      act(() => {
        result.current[1].setValues({ tp_trigger_price: trigger });
      });

      let validationError: unknown;
      await act(async () => {
        try {
          await result.current[1].validate();
        } catch (error) {
          validationError = error;
        }
      });

      expect(validationError).toMatchObject({
        tp_trigger_price: {
          message: "Trigger price cannot be changed after TP/SL is triggered",
        },
      });
      expect(mockUpdateOrder).not.toHaveBeenCalled();
    },
  );
});
