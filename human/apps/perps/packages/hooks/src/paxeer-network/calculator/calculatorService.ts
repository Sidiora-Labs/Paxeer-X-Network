import { Calculator, CalculatorScheduler, CalculatorScope } from "../../types";
import { CalculatorContext } from "./calculatorContext";

/**
 * Batch update collector type for grouping updates by scope and calculator name
 */
type BatchUpdateCollector = Map<CalculatorScope, Map<string, any>>;

type CalcOptions = {
  skipUpdate?: boolean;
  skipPending?: boolean;
  /**
   * Skip calculation when the tab is unactivated.
   */
  skipWhenOnPause?: boolean;
};

type CalcItem = {
  scope: CalculatorScope;
  data: any;
  options?: CalcOptions;
};

class CalculatorService {
  private calculators: Map<string, Calculator[]>;

  private pendingCalc: CalcItem[] = [];

  private calcQueue: CalcItem[] = [];

  /**
   * Reference count for each calculator, used to determine if a calculator is still in use.
   */
  private referenceCount: Map<string, number> = new Map();

  private ctx?: CalculatorContext;

  private isPaused = false;

  constructor(
    private readonly scheduler: CalculatorScheduler,
    calculators: [string, Calculator[]][],
  ) {
    this.calculators = new Map(calculators);
  }

  register(scope: string, calculator: Calculator) {
    const ref_count_name = `${scope}_${calculator.name}`;
    const count = this.referenceCount.get(ref_count_name);
    // console.log("register", scope, calculator.name, count);
    if (typeof count !== "undefined" && count > 0) {
      this.referenceCount.set(ref_count_name, count + 1);

      return;
    }

    const calculators = this.calculators.get(scope);

    if (Array.isArray(calculators)) {
      calculators.push(calculator);

      // this.calculators.set(scope, [...calculators, calculator]);
    } else {
      this.calculators.set(scope, [calculator]);
    }

    // console.log("register=====>>>>>>", scope, calculator.name);

    this.referenceCount.set(ref_count_name, 1);
  }

  unregister(scope: string, calculator: Calculator) {
    const ref_count_name = `${scope}_${calculator.name}`;
    const count = this.referenceCount.get(ref_count_name);

    // console.log("unregister", scope, calculator.name, count);

    if (typeof count !== "undefined" && count > 1) {
      this.referenceCount.set(ref_count_name, count - 1);
      return;
    }
    const calculators = this.calculators.get(scope);
    if (Array.isArray(calculators)) {
      const index = calculators.findIndex((c) => c.name === calculator.name);
      // console.log("<<<<<=======unregister", scope, calculator.name, index);
      if (index > -1) {
        calculators[index].destroy?.();
        calculators.splice(index, 1);
      }
    }

    this.referenceCount.delete(ref_count_name);
  }

  async calc(scope: CalculatorScope, data: any, options?: CalcOptions) {
    if (scope !== CalculatorScope.POSITION) {
      if (!options?.skipWhenOnPause) {
        console.log(`----`);
      }
    }
    const ctx = CalculatorContext.create(scope, data);

    if (!ctx.isReady && options?.skipPending) {
      return;
    }

    // handle pending calc
    // await this.handlePendingCalc();

    /**
     * if the tab is not activated and this action allows skip, then skip
     */
    if (options?.skipWhenOnPause && !this.windowIsVisible) return;

    // console.log("-------------[calc]:", scope, ctx.isReady, data);
    this.calcQueue.push({ scope, data, options });

    // this.calcQueue.push({ scope, data, options });

    await this.handleCalcQueue(ctx);

    this.ctx = ctx;
  }

  // private async handlePendingCalc() {
  //   // console.log("[handlePendingCalc]:", this.pendingCalc);
  //   if (this.pendingCalc.length === 0) return;
  //   this.calcQueue = [...this.pendingCalc, ...this.calcQueue];

  //   this.pendingCalc = [];
  // }

  private async handleCalcQueue(context?: CalculatorContext) {
    // Phase 1: Collect all updates from queue tasks
    const batchCollector: BatchUpdateCollector = new Map();
    let currentContext = context;

    while (this.calcQueue.length > 0) {
      const first = this.calcQueue.shift();
      if (!first) break;

      const { scope, data, options } = first;
      const ctx = currentContext || CalculatorContext.create(scope, data);
      const calculators = this.calculators.get(scope);

      if (Array.isArray(calculators) && calculators.length) {
        try {
          await this.scheduler.calc(scope, calculators, data, ctx);
        } catch (e) {
          console.error(e);
        }

        // Collect updates (skip immediate update call)
        if (!options?.skipUpdate) {
          this.collectUpdates(
            batchCollector,
            scope,
            calculators,
            ctx.outputToValue(),
          );
        }
      }

      // Update context for next task
      currentContext = ctx;
    }

    // Phase 2: Batch commit all updates
    await this.commitBatchUpdates(batchCollector);

    // Save context
    this.ctx = currentContext;
  }

  private collectUpdates(
    collector: BatchUpdateCollector,
    scope: CalculatorScope,
    calculators: Calculator[],
    data: Record<string, any>,
  ): void {
    if (!collector.has(scope)) {
      collector.set(scope, new Map());
    }

    const scopeCollector = collector.get(scope)!;

    for (const calculator of calculators) {
      const item = data[calculator.name];
      if (item !== undefined && item !== null) {
        scopeCollector.set(calculator.name, item);
      }
    }
  }

  private async commitBatchUpdates(
    collector: BatchUpdateCollector,
  ): Promise<void> {
    if (collector.size === 0) return;

    for (const [scope, updateMap] of collector.entries()) {
      const calculators = this.calculators.get(scope);
      if (!Array.isArray(calculators)) continue;

      const batchData: Record<string, any> = {};
      for (const [calculatorName, data] of updateMap.entries()) {
        batchData[calculatorName] = data;
      }

      try {
        this.scheduler.update(scope, calculators, batchData);
      } catch (e) {
        console.error(`Batch update failed for scope ${scope}:`, e);
      }
    }

    collector.clear();
  }

  stop() {
    this.calcQueue = [];
    this.ctx?.clearCache();
  }

  private get windowIsVisible() {
    if (typeof document === "undefined") {
      return true;
    }
    return document.visibilityState === "visible";
  }
}

const CalculatorServiceID = "CalculatorService";

// SimpleDI.registerByName(CalculatorServiceID, CalculatorService);

export { CalculatorService, CalculatorServiceID };
