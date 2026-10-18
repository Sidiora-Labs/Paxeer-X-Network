import { CalculatorCtx, CalculatorScope } from "../../types";
import { BaseCalculator } from "./baseCalculator";

import { useIndexPriceStore } from "../useIndexPrice/useIndexPriceStore";

export const IndexPriceCalculatorName = "indexPriceCalculator";

class IndexPriceCalculator extends BaseCalculator<any> {
  name: string = IndexPriceCalculatorName;

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx) {
    return data;
  }

  update(data: any) {
    if (!data) return;
    useIndexPriceStore.getState().actions.updateIndexPrice(data);
  }

  static getValue() {
    return useIndexPriceStore.getState().indexPrices;
  }
}

export { IndexPriceCalculator };
