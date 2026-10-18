import { Calculator, CalculatorCtx, CalculatorScope } from "../../types";
import { BaseCalculator } from "./baseCalculator";

class OrderEntryCalculator extends BaseCalculator<any> {
  name: string = "orderEntry";

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx) {
    console.log("!!!! Calculating positions...", scope, data);
    return [];
  }

  update(data: any) {
    console.log("!!!! Updating orderEntry...", data);
  }
}

export { OrderEntryCalculator };
