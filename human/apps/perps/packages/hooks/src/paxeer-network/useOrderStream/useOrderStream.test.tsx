import { act, renderHook } from "@testing-library/react";
import {
  AlgoOrderRootType,
  OrderEntity,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { createMockSymbolConfig } from "../../services/orderCreator/__test__/testHelpers";
import { useOrderStream } from "./useOrderStream";

const mockUpdateAlgoOrder = jest.fn().mockResolvedValue({ success: true });
const mockSymbolInfo = createMockSymbolConfig();
let mockOrders: any[];
let mockMarkPrice: number;

jest.mock("../../provider/dataCenter/dataCenterContext", () => ({
  useDataCenterContext: () => ({}),
}));
jest.mock("../../useMutation", () => ({
  useMutation: () => [mockUpdateAlgoOrder, {}],
}));
jest.mock("../../usePrivateInfiniteQuery", () => ({
  usePrivateInfiniteQuery: (key: () => string) => ({
    data: [{ rows: key().includes("/algo/orders") ? mockOrders : [] }],
    mutate: jest.fn(),
  }),
}));
jest.mock("../useMarkPricesStream", () => ({
  useMarkPricesStream: () => ({ data: { PERP_ETH_USDC: mockMarkPrice } }),
}));
jest.mock("../useSymbolsInfo", () => ({
  useSymbolsInfo: () => ({ PERP_ETH_USDC: () => mockSymbolInfo }),
}));

const createOrder = (fullPosition = false) => ({
  algo_order_id: 10,
  algo_type: fullPosition
    ? AlgoOrderRootType.POSITIONAL_TP_SL
    : AlgoOrderRootType.TP_SL,
  symbol: "PERP_ETH_USDC",
  is_triggered: false,
  child_orders: [
    {
      algo_order_id: 11,
      parent_algo_order_id: 10,
      root_algo_order_id: 10,
      algo_type: "TAKE_PROFIT",
      type: OrderType.LIMIT,
      side: OrderSide.SELL,
      quantity: 1,
      trigger_price: 4100,
      price: 4130,
      is_activated: true,
      is_triggered: false,
    },
  ],
});

describe("useOrderStream TP/SL updates", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockOrders = [createOrder()];
    mockMarkPrice = 4000;
  });

  it.each([undefined, "4120"])(
    "preserves partial-position quantity when the new price is %s",
    async (price) => {
      const { result } = renderHook(() => useOrderStream({}));
      await act(async () => {
        await result.current[1].updateAlgoOrder("11", {
          order_quantity: "2",
          order_price: price,
        } as OrderEntity);
      });
      expect(mockUpdateAlgoOrder).toHaveBeenCalledWith({
        order_id: 10,
        child_orders: [
          { order_id: 11, quantity: 2, ...(price && { price: 4120 }) },
        ],
      });
    },
  );

  it("omits quantities when editing an entire-position leg", async () => {
    mockOrders = [createOrder(true)];
    const { result } = renderHook(() => useOrderStream({}));
    await act(async () => {
      await result.current[1].updateAlgoOrder("11", {
        order_quantity: "2",
        order_price: "4120",
      } as OrderEntity);
    });
    expect(mockUpdateAlgoOrder).toHaveBeenCalledWith({
      order_id: 10,
      child_orders: [{ order_id: 11, price: 4120 }],
    });
  });

  it.each([
    { fullPosition: false, parentTriggered: true, childTriggered: false },
    { fullPosition: true, parentTriggered: true, childTriggered: false },
    { fullPosition: false, parentTriggered: false, childTriggered: true },
  ])("allows triggered Limit price edits with %j", async (state) => {
    mockOrders = [createOrder(state.fullPosition)];
    mockOrders[0].is_triggered = state.parentTriggered;
    mockOrders[0].child_orders[0].is_triggered = state.childTriggered;
    mockMarkPrice = 4110;
    const { result } = renderHook(() => useOrderStream({}));
    await act(async () => {
      await result.current[1].updateAlgoOrder("11", {
        order_type: OrderType.LIMIT,
        order_price: "4120",
        trigger_price: "4100.00",
      } as OrderEntity);
    });
    expect(mockUpdateAlgoOrder).toHaveBeenCalledWith({
      order_id: 10,
      child_orders: [{ order_id: 11, price: 4120 }],
    });
  });

  it("uses the TP/SL parent stage for nested Bracket updates", async () => {
    const parent = createOrder();
    parent.is_triggered = true;
    parent.child_orders[0].root_algo_order_id = 20;
    mockOrders = [
      {
        algo_order_id: 20,
        algo_type: AlgoOrderRootType.BRACKET,
        symbol: parent.symbol,
        child_orders: [parent],
      },
    ];
    mockMarkPrice = 4110;
    const { result } = renderHook(() => useOrderStream({}));
    await act(async () => {
      await result.current[1].updateTPSLOrder(20, [
        { order_id: 10, child_orders: [{ order_id: 11, price: 4120 }] },
      ]);
    });
    expect(mockUpdateAlgoOrder).toHaveBeenCalledWith({
      order_id: 20,
      child_orders: [
        { order_id: 10, child_orders: [{ order_id: 11, price: 4120 }] },
      ],
    });
  });

  it.each([
    { triggered: false, price: "4120", trigger: "4100" },
    { triggered: true, price: "4120", trigger: "4090" },
    { triggered: true, price: "0", trigger: "4100" },
    { triggered: true, price: "100", trigger: "4100" },
  ])(
    "retains price validation for %j",
    async ({ triggered, price, trigger }) => {
      mockOrders[0].is_triggered = triggered;
      mockMarkPrice = 4110;
      const { result } = renderHook(() => useOrderStream({}));
      await act(async () => {
        await expect(
          result.current[1].updateAlgoOrder("11", {
            order_price: price,
            trigger_price: trigger,
          } as OrderEntity),
        ).rejects.toThrow();
      });
      expect(mockUpdateAlgoOrder).not.toHaveBeenCalled();
    },
  );
});
