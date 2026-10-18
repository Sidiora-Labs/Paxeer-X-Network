import { Calculator, CalculatorCtx, CalculatorScope } from "../../types";
import { BaseCalculator } from "./baseCalculator";
import { useMarkPriceStore } from "../useMarkPrice/useMarkPriceStore";

export const MarketCalculatorName = "markPriceCalculator";
class MarkPriceCalculator extends BaseCalculator<any> {
  name: string = MarketCalculatorName;

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx) {
    return data;
  }

  update(data: any, scope: CalculatorScope) {
    useMarkPriceStore.getState().actions.updateMarkPrice(data);
  }
}

export { MarkPriceCalculator };
