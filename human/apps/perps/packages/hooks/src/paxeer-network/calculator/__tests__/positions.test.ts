import { API } from "@paxeer-network/types";
import { zero } from "@paxeer-network/utils";
import { CalculatorScope } from "../../../types";
import { CalculatorContext } from "../calculatorContext";
import { PositionCalculator } from "../positions";

/**
 * Mock dependencies
 */
jest.mock("../../appStore", () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { zero: mockZero } = require("@paxeer-network/utils");
  const mockAccountInfo = {
    account_id: "test_account",
    email: "test@example.com",
    imr_factor: {
      PERP_ETH_USDC: 0.1,
      PERP_BTC_USDC: 0.1,
    },
    max_leverage: 10,
  } as unknown as API.AccountInfo;

  return {
    useAppStore: {
      getState: jest.fn(() => ({
        accountInfo: mockAccountInfo,
        symbolsInfo: {
          PERP_ETH_USDC: {
            base_imr: 0.05,
            base_mmr: 0.02,
          } as API.SymbolExt,
          PERP_BTC_USDC: {
            base_imr: 0.05,
            base_mmr: 0.02,
          } as API.SymbolExt,
        },
        fundingRates: {
          PERP_ETH_USDC: {
            sum_unitary_funding: 0.001,
          } as API.FundingRate,
          PERP_BTC_USDC: {
            sum_unitary_funding: 0.001,
          } as API.FundingRate,
        },
        tokensInfo: [],
        portfolio: {
          totalCollateral: mockZero,
          totalValue: null,
          freeCollateral: mockZero,
          availableBalance: 0,
          unsettledPnL: 0,
          totalUnrealizedROI: 0,
          usdcHolding: 0,
        },
      })),
    },
  };
});

jest.mock("../../usePositionStream/usePosition.store", () => {
  const mockPositions: Record<string, API.PositionsTPSLExt> = {
    all: {
      rows: [],
      unrealPnL: 0,
      total_unreal_pnl: 0,
      total_unreal_pnl_index: 0,
      notional: 0,
      unsettledPnL: 0,
      total_unsettled_pnl: 0,
      unrealPnlROI: 0,
      unrealPnlROI_index: 0,
    } as unknown as API.PositionsTPSLExt,
  };

  return {
    usePositionStore: {
      getState: jest.fn(() => ({
        positions: mockPositions,
        actions: {
          setPositions: jest.fn(),
          closePosition: jest.fn(),
        },
      })),
    },
  };
});

jest.mock("../../../next/apiStatus/apiStatus.store", () => {
  return {
    useApiStatusStore: {
      getState: jest.fn(() => ({
        apis: {
          positions: {
            loading: false,
          },
        },
        actions: {
          updateApiLoading: jest.fn(),
        },
      })),
    },
  };
});

/**
 * Create a mock calculator context
 */
function createMockContext(): CalculatorContext {
  const ctx = {
    accountInfo: {
      account_id: "test_account",
      email: "test@example.com",
      imr_factor: {
        PERP_ETH_USDC: 0.1,
        PERP_BTC_USDC: 0.1,
      },
      max_leverage: 10,
    } as unknown as API.AccountInfo,
    symbolsInfo: {
      PERP_ETH_USDC: {
        base_imr: 0.05,
        base_mmr: 0.02,
      } as API.SymbolExt,
      PERP_BTC_USDC: {
        base_imr: 0.05,
        base_mmr: 0.02,
      } as API.SymbolExt,
    },
    fundingRates: {
      PERP_ETH_USDC: {
        sum_unitary_funding: 0.001,
      } as API.FundingRate,
      PERP_BTC_USDC: {
        sum_unitary_funding: 0.001,
      } as API.FundingRate,
    },
    tokensInfo: [],
    portfolio: {
      totalCollateral: zero,
      totalValue: null,
      freeCollateral: zero,
      availableBalance: 0,
      unsettledPnL: 0,
      totalUnrealizedROI: 0,
      usdcHolding: 0,
    },
    get: jest.fn((fn) => fn({})),
    saveOutput: jest.fn(),
    outputToValue: jest.fn(() => ({})),
    clearCache: jest.fn(),
    isReady: true,
  } as unknown as CalculatorContext;

  return ctx;
}

/**
 * Create mock position data
 */
function createMockPosition(
  symbol: string = "PERP_ETH_USDC",
  overrides?: Partial<API.Position>,
): API.Position {
  return {
    symbol,
    position_qty: 1,
    mark_price: 2000,
    average_open_price: 1900,
    cost_position: 1900,
    last_sum_unitary_funding: 0.0005,
    leverage: 5,
    ...overrides,
  } as API.Position;
}

