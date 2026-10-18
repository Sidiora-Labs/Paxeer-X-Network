import { Calculator, CalculatorCtx, CalculatorScope } from "../../types";

abstract class BaseCalculator<T> implements Calculator<T> {
  abstract name: string;
  private _cache?: T;
  abstract calc(
    scope: CalculatorScope,
    data: any,
    ctx: CalculatorCtx
  ): T | null;
  abstract update(data: T | null, scope: CalculatorScope): void;

  cache(data: T) {
    this._cache = data;
  }
}

export { BaseCalculator };
