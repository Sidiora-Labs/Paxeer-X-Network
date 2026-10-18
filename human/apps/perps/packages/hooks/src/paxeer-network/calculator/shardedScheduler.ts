import {
  Calculator,
  CalculatorCtx,
  CalculatorScheduler,
  CalculatorScope,
} from "../../types";

type ShardProcessor<T, R> = (shard: T[]) => R;

interface IdleDeadline {
  timeRemaining: () => number;
  readonly didTimeout: boolean;
}

/**
 * Polyfill for requestIdleCallback
 */
const requestIdleCallbackPolyfill = (
  callback: (deadline: IdleDeadline) => void,
  options?: { timeout: number },
): ReturnType<typeof setTimeout> => {
  const startTime = Date.now();

  return setTimeout(() => {
    callback({
      didTimeout: false,
      timeRemaining: () => Math.max(0, 50 - (Date.now() - startTime)),
    });
  }, 1); // Use 1ms timeout as a fallback
};

/**
 * Polyfill for cancelIdleCallback
 */
const cancelIdleCallbackPolyfill = (id: number) => {
  clearTimeout(id);
};

/**
 * Export the native requestIdleCallback or polyfill
 */
const safeRequestIdleCallback =
  typeof window !== "undefined" && window.requestIdleCallback
    ? window.requestIdleCallback.bind(window)
    : requestIdleCallbackPolyfill;

/**
 * Export the native cancelIdleCallback or polyfill
 */
const safeCancelIdleCallback =
  typeof window !== "undefined" && window.cancelIdleCallback
    ? window.cancelIdleCallback.bind(window)
    : cancelIdleCallbackPolyfill;

class ShardingScheduler implements CalculatorScheduler {
  // run(calculators: Calculator[]) {}
  calc(
    scope: CalculatorScope,
    calculators: Calculator[],
    data: any,
    ctx: CalculatorCtx,
  ): Promise<any> {
    return new Promise((resolve, reject) => {
      try {
        this.computation<Calculator, any>(
          calculators,
          (shard) => {
            const results = [];
            for (let index = 0; index < shard.length; index++) {
              const calculator = shard[index];
              const result = calculator.calc(scope, data, ctx);

              if (result) {
                ctx.saveOutput(calculator.name, result);
                results.push(result);
              }
            }

            return results;
          },
          (results) => {
            resolve(results);
          },
        );
      } catch (error) {
        console.error("ShardingScheduler calc error", error);
        reject(error);
      }
    });
    // for (let index = 0; index < calculators.length; index++) {
    //   const calculator = calculators[index];
    //   const result = calculator.calc(scope, data, ctx);
    //   // console.log("item calc ======>>>>>>", scope, calculator.name, result);
    //   if (result) {
    //     ctx.saveOutput(calculator.name, result);
    //   }
    // }
    // return Promise.resolve();
  }

  update(scope: CalculatorScope, calculators: Calculator[], data: any) {
    for (let index = 0; index < calculators.length; index++) {
      const calculator = calculators[index];
      const item = data[calculator.name];

      if (!!item) {
        calculator.update(item, scope);
      }
    }
    return Promise.resolve();
  }

  /**
   * Maximum continuous execution time per frame (in milliseconds)
   * Prevents blocking the main thread for too long
   */
  private readonly MAX_CONTINUOUS_MS = 5;

  /**
   * Minimum remaining idle time threshold (in milliseconds)
   * Ensures the browser has enough idle time for other tasks
   */
  private readonly MIN_IDLE_REMAINING = 2;

  private computation<T, R>(
    data: T[],
    processor: ShardProcessor<T, R[]>,
    onComplete: (results: R[]) => void,
  ): void {
    let index = 0;
    const results: R[] = [];
    const MAX_CONTINUOUS_MS = this.MAX_CONTINUOUS_MS;
    const MIN_IDLE_REMAINING = this.MIN_IDLE_REMAINING;

    const processNextShard = (deadline: IdleDeadline) => {
      const frameStart = performance.now();

      while (index < data.length) {
        // Dual condition check: both actual elapsed time and idle deadline
        const elapsed = performance.now() - frameStart;
        const remaining = deadline.timeRemaining();

        // Yield control when time budget is exhausted
        if (elapsed > MAX_CONTINUOUS_MS || remaining < MIN_IDLE_REMAINING) {
          safeRequestIdleCallback(processNextShard, {
            timeout: 1000,
          });
          return;
        }

        // Process one item at a time for better granularity
        const result = processor([data[index]]);
        if (result && result.length > 0) {
          results.push(...result);
        }
        index++;
      }

      // All items processed
      onComplete(results);
    };

    safeRequestIdleCallback(processNextShard, {
      timeout: 1000,
    });
  }
}

export { ShardingScheduler, safeRequestIdleCallback, safeCancelIdleCallback };
