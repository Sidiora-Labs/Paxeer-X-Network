import { renderHook } from "@testing-library/react-hooks";
import { MarginMode, OrderSide } from "@paxeer-network/types";
import { useMaxQty } from "../useMaxQty";

const mockMaxQtyForIsolatedMargin = jest.fn((..._args: unknown[]) => 42);

const mockPositionRows: Array<{
  symbol: string;
  margin_mode: MarginMode;
  position_qty: number;
  pending_long_qty: number;
  pending_short_qty: number;
  leverage: number;
}> = [];

jest.mock("@paxeer-network/perp", () => {
  const actual = jest.requireActual("@paxeer-network/perp");
  return {
    ...actual,
    account: {
      ...actual.account,
      maxQtyForIsolatedMargin: (...args: unknown[]) =>
        mockMaxQtyForIsolatedMargin(...args),
    },
  };
});

jest.mock("../appStore", () => ({
  useAccountInfo: () => ({
    imr_factor: { PERP_BTC_USDC: 0.0000001 },
    max_notional: { PERP_BTC_USDC: 1000000 },
  }),
}));

jest.mock("../useCollateral", () => ({
  useCollateral: () => ({
    totalCollateral: 1000,
    freeCollateral: 250,
    freeCollateralUSDCOnly: 10,
  }),
}));

jest.mock("../useLeverageBySymbol", () => ({
  useLeverageBySymbol: () => 10,
}));

jest.mock("../useMarkPricesStream", () => ({
  useMarkPricesStream: () => ({
    data: { PERP_BTC_USDC: 50000 },
  }),
}));

jest.mock("../useOrderStream/useOrderStream", () => ({
  useOrderStream: () => [null, {}],
}));

jest.mock("../usePositionStream/usePosition.store", () => ({
  usePositions: () => mockPositionRows,
}));

jest.mock("../useSymbolsInfo", () => ({
  useSymbolsInfo: () => ({
    PERP_BTC_USDC: (key: string) =>
      ({
        base_imr: 0.1,
        base_max: 100,
      })[key],
  }),
}));

describe("useMaxQty", () => {
  beforeEach(() => {
    mockMaxQtyForIsolatedMargin.mockClear();
    mockPositionRows.splice(0);
  });

  it("should use total free collateral for isolated-margin orders", () => {
    const { result } = renderHook(() =>
      useMaxQty("PERP_BTC_USDC", OrderSide.BUY, {
        marginMode: MarginMode.ISOLATED,
      }),
    );

    expect(result.current).toBe(42);
    expect(mockMaxQtyForIsolatedMargin).toHaveBeenCalledWith(
      expect.objectContaining({
        availableBalance: 250,
        isolatedPendingOrders: [],
      }),
    );
  });

  it("should synthesize aggregate pending orders at mark price when the stream is unavailable", () => {
    mockPositionRows.push({
      symbol: "PERP_BTC_USDC",
      margin_mode: MarginMode.ISOLATED,
      position_qty: 1,
      pending_long_qty: 0,
      pending_short_qty: 2,
      leverage: 10,
    });

    renderHook(() =>
      useMaxQty("PERP_BTC_USDC", OrderSide.SELL, {
        marginMode: MarginMode.ISOLATED,
      }),
    );

    // The flip binary search must always receive per-order input so it runs
    // the close/open allocation, even while the order stream is loading
    expect(mockMaxQtyForIsolatedMargin).toHaveBeenCalledWith(
      expect.objectContaining({
        availableBalance: 250,
        isolatedPendingOrders: [
          { side: OrderSide.SELL, referencePrice: 50000, quantity: 2 },
        ],
      }),
    );
  });
});