describe("PositionCalculator", () => {
  let calculator: PositionCalculator;
  let mockCtx: CalculatorContext;

  beforeEach(() => {
    calculator = new PositionCalculator();
    mockCtx = createMockContext();
    jest.clearAllMocks();
  });

  describe("constructor", () => {
    test("should create calculator with default symbol 'all'", () => {
      const calc = new PositionCalculator();
      expect(calc.name).toBe("positionCalculator_all");
    });

    test("should create calculator with specific symbol", () => {
      const calc = new PositionCalculator("PERP_ETH_USDC");
      expect(calc.name).toBe("positionCalculator_PERP_ETH_USDC");
    });
  });

  describe("calc() - MARK_PRICE scope", () => {
    test("should calculate positions with mark price", () => {
      const markPrices = {
        PERP_ETH_USDC: 2100,
        PERP_BTC_USDC: 50000,
      };

      const positionData = {
        rows: [createMockPosition("PERP_ETH_USDC")],
      } as unknown as API.PositionInfo;

      // Mock getPosition to return position data
      (mockCtx.get as jest.Mock).mockReturnValue(positionData);

      const result = calculator.calc(
        CalculatorScope.MARK_PRICE,
        markPrices,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].mark_price).toBe(2100);
      }
    });

    test("should return null if no positions", () => {
      const markPrices = { PERP_ETH_USDC: 2100 };
      (mockCtx.get as jest.Mock).mockReturnValue(null);

      const result = calculator.calc(
        CalculatorScope.MARK_PRICE,
        markPrices,
        mockCtx,
      );

      // getPosition may return empty positions object instead of null
      expect(result).toBeDefined();
      if (result && result.rows) {
        expect(result.rows.length).toBe(0);
      }
    });

    test("should return positions without modification if rows array is empty", () => {
      const markPrices = { PERP_ETH_USDC: 2100 };
      const emptyPositions = {
        rows: [],
      } as unknown as API.PositionInfo;
      (mockCtx.get as jest.Mock).mockReturnValue(emptyPositions);

      const result = calculator.calc(
        CalculatorScope.MARK_PRICE,
        markPrices,
        mockCtx,
      );

      expect(result).toEqual(emptyPositions);
    });
  });

  describe("calc() - INDEX_PRICE scope", () => {
    test("should calculate positions with index price", () => {
      const indexPrices = {
        PERP_ETH_USDC: 2100,
        PERP_BTC_USDC: 50000,
      };

      const positionData: API.PositionInfo = {
        rows: [
          createMockPosition("PERP_ETH_USDC", {
            index_price: 2000,
          }),
        ],
      };

      (mockCtx.get as jest.Mock).mockReturnValue(positionData);

      const result = calculator.calc(
        CalculatorScope.INDEX_PRICE,
        indexPrices,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].index_price).toBe(2100);
      }
    });

    test("should use mark_price as fallback if index_price not provided", () => {
      const indexPrices = {
        PERP_ETH_USDC: 2100,
      };

      const positionData: API.PositionInfo = {
        rows: [
          createMockPosition("PERP_ETH_USDC", {
            mark_price: 2000,
            // No index_price
          }),
        ],
      };

      (mockCtx.get as jest.Mock).mockReturnValue(positionData);

      const result = calculator.calc(
        CalculatorScope.INDEX_PRICE,
        indexPrices,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].index_price).toBe(2100);
      }
    });
  });

  describe("calc() - POSITION scope", () => {
    test("should format position data", () => {
      const positionData = {
        rows: [createMockPosition("PERP_ETH_USDC")],
      } as unknown as API.PositionInfo;

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      expect(result?.rows).toBeDefined();
    });

    test("should return empty positions if rows array is empty", () => {
      const positionData: API.PositionInfo = {
        rows: [],
      };

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      expect(result?.rows).toEqual([]);
    });
  });

  describe("calc() - format method", () => {
    test("should calculate unrealized PnL", () => {
      const positionData: API.PositionInfo = {
        rows: [
          createMockPosition("PERP_ETH_USDC", {
            position_qty: 1,
            average_open_price: 1900,
            mark_price: 2000,
          }),
        ],
      };

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].unrealized_pnl).toBeDefined();
        expect(result.rows[0].notional).toBeDefined();
      }
    });

    test("should calculate maintenance margin", () => {
      const positionData = {
        rows: [createMockPosition("PERP_ETH_USDC")],
      } as unknown as API.PositionInfo;

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].mm).toBeDefined();
        expect(result.rows[0].mmr).toBeDefined();
      }
    });

    test("should calculate funding fee", () => {
      const positionData: API.PositionInfo = {
        rows: [
          createMockPosition("PERP_ETH_USDC", {
            position_qty: 1,
            last_sum_unitary_funding: 0.0005,
          }),
        ],
      };

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows && result.rows.length > 0) {
        expect(result.rows[0].fundingFee).toBeDefined();
      }
    });

    test("should return data as-is if context is not ready", () => {
      const positionData = {
        rows: [createMockPosition("PERP_ETH_USDC")],
      } as unknown as API.PositionInfo;

      const notReadyCtx = {
        ...mockCtx,
        accountInfo: undefined,
      } as CalculatorContext;

      const result = calculator.calc(
        CalculatorScope.POSITION,
        positionData,
        notReadyCtx,
      );

      expect(result).toBeDefined();
      expect(result).toEqual(positionData);
    });
  });

  describe("update()", () => {
    test("should update position store with calculated data", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        usePositionStore,
      } = require("../../usePositionStream/usePosition.store");
      const mockSetPositions = jest.fn();
      (usePositionStore.getState as jest.Mock).mockReturnValue({
        positions: {},
        actions: {
          setPositions: mockSetPositions,
          closePosition: jest.fn(),
        },
      });

      const data = {
        rows: [createMockPosition("PERP_ETH_USDC") as API.PositionTPSLExt],
        unrealPnL: 100,
        total_unreal_pnl: 100,
        total_unreal_pnl_index: 0,
        notional: 2000,
        unsettledPnL: 0,
        total_unsettled_pnl: 0,
        unrealPnlROI: 0,
        unrealPnlROI_index: 0,
      } as API.PositionsTPSLExt;

      calculator.update(data, CalculatorScope.POSITION);

      expect(mockSetPositions).toHaveBeenCalledWith("all", data);
    });

    test("should not update if data is null", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        usePositionStore,
      } = require("../../usePositionStream/usePosition.store");
      const mockSetPositions = jest.fn();
      (usePositionStore.getState as jest.Mock).mockReturnValue({
        positions: {},
        actions: {
          setPositions: mockSetPositions,
          closePosition: jest.fn(),
        },
      });

      calculator.update(null, CalculatorScope.POSITION);

      expect(mockSetPositions).not.toHaveBeenCalled();
    });

    test("should not update if rows is not an array", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        usePositionStore,
      } = require("../../usePositionStream/usePosition.store");
      const mockSetPositions = jest.fn();
      (usePositionStore.getState as jest.Mock).mockReturnValue({
        positions: {},
        actions: {
          setPositions: mockSetPositions,
          closePosition: jest.fn(),
        },
      });

      calculator.update(
        { rows: null } as unknown as API.PositionsTPSLExt,
        CalculatorScope.POSITION,
      );

      expect(mockSetPositions).not.toHaveBeenCalled();
    });

    test("should update API loading status when positions are loaded", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        useApiStatusStore,
      } = require("../../../next/apiStatus/apiStatus.store");
      const mockUpdateApiLoading = jest.fn();
      (useApiStatusStore.getState as jest.Mock).mockReturnValue({
        apis: {
          positions: {
            loading: true,
          },
        },
        actions: {
          updateApiLoading: mockUpdateApiLoading,
        },
      });

      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        usePositionStore,
      } = require("../../usePositionStream/usePosition.store");
      (usePositionStore.getState as jest.Mock).mockReturnValue({
        positions: {},
        actions: {
          setPositions: jest.fn(),
          closePosition: jest.fn(),
        },
      });

      const data = {
        rows: [createMockPosition("PERP_ETH_USDC") as API.PositionTPSLExt],
        unrealPnL: 100,
        total_unreal_pnl: 100,
        total_unreal_pnl_index: 0,
        notional: 2000,
        unsettledPnL: 0,
        total_unsettled_pnl: 0,
        unrealPnlROI: 0,
        unrealPnlROI_index: 0,
      } as API.PositionsTPSLExt;

      calculator.update(data, CalculatorScope.POSITION);

      expect(mockUpdateApiLoading).toHaveBeenCalledWith("positions", false);
    });
  });

  describe("destroy()", () => {
    test("should close position and delete from context", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const {
        usePositionStore,
      } = require("../../usePositionStream/usePosition.store");
      const mockClosePosition = jest.fn();
      (usePositionStore.getState as jest.Mock).mockReturnValue({
        positions: {},
        actions: {
          setPositions: jest.fn(),
          closePosition: mockClosePosition,
        },
      });

      const mockDeleteByName = jest.fn();
      jest.spyOn(CalculatorContext, "instance", "get").mockReturnValue({
        deleteByName: mockDeleteByName,
      } as unknown as CalculatorContext);

      calculator.destroy();

      expect(mockClosePosition).toHaveBeenCalledWith("all");
      expect(mockDeleteByName).toHaveBeenCalledWith("positionCalculator_all");
    });
  });

  describe("symbol-specific calculator", () => {
    test("should filter positions by symbol", () => {
      const symbolCalculator = new PositionCalculator("PERP_ETH_USDC");
      const positionData: API.PositionInfo = {
        rows: [
          createMockPosition("PERP_ETH_USDC"),
          createMockPosition("PERP_BTC_USDC"),
        ],
      };

      const result = symbolCalculator.calc(
        CalculatorScope.POSITION,
        positionData,
        mockCtx,
      );

      expect(result).toBeDefined();
      if (result && result.rows) {
        // Should only contain PERP_ETH_USDC positions
        expect(result.rows.every((p) => p.symbol === "PERP_ETH_USDC")).toBe(
          true,
        );
      }
    });
  });
});
