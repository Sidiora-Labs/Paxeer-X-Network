import {
  Calculator,
  CalculatorScheduler,
  CalculatorScope,
} from "../../../types";
import { CalculatorContext } from "../calculatorContext";
import { CalculatorService } from "../calculatorService";

/**
 * Mock useAppStore to provide test data
 */
jest.mock("../../appStore", () => {
  const mockAccountInfo = {
    account_id: "test_account",
    email: "test@example.com",
  };

  return {
    useAppStore: {
      getState: jest.fn(() => ({
        accountInfo: mockAccountInfo,
        symbolsInfo: {},
        fundingRates: {},
        tokensInfo: [],
        portfolio: {
          totalCollateral: { toNumber: () => 0 },
          totalValue: null,
          freeCollateral: { toNumber: () => 0 },
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
 * Create a mock calculator for testing
 */
function createMockCalculator(
  name: string,
  calcResult: unknown = { result: `result_${name}` },
): jest.Mocked<Calculator> {
  return {
    name,
    calc: jest.fn((scope, data, ctx) => {
      // Simulate saving output to context only if result is not null
      if (calcResult !== null) {
        ctx.saveOutput(name, calcResult);
      }
      return calcResult;
    }),
    cache: jest.fn(),
    update: jest.fn(),
  };
}

/**
 * Create a mock scheduler for testing
 */
function createMockScheduler(): jest.Mocked<CalculatorScheduler> {
  return {
    calc: jest.fn(async (scope, calculators, data, ctx): Promise<unknown[]> => {
      // Simulate scheduler calling each calculator's calc method
      const results: unknown[] = [];
      for (const calculator of calculators) {
        const result = calculator.calc(scope, data, ctx);
        if (result) {
          results.push(result);
        }
      }
      return results;
    }),
    update: jest.fn(
      (scope, calculators, data: Record<string, unknown>): void => {
        // Simulate scheduler calling each calculator's update method
        for (const calculator of calculators) {
          const item = data[calculator.name];
          if (item) {
            calculator.update(item, scope);
          }
        }
      },
    ),
  };
}

describe("CalculatorService", () => {
  let mockScheduler: jest.Mocked<CalculatorScheduler>;
  let mockCalculator1: jest.Mocked<Calculator>;
  let mockCalculator2: jest.Mocked<Calculator>;
  let calculatorService: CalculatorService;

  beforeEach(() => {
    // Reset CalculatorContext singleton instance
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (CalculatorContext as any)._instance = undefined;

    // Create fresh mocks for each test
    mockScheduler = createMockScheduler();
    mockCalculator1 = createMockCalculator("calculator1", { value: 100 });
    mockCalculator2 = createMockCalculator("calculator2", { value: 200 });

    // Create calculator service with test calculators
    calculatorService = new CalculatorService(mockScheduler, [
      [CalculatorScope.MARK_PRICE, [mockCalculator1, mockCalculator2]],
      [CalculatorScope.POSITION, [mockCalculator1]],
    ]);

    // Clear all mocks
    jest.clearAllMocks();
  });

  afterEach(() => {
    // Clean up CalculatorContext singleton
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (CalculatorContext as any)._instance = undefined;
  });

  describe("calc() - Queue Processing", () => {
    test("should add item to queue and process it", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Verify scheduler.calc was called
      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);
      expect(mockScheduler.calc).toHaveBeenCalledWith(
        CalculatorScope.MARK_PRICE,
        [mockCalculator1, mockCalculator2],
        testData,
        expect.any(CalculatorContext),
      );
    });

    test("should process queue items in order", async () => {
      const callOrder: CalculatorScope[] = [];

      // Track call order
      mockScheduler.calc.mockImplementation(async (scope) => {
        callOrder.push(scope);
        return [];
      });

      // Add multiple items to queue
      await calculatorService.calc(CalculatorScope.MARK_PRICE, { data: 1 });
      await calculatorService.calc(CalculatorScope.POSITION, { data: 2 });
      await calculatorService.calc(CalculatorScope.MARK_PRICE, { data: 3 });

      // Verify items were processed in order
      expect(callOrder).toEqual([
        CalculatorScope.MARK_PRICE,
        CalculatorScope.POSITION,
        CalculatorScope.MARK_PRICE,
      ]);
    });

    test("should handle empty queue gracefully", async () => {
      // Call calc with no items in queue
      await calculatorService.calc(CalculatorScope.MARK_PRICE, {});

      // Should not throw and should process the item
      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);
    });
  });

  describe("Calculation Result Verification", () => {
    test("should call calc method on each calculator", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Verify each calculator's calc method was called
      expect(mockCalculator1.calc).toHaveBeenCalledTimes(1);
      expect(mockCalculator1.calc).toHaveBeenCalledWith(
        CalculatorScope.MARK_PRICE,
        testData,
        expect.any(Object),
      );

      expect(mockCalculator2.calc).toHaveBeenCalledTimes(1);
      expect(mockCalculator2.calc).toHaveBeenCalledWith(
        CalculatorScope.MARK_PRICE,
        testData,
        expect.any(Object),
      );
    });

    test("should save calculator results to context output", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Get the context that was used
      const calcCall = mockScheduler.calc.mock.calls[0];
      const ctx = calcCall[3] as CalculatorContext;

      // Verify results were saved to context
      const output = ctx.outputToValue();
      expect(output.calculator1).toEqual({ value: 100 });
      expect(output.calculator2).toEqual({ value: 200 });
    });

    test("should handle different CalculatorScope correctly", async () => {
      await calculatorService.calc(CalculatorScope.MARK_PRICE, {});
      await calculatorService.calc(CalculatorScope.POSITION, {});

      // MARK_PRICE scope should use both calculators
      expect(mockScheduler.calc).toHaveBeenNthCalledWith(
        1,
        CalculatorScope.MARK_PRICE,
        [mockCalculator1, mockCalculator2],
        {},
        expect.any(CalculatorContext),
      );

      // POSITION scope should use only calculator1
      expect(mockScheduler.calc).toHaveBeenNthCalledWith(
        2,
        CalculatorScope.POSITION,
        [mockCalculator1],
        {},
        expect.any(CalculatorContext),
      );
    });

    test("should handle calculators returning null", async () => {
      const nullCalculator = createMockCalculator("nullCalculator", null);
      const service = new CalculatorService(mockScheduler, [
        [CalculatorScope.MARK_PRICE, [nullCalculator]],
      ]);

      await service.calc(CalculatorScope.MARK_PRICE, {});

      // Should still call calc but result is null
      expect(nullCalculator.calc).toHaveBeenCalledTimes(1);

      const calcCall = mockScheduler.calc.mock.calls[0];
      const ctx = calcCall[3] as CalculatorContext;
      const output = ctx.outputToValue();

      // Null result should not be saved
      expect(output.nullCalculator).toBeUndefined();
    });
  });

  describe("Method Call Verification", () => {
    test("should call scheduler.calc with correct parameters", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);
      expect(mockScheduler.calc).toHaveBeenCalledWith(
        CalculatorScope.MARK_PRICE,
        [mockCalculator1, mockCalculator2],
        testData,
        expect.any(CalculatorContext),
      );
    });

    test("should call scheduler.update after calc when skipUpdate is false", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Verify update was called
      expect(mockScheduler.update).toHaveBeenCalledTimes(1);

      // Get the context to check output
      const calcCall = mockScheduler.calc.mock.calls[0];
      const ctx = calcCall[3] as CalculatorContext;
      const output = ctx.outputToValue();

      // Verify update was called with correct parameters
      expect(mockScheduler.update).toHaveBeenCalledWith(
        CalculatorScope.MARK_PRICE,
        [mockCalculator1, mockCalculator2],
        output,
      );
    });

    test("should call calculator.update method through scheduler", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Verify update was called on scheduler
      expect(mockScheduler.update).toHaveBeenCalledTimes(1);

      // Get the output that was passed to update
      const updateCall = mockScheduler.update.mock.calls[0];
      const output = updateCall[2];

      // Verify calculators' update methods were called
      expect(mockCalculator1.update).toHaveBeenCalledTimes(1);
      expect(mockCalculator1.update).toHaveBeenCalledWith(
        output.calculator1,
        CalculatorScope.MARK_PRICE,
      );

      expect(mockCalculator2.update).toHaveBeenCalledTimes(1);
      expect(mockCalculator2.update).toHaveBeenCalledWith(
        output.calculator2,
        CalculatorScope.MARK_PRICE,
      );
    });

    test("should call ctx.saveOutput for each calculator result", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData);

      // Get the context that was used
      const calcCall = mockScheduler.calc.mock.calls[0];
      const ctx = calcCall[3] as CalculatorContext;

      // Verify saveOutput was called (through calculator.calc implementation)
      const output = ctx.outputToValue();
      expect(output.calculator1).toBeDefined();
      expect(output.calculator2).toBeDefined();
    });
  });

  describe("Option Handling", () => {
    test("should skip update when skipUpdate is true", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData, {
        skipUpdate: true,
      });

      // Verify calc was called
      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);

      // Verify update was NOT called
      expect(mockScheduler.update).not.toHaveBeenCalled();
    });

    test("should skip calculation when skipPending is true and ctx is not ready", async () => {
      // Create a mock context that is not ready
      const mockNotReadyContext = {
        isReady: false,
        saveOutput: jest.fn(),
        outputToValue: jest.fn(() => ({})),
        get: jest.fn(),
        clearCache: jest.fn(),
      } as unknown as CalculatorContext;

      // Mock CalculatorContext.create to return not ready context
      jest
        .spyOn(CalculatorContext, "create")
        .mockReturnValue(mockNotReadyContext);

      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData, {
        skipPending: true,
      });

      // Verify calc was NOT called because context is not ready
      expect(mockScheduler.calc).not.toHaveBeenCalled();

      // Restore original create method
      jest.restoreAllMocks();
    });

    test("should process calculation when skipPending is true but ctx is ready", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData, {
        skipPending: true,
      });

      // Verify calc was called because context is ready
      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);
    });

    test("should skip calculation when skipWhenOnPause is true and window is not visible", async () => {
      // Mock document.visibilityState
      Object.defineProperty(document, "visibilityState", {
        writable: true,
        value: "hidden",
      });

      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData, {
        skipWhenOnPause: true,
      });

      // Verify calc was NOT called because window is not visible
      expect(mockScheduler.calc).not.toHaveBeenCalled();

      // Reset visibilityState
      Object.defineProperty(document, "visibilityState", {
        writable: true,
        value: "visible",
      });
    });

    test("should process calculation when skipWhenOnPause is true but window is visible", async () => {
      // Mock document.visibilityState
      Object.defineProperty(document, "visibilityState", {
        writable: true,
        value: "visible",
      });

      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.MARK_PRICE, testData, {
        skipWhenOnPause: true,
      });

      // Verify calc was called because window is visible
      expect(mockScheduler.calc).toHaveBeenCalledTimes(1);
    });

    test("should handle scope without registered calculators", async () => {
      const testData = { price: 1000 };
      await calculatorService.calc(CalculatorScope.ORDER, testData);

      // Verify scheduler was not called (no calculators for ORDER scope)
      expect(mockScheduler.calc).not.toHaveBeenCalled();
      expect(mockScheduler.update).not.toHaveBeenCalled();
    });
  });

  describe("stop()", () => {
    test("should clear calc queue", async () => {
      // Add items to queue
      calculatorService.calc(CalculatorScope.MARK_PRICE, {});
      calculatorService.calc(CalculatorScope.POSITION, {});

      // Stop the service
      calculatorService.stop();

      // Verify queue is cleared (no more processing)
      // Note: This is hard to test directly since queue is private
      // We can verify by checking that stop doesn't throw
      expect(() => calculatorService.stop()).not.toThrow();
    });

    test("should clear context cache", async () => {
      await calculatorService.calc(CalculatorScope.MARK_PRICE, {});

      // Get the context
      const calcCall = mockScheduler.calc.mock.calls[0];
      const ctx = calcCall[3] as CalculatorContext;

      // Save some data to context
      ctx.saveOutput("test", { value: 123 });
      expect(ctx.outputToValue().test).toEqual({ value: 123 });

      // Stop should clear cache
      calculatorService.stop();

      // Verify cache is cleared
      expect(ctx.outputToValue()).toEqual({});
    });
  });

  describe("register and unregister", () => {
    test("should register a new calculator", async () => {
      const newCalculator = createMockCalculator("newCalculator");
      calculatorService.register(CalculatorScope.ORDER, newCalculator);

      // Verify calculator was added
      await calculatorService.calc(CalculatorScope.ORDER, {});
      expect(mockScheduler.calc).toHaveBeenCalledWith(
        CalculatorScope.ORDER,
        [newCalculator],
        {},
        expect.any(CalculatorContext),
      );
    });

    test("should increment reference count when registering same calculator twice", async () => {
      const calculator = createMockCalculator("testCalculator");
      calculatorService.register(CalculatorScope.ORDER, calculator);
      calculatorService.register(CalculatorScope.ORDER, calculator);

      // Both registrations should succeed
      await calculatorService.calc(CalculatorScope.ORDER, {});
      expect(mockScheduler.calc).toHaveBeenCalled();
    });

    test("should unregister a calculator", async () => {
      const calculator = createMockCalculator("testCalculator");
      calculatorService.register(CalculatorScope.ORDER, calculator);

      // Unregister
      calculatorService.unregister(CalculatorScope.ORDER, calculator);

      // Verify calculator is removed
      await calculatorService.calc(CalculatorScope.ORDER, {});
      expect(mockScheduler.calc).not.toHaveBeenCalled();
    });

    test("should call destroy when unregistering calculator with destroy method", () => {
      const destroyFn = jest.fn();
      const calculator = {
        ...createMockCalculator("testCalculator"),
        destroy: destroyFn,
      };
      calculatorService.register(CalculatorScope.ORDER, calculator);

      calculatorService.unregister(CalculatorScope.ORDER, calculator);

      expect(destroyFn).toHaveBeenCalledTimes(1);
    });
  });
});
