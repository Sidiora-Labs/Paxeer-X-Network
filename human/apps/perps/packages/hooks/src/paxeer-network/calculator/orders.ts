import { API } from "@paxeer-network/types";
import { Calculator, CalculatorCtx, CalculatorScope } from "../../types";
import { BaseCalculator } from "./baseCalculator";
import { Portfolio } from "../appStore";
import { Decimal } from "@paxeer-network/utils";

export class OrderCalculator extends BaseCalculator<API.OrderExt[]> {
  name = "orders";

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx): API.OrderExt[] {
    // Implementation logic here
    return [];
  }

  update(data: any | null, scope: CalculatorScope): void {
    // Implementation logic here
  }
}
