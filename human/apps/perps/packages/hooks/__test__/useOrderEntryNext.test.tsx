// import { renderHook } from "@testing-library/react-hooks";
import { act, renderHook } from "@testing-library/react-hooks";
import {
  AlgoOrderRootType,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { PaxeerNetworkConfigProvider } from "../lib";
import { useOrderStore } from "../src/next/useOrderEntry/orderEntry.store";
import { useOrderEntryNextInternal } from "../src/next/useOrderEntry/useOrderEntry.internal";

// beforeEach(() => {
//   useOrderStore.getState().actions.resetOrder();
// });

// const configWrapper = ({ children }) => {
//   return (
//     <PaxeerNetworkConfigProvider
//       brokerId={"paxeer-network"}
//       brokerName={""}
//       networkId={"testnet"}
//     >
//       {children}
//     </PaxeerNetworkConfigProvider>
//   );
// };

describe("order state initial", () => {
  beforeEach(() => {
    useOrderStore.getState().actions.resetOrder();
  });
  test("when not provider symbol then throw error", () => {
    // const { result } = renderHook(() => useOrderEntryNext());

    // @ts-ignore
    expect(() => useOrderEntryNextInternal()).toThrow("Symbol is required");
  });

  test("pass symbol", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC"),
    );

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
    });
  });

  test("initial order state", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        initialOrder: {
          side: OrderSide.BUY,
          order_type: OrderType.LIMIT,
        },
      }),
    );
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      order_type: OrderType.LIMIT,
    });
  });

  test("update order symbol from props", () => {
    const { result, rerender } = renderHook(
      ({ symbol }) => useOrderEntryNextInternal(symbol),
      {
        initialProps: { symbol: "PERP_ETH_USDC" },
      },
    );
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
    });
    rerender({ symbol: "PERP_BTC_USDC" });
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_BTC_USDC",
    });
  });
  test("can't update initial state", () => {
    const { result, rerender } = renderHook(
      ({ symbol, options }) => useOrderEntryNextInternal(symbol, options),
      {
        initialProps: {
          symbol: "PERP_ETH_USDC",
          options: {
            initialOrder: {
              side: OrderSide.BUY,
              order_type: OrderType.LIMIT,
            },
          },
        },
      },
    );

    rerender({
      symbol: "PERP_BTC_USDC",
      options: {
        initialOrder: {
          side: OrderSide.SELL,
          order_type: OrderType.MARKET,
        },
      },
    });
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_BTC_USDC",
      side: OrderSide.BUY,
      order_type: OrderType.LIMIT,
    });
  });
});

describe("order state update, LIMIT Order", () => {
  useOrderStore.getState().actions.resetOrder();

  test("update order side", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC"),
    );

    act(() => {
      result.current.setValue("side", OrderSide.BUY);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      type: OrderType.LIMIT, // the default value
    });
  });

  // Bracket order
  test(`update order type: BRACKET ORDER`, () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC"),
    );

    act(() => {
      result.current.setValue("algo_type", AlgoOrderRootType.BRACKET);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      algo_type: AlgoOrderRootType.BRACKET,
      type: OrderType.LIMIT, // the default value
    });
  });

  test("input order qty", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        initialOrder: {
          side: OrderSide.BUY,
          type: OrderType.LIMIT,
        },
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );

    // set quantity
    act(() => {
      result.current.setValue("quantity", 1);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      quantity: "1",
      side: OrderSide.BUY,
      type: OrderType.LIMIT,
      total: "",
    });
  });

  test("input price", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );

    act(() => {
      result.current.setValue("price", 1000);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      quantity: "1",
      side: OrderSide.BUY,
      type: OrderType.LIMIT,
      total: "1000",
      price: "1000",
    });
  });
  test("input qty: 0.8", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );

    act(() => {
      result.current.setValue("quantity", 0.8);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      quantity: "0.8",
      side: OrderSide.BUY,
      type: OrderType.LIMIT,
      total: "800",
      price: "1000",
    });
  });
  test("input total: 700", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );

    act(() => {
      result.current.setValue("total", 700);
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      quantity: "0.7",
      side: OrderSide.BUY,
      type: OrderType.LIMIT,
      total: "700",
      price: "1000",
    });
  });
});

describe("order state update,STOP LIMIT Order", () => {
  useOrderStore.getState().actions.resetOrder();
  test("update order type: STOP LIMIT", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );
    act(() => {
      result.current.setValue("type", OrderType.STOP_LIMIT);
    });
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      type: OrderType.STOP_LIMIT,
      // algo_type: AlgoOrderRootType.STOP,
    });
  });

  test("update trigger price", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        initialOrder: {
          side: OrderSide.BUY,
          type: OrderType.STOP_LIMIT,
        },
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );
    act(() => {
      result.current.setValue("trigger_price", 1000);
    });
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      type: OrderType.STOP_LIMIT,
      // algo_type: AlgoOrderRootType.STOP,
      trigger_price: "1000",
    });
  });
  test("update order price", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        initialOrder: {
          side: OrderSide.BUY,
          type: OrderType.STOP_LIMIT,
        },
        symbolInfo: {
          base_dp: 3,
          quote_dp: 3,
        },
      }),
    );
    act(() => {
      result.current.setValue("price", 1000);
    });
    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      type: OrderType.STOP_LIMIT,
      price: "1000",
      // algo_type: AlgoOrderRootType.STOP,
      // trigger_price: "1000",
    });
  });
});

describe("MARKET Order", () => {
  test("input qty, calc total", () => {
    const { result } = renderHook(() =>
      useOrderEntryNextInternal("PERP_ETH_USDC", {
        initialOrder: {
          side: OrderSide.BUY,
          type: OrderType.STOP_MARKET,
        },
        symbolInfo: {
          base_dp: 4,
          quote_dp: 2,
        },
      }),
    );

    act(() => {
      result.current.setValue("quantity", 0.25, {
        markPrice: 2404.38,
      });
    });

    expect(result.current.formattedOrder).toEqual({
      symbol: "PERP_ETH_USDC",
      side: OrderSide.BUY,
      type: OrderType.STOP_MARKET,
      price: "",
      quantity: "0.25",
      total: "601.09",
      // algo_type: AlgoOrderRootType.STOP,
      // trigger_price: "1000",
    });
  });
});
