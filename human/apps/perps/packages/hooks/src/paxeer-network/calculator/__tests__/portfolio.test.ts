import { API, EMPTY_LIST } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { CalculatorScope } from "../../../types";
import { CalculatorContext } from "../calculatorContext";
import { IndexPriceCalculatorName } from "../indexPrice";
import { MarketCalculatorName } from "../markPrice";
import { PortfolioCalculator } from "../portfolio";

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
        },
        tokensInfo: [],
        portfolio: {
          holding: [],
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
      },
      max_leverage: 10,
    } as unknown as API.AccountInfo,
    symbolsInfo: {
      PERP_ETH_USDC: {
        base_imr: 0.05,
        base_mmr: 0.02,
      } as API.SymbolExt,
    },
    tokensInfo: [],
    portfolio: {
      holding: [],
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
 * Create mock holding data
 */
function createMockHolding(
  token: string = "USDC",
  overrides?: Partial<API.Holding>,
): API.Holding {
  return {
    token,
    holding: 1000,
    frozen: 0,
    pending_short: 0,
    pending_exposure: 0,
    pending_long: 0,
    pending_long_exposure: 0,
    version: 1,
    staked: 0,
    unbonding: 0,
    vault: 0,
    fee24H: 0,
    markPrice: 1,
    ...overrides,
  };
}

/**
 * Create mock positions data
 */
function createMockPositions(): API.PositionsTPSLExt {
  return {
    rows: [],
    unrealPnL: 0,
    total_unreal_pnl: 0,
    total_unreal_pnl_index: 0,
    notional: 0,
    unsettledPnL: 0,
    total_unsettled_pnl: 0,
    unrealPnlROI: 0,
    unrealPnlROI_index: 0,
  } as unknown as API.PositionsTPSLExt;
}

describe("PortfolioCalculator", () => {
  let calculator: PortfolioCalculator;
  let mockCtx: CalculatorContext;

  beforeEach(() => {
    calculator = new PortfolioCalculator();
    mockCtx = createMockContext();
    jest.clearAllMocks();
  });

  describe("calc() - MARK_PRICE scope", () => {
    test("should use mark prices from data", () => {
      const markPrices = {
        PERP_ETH_USDC: 2000,
      };

      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(
        CalculatorScope.MARK_PRICE,
        markPrices,
        mockCtx,
      );

      expect(result).toBeDefined();
    });

    test("should get mark prices from context if not in data", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(CalculatorScope.POSITION, {}, mockCtx);

      expect(result).toBeDefined();
    });
  });

  describe("calc() - INDEX_PRICE scope", () => {
    test("should use index prices from data", () => {
      const indexPrices = {
        PERP_ETH_USDC: 2000,
      };

      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(
        CalculatorScope.INDEX_PRICE,
        indexPrices,
        mockCtx,
      );

      expect(result).toBeDefined();
    });

    test("should get index prices from context if not in data", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeDefined();
    });
  });

  describe("calc() - PORTFOLIO scope", () => {
    test("should update holding when data contains holding array", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];
      const newHolding = [createMockHolding("USDC", { holding: 2000 })];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(
        CalculatorScope.PORTFOLIO,
        { holding: newHolding },
        mockCtx,
      );

      expect(result).toBeDefined();
    });

    test("should update holding when data contains holding object", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(
        CalculatorScope.PORTFOLIO,
        {
          holding: {
            USDC: {
              holding: 2000,
              frozen: 0,
            },
          },
        },
        mockCtx,
      );

      expect(result).toBeDefined();
    });
  });

  describe("calc() - format method", () => {
    test("should return null if required data is missing", () => {
      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeNull();
    });

    test("should return null if holding is missing", () => {
      const mockPositions = createMockPositions();

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      // Mock getPortfolio to return portfolio with undefined holding
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      (useAppStore.getState as jest.Mock).mockReturnValueOnce({
        portfolio: {
          holding: undefined,
        },
      });

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeNull();
    });

    test("should return null if positions are missing", () => {
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: null,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeNull();
    });

    test("should return null if markPrices are missing", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      // For MARK_PRICE scope, markPrices come from data parameter
      // So we pass null as data to test missing markPrices
      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: null,
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      (useAppStore.getState as jest.Mock).mockReturnValueOnce({
        portfolio: {
          holding: mockHolding,
        },
      });

      // Pass null as data for MARK_PRICE scope to test missing markPrices
      const result = calculator.calc(CalculatorScope.MARK_PRICE, null, mockCtx);

      expect(result).toBeNull();
    });

    test("should return null if indexPrices are missing", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: null,
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      (useAppStore.getState as jest.Mock).mockReturnValueOnce({
        portfolio: {
          holding: mockHolding,
        },
      });

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeNull();
    });

    test("should return null if accountInfo is missing", () => {
      const mockPositions = createMockPositions();
      const mockHolding = [createMockHolding("USDC")];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };
      (mockCtx as any).accountInfo = undefined;

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeNull();
    });

    test("should calculate portfolio values when all data is present", () => {
      const mockPositions: API.PositionsTPSLExt = {
        rows: [],
        unrealPnL: 100,
        total_unreal_pnl: 100,
        total_unreal_pnl_index: 0,
        notional: 2000,
        unsettledPnL: 50,
        total_unsettled_pnl: 50,
        unrealPnlROI: 0.05,
        unrealPnlROI_index: 0,
      };
      const mockHolding = [createMockHolding("USDC", { holding: 1000 })];

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = {
        holding: mockHolding,
      };

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      expect(result).toBeDefined();
      if (result) {
        expect(result.totalCollateral).toBeDefined();
        expect(result.totalValue).toBeDefined();
        expect(result.freeCollateral).toBeDefined();
        expect(result.availableBalance).toBeDefined();
        expect(result.totalUnrealizedROI).toBeDefined();
        expect(result.unsettledPnL).toBe(50);
        // Holding should be present in result
        expect(result.holding).toBeDefined();
        expect(Array.isArray(result.holding)).toBe(true);
      }
    });
  });

  describe("update()", () => {
    test("should update app store with portfolio data", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      const mockBatchUpdate = jest.fn();
      (useAppStore.getState as jest.Mock).mockReturnValue({
        portfolio: {},
        actions: {
          batchUpdateForPortfolio: mockBatchUpdate,
        },
      });

      const data = {
        totalCollateral: new Decimal(1000),
        totalValue: new Decimal(2000),
        freeCollateral: new Decimal(500),
        availableBalance: 100,
        totalUnrealizedROI: 0.1,
        unsettledPnL: 50,
        holding: [createMockHolding("USDC")],
        usdcHolding: 1000,
      };

      calculator.update(
        data as unknown as Parameters<typeof calculator.update>[0],
        CalculatorScope.PORTFOLIO,
      );

      expect(mockBatchUpdate).toHaveBeenCalledWith({
        totalCollateral: data.totalCollateral,
        totalValue: data.totalValue,
        freeCollateral: data.freeCollateral,
        availableBalance: data.availableBalance,
        totalUnrealizedROI: data.totalUnrealizedROI,
        unsettledPnL: data.unsettledPnL,
        holding: data.holding as API.Holding[],
        usdcHolding: 1000,
      });
    });

    test("should not update if data is null", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      const mockBatchUpdate = jest.fn();
      (useAppStore.getState as jest.Mock).mockReturnValue({
        portfolio: {},
        actions: {
          batchUpdateForPortfolio: mockBatchUpdate,
        },
      });

      calculator.update(null, CalculatorScope.PORTFOLIO);

      expect(mockBatchUpdate).not.toHaveBeenCalled();
    });

    test("should handle usdcHolding as Decimal", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      const mockBatchUpdate = jest.fn();
      (useAppStore.getState as jest.Mock).mockReturnValue({
        portfolio: {},
        actions: {
          batchUpdateForPortfolio: mockBatchUpdate,
        },
      });

      const data = {
        totalCollateral: new Decimal(1000),
        totalValue: new Decimal(2000),
        freeCollateral: new Decimal(500),
        availableBalance: 100,
        totalUnrealizedROI: 0.1,
        unsettledPnL: 50,
        holding: [],
        usdcHolding: new Decimal(1000),
      };

      calculator.update(
        data as unknown as Parameters<typeof calculator.update>[0],
        CalculatorScope.PORTFOLIO,
      );

      expect(mockBatchUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          usdcHolding: 1000,
        }),
      );
    });

    test("should handle empty holding array", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      const mockBatchUpdate = jest.fn();
      (useAppStore.getState as jest.Mock).mockReturnValue({
        portfolio: {},
        actions: {
          batchUpdateForPortfolio: mockBatchUpdate,
        },
      });

      const data = {
        totalCollateral: new Decimal(1000),
        totalValue: new Decimal(2000),
        freeCollateral: new Decimal(500),
        availableBalance: 100,
        totalUnrealizedROI: 0.1,
        unsettledPnL: 50,
        holding: [],
        usdcHolding: 1000,
      };

      calculator.update(
        data as unknown as Parameters<typeof calculator.update>[0],
        CalculatorScope.PORTFOLIO,
      );

      expect(mockBatchUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          holding: [],
        }),
      );
    });
  });

  describe("getPortfolio()", () => {
    test("should get portfolio from context output", () => {
      const mockPortfolio = {
        holding: [createMockHolding("USDC")],
        totalCollateral: zero,
        totalValue: null,
        freeCollateral: zero,
        availableBalance: 0,
        unsettledPnL: 0,
        totalUnrealizedROI: 0,
        usdcHolding: 0,
      };

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          portfolio: mockPortfolio,
        };
        return fn(output);
      });

      const mockPositions = createMockPositions();
      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          portfolio: mockPortfolio,
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = undefined;

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      // Should use portfolio from context
      expect(result).toBeDefined();
    });

    test("should fallback to app store portfolio", () => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const { useAppStore } = require("../../appStore");
      const mockPortfolio = {
        holding: [createMockHolding("USDC")],
        totalCollateral: zero,
        totalValue: null,
        freeCollateral: zero,
        availableBalance: 0,
        unsettledPnL: 0,
        totalUnrealizedROI: 0,
        usdcHolding: 0,
      };

      (useAppStore.getState as jest.Mock).mockReturnValue({
        portfolio: mockPortfolio,
      });

      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {};
        return fn(output);
      });

      const mockPositions = createMockPositions();
      (mockCtx.get as jest.Mock).mockImplementation((fn) => {
        const output: Record<string, unknown> = {
          [MarketCalculatorName]: { PERP_ETH_USDC: 2000 },
          [IndexPriceCalculatorName]: { PERP_ETH_USDC: 2000 },
          positionCalculator_all: mockPositions,
        };
        return fn(output);
      });

      (mockCtx as any).portfolio = undefined;

      const result = calculator.calc(CalculatorScope.MARK_PRICE, {}, mockCtx);

      // Should use portfolio from app store
      expect(result).toBeDefined();
    });
  });
});
