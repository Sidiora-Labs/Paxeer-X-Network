import { act, renderHook } from "@testing-library/react-hooks";
import { MarginMode, OrderSide, OrderType } from "@paxeer-network/types";
import {
  DEFAULT_USDC_BORROW_LIMIT,
  USDCBorrowLimitExceededError,
} from "../usdcBorrowLimit";
import { useOrderEntry } from "../useOrderEntry";

const mockSymbol = "PERP_BTC_USDC";
const mockGeneratedOrder = {
  symbol: mockSymbol,
  side: OrderSide.BUY,
  order_type: OrderType.LIMIT,
  order_price: "100",
  order_quantity: "1",
  reduce_only: false,
  margin_mode: MarginMode.ISOLATED,
};
const mockFormattedOrder = {
  ...mockGeneratedOrder,
  order_quantity: "0",
};

const mockCreateOrder = jest.fn();
const mockValidate = jest.fn();
const mockGenerateOrder = jest.fn();
let mockOrderbookUpdate: ((data: unknown) => void) | undefined;
const mockPositionRows: Array<{
  symbol: string;
  margin_mode: MarginMode;
  position_qty: number;
  pending_long_qty: number;
  pending_short_qty: number;
  leverage: number;
}> = [];
const mockPortfolio = {
  holding: [
    {
      token: "USDC",
      holding: 100,
      frozen: 0,
      pending_short: 0,
      isolated_margin: 0,
      isolated_order_frozen: 0,
      updated_time: 0,
    },
  ],
  unsettledPnL: 0,
};

jest.mock("../../../paxeer-network/appStore", () => ({
  useAccountInfo: () => undefined,
  useAppStore: {
    getState: () => ({ portfolio: mockPortfolio }),
  },
}));

jest.mock("../../../paxeer-network/paxeerNetworkHooks", () => ({
  useCollateral: () => ({ freeCollateral: 0, totalCollateral: 0 }),
  useFundingRatesStore: () => ({}),
  useMaxQty: jest.fn(() => 10),
  useSymbolLeverageMap: () => ({
    getSymbolLeverage: () => 10,
    refresh: jest.fn(),
  }),
  useSymbolsInfo: () => ({
    [mockSymbol]: () => ({
      symbol: mockSymbol,
      base_dp: 4,
      quote_dp: 2,
      base_tick: 0.0001,
      price_range: 0.05,
    }),
  }),
}));

jest.mock("../../../paxeer-network/useMarkPrice/useMarkPriceStore", () => ({
  useMarkPriceActions: () => ({ getMarkPriceBySymbol: () => 100 }),
}));

jest.mock("../../../paxeer-network/useOrderStream/useOrderStream", () => ({
  useOrderStream: () => [null, {}],
}));

jest.mock("../../../paxeer-network/usePositionStream/usePosition.store", () => ({
  usePositions: () => mockPositionRows,
  usePositionStore: {
    getState: () => ({ positions: { all: { rows: mockPositionRows } } }),
  },
}));

jest.mock("../../../paxeerNetworkContext", () => ({
  usePaxeerNetworkContext: () => ({ orderMetadata: undefined }),
}));

jest.mock("../../../provider/store/symbolStore", () => ({
  useSymbolStore: (selector: (state: { fetchData: jest.Mock }) => unknown) =>
    selector({ fetchData: jest.fn() }),
}));

jest.mock("../../../useConfig", () => ({ useConfig: () => "" }));
jest.mock("../../../useEventEmitter", () => ({
  useEventEmitter: () => ({
    on: jest.fn((event: string, callback: (data: unknown) => void) => {
      if (event === "orderbook:update") {
        mockOrderbookUpdate = callback;
      }
    }),
    off: jest.fn(),
  }),
}));
jest.mock("../../../useMutation", () => ({
  useMutation: () => [mockCreateOrder, { isMutating: false }],
}));
jest.mock("../../../useTrack", () => ({
  useTrack: () => ({ track: jest.fn() }),
}));
jest.mock("../../../utils/order/orderPrice", () => ({
  ...jest.requireActual("../../../utils/order/orderPrice"),
  getOrderReferencePriceFromOrder: jest.fn(() => 100),
}));
jest.mock("../useOrderEntry.internal", () => ({
  useOrderEntryNextInternal: () => ({
    formattedOrder: mockFormattedOrder,
    setValue: jest.fn(),
    setValues: jest.fn(),
    setValuesRaw: jest.fn(),
    validate: mockValidate,
    generateOrder: mockGenerateOrder,
    reset: jest.fn(),
    onMarkPriceChange: jest.fn(),
  }),
}));
jest.mock("../useRwaLeverageSync", () => ({
  useRwaLeverageSync: jest.fn(),
}));
jest.mock("use-debounce", () => ({
  useDebouncedCallback: (callback: (data: unknown) => void) => callback,
}));

const mockUseMaxQty = jest.requireMock("../../../paxeer-network/paxeerNetworkHooks")
  .useMaxQty as jest.Mock;
const mockGetOrderReferencePriceFromOrder = jest.requireMock(
  "../../../utils/order/orderPrice",
).getOrderReferencePriceFromOrder as jest.Mock;

describe("useOrderEntry USDC borrow limit submission guard", () => {
  beforeEach(() => {
    mockCreateOrder.mockReset();
    mockValidate.mockReset().mockResolvedValue({});
    mockGenerateOrder.mockReset().mockReturnValue(mockGeneratedOrder);
    mockUseMaxQty.mockClear();
    mockGetOrderReferencePriceFromOrder.mockReset().mockReturnValue(100);
    Object.assign(mockFormattedOrder, {
      ...mockGeneratedOrder,
      order_quantity: "0",
    });
    delete (mockFormattedOrder as { slippage?: number }).slippage;
    mockPortfolio.holding[0].holding = 100;
    mockPortfolio.unsettledPnL = 0;
    mockPositionRows.splice(0);
    mockOrderbookUpdate = undefined;
  });

  const publishOrderbook = () => {
    act(() => {
      mockOrderbookUpdate?.({
        asks: [[100, 10]],
        bids: [[99, 10]],
      });
    });
  };

  it("keeps the BBO gate for max qty while the orderbook is unavailable", () => {
    Object.assign(mockFormattedOrder, {
      order_type: OrderType.MARKET,
      slippage: 1,
    });
    mockGetOrderReferencePriceFromOrder.mockImplementation((order) =>
      order.side === OrderSide.BUY ? 110 : 90,
    );

    renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );

    expect(mockGetOrderReferencePriceFromOrder).not.toHaveBeenCalled();
    expect(mockUseMaxQty).toHaveBeenCalledWith(mockSymbol, OrderSide.BUY, {
      reduceOnly: false,
      marginMode: MarginMode.ISOLATED,
      currentOrderReferencePrice: undefined,
    });
    expect(mockUseMaxQty).toHaveBeenCalledWith(mockSymbol, OrderSide.SELL, {
      reduceOnly: false,
      marginMode: MarginMode.ISOLATED,
      currentOrderReferencePrice: undefined,
    });
  });

  it("keeps CROSS MARKET orders on the legacy Ask1/Bid1 reference", () => {
    Object.assign(mockFormattedOrder, {
      order_type: OrderType.MARKET,
      margin_mode: MarginMode.CROSS,
      slippage: 1,
    });
    mockGetOrderReferencePriceFromOrder.mockImplementation(() => 100);

    renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.CROSS },
      }),
    );
    publishOrderbook();

    expect(mockGetOrderReferencePriceFromOrder).toHaveBeenCalledWith(
      expect.objectContaining({ side: OrderSide.BUY }),
      [100, 99],
      undefined,
    );
    // cross MARKET BUY ignores the slippage markup
    expect(mockUseMaxQty).toHaveBeenCalledWith(
      mockSymbol,
      OrderSide.BUY,
      expect.objectContaining({ currentOrderReferencePrice: 100 }),
    );
  });

  it("clamps the isolated MARKET BUY reference against the mid-price slippage", () => {
    Object.assign(mockFormattedOrder, {
      order_type: OrderType.MARKET,
      slippage: 1,
    });
    mockGetOrderReferencePriceFromOrder.mockImplementation(() => 110);

    renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );
    publishOrderbook(); // asks [[100, 10]], bids [[99, 10]] -> mid 99.5

    expect(mockGetOrderReferencePriceFromOrder).toHaveBeenCalledWith(
      expect.objectContaining({ side: OrderSide.BUY }),
      [100, 99],
      { markPrice: 100, priceRange: 0.05, pricePrecision: 2 },
    );
    // min(110, 99.5 * 1.01)
    expect(mockUseMaxQty).toHaveBeenCalledWith(
      mockSymbol,
      OrderSide.BUY,
      expect.objectContaining({ currentOrderReferencePrice: 100.495 }),
    );
  });

  it("rechecks current portfolio state and blocks mutation when the limit is exceeded", async () => {
    const { result } = renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );
    publishOrderbook();

    expect(
      result.current.helper.getProjectedUSDCBorrow(mockGeneratedOrder),
    ).toBe(0);

    mockPortfolio.holding[0].holding = -50_000;

    let thrownError: unknown;
    await act(async () => {
      try {
        await result.current.submit({ usdcBorrowLimit: 50_000 });
      } catch (error) {
        thrownError = error;
      }
    });

    expect(thrownError).toBeInstanceOf(USDCBorrowLimitExceededError);
    expect(mockCreateOrder).not.toHaveBeenCalled();
  });

  it("falls back to the default limit when the threshold is unavailable", async () => {
    const { result } = renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );
    publishOrderbook();

    mockPortfolio.holding[0].holding = -50_000;

    let thrownError: unknown;
    await act(async () => {
      try {
        await result.current.submit();
      } catch (error) {
        thrownError = error;
      }
    });

    expect(thrownError).toBeInstanceOf(USDCBorrowLimitExceededError);
    expect((thrownError as USDCBorrowLimitExceededError).borrowLimit).toBe(
      DEFAULT_USDC_BORROW_LIMIT,
    );
    expect(mockCreateOrder).not.toHaveBeenCalled();
  });

  it("preserves the mutation parameters and result when under the limit", async () => {
    type SubmitResult = {
      success: boolean;
      data: Record<string, any>;
      timestamp: number;
    };
    const response: SubmitResult = {
      success: true,
      data: { order_id: 1 },
      timestamp: 1,
    };
    mockCreateOrder.mockResolvedValue(response);
    const { result } = renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );
    publishOrderbook();

    let submitResult: SubmitResult | undefined;
    await act(async () => {
      submitResult = await result.current.submit({
        resetOnSuccess: false,
        usdcBorrowLimit: 50_000,
      });
    });

    expect(mockCreateOrder).toHaveBeenCalledTimes(1);
    expect(mockCreateOrder).toHaveBeenCalledWith(mockGeneratedOrder);
    expect(submitResult).toBe(response);
  });

  it("matches the current position by both symbol and margin mode", () => {
    mockPortfolio.holding[0].holding = -49_990;
    mockPositionRows.push(
      {
        symbol: mockSymbol,
        margin_mode: MarginMode.CROSS,
        position_qty: -10,
        pending_long_qty: 0,
        pending_short_qty: 0,
        leverage: 10,
      },
      {
        symbol: mockSymbol,
        margin_mode: MarginMode.ISOLATED,
        position_qty: 0,
        pending_long_qty: 0,
        pending_short_qty: 0,
        leverage: 10,
      },
    );
    const { result } = renderHook(() =>
      useOrderEntry(mockSymbol, {
        initialOrder: { margin_mode: MarginMode.ISOLATED },
      }),
    );
    publishOrderbook();

    expect(
      result.current.helper.getProjectedUSDCBorrow(mockGeneratedOrder),
    ).toBeCloseTo(50_000.06, 8);
  });
});
