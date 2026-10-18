import { AlgoOrderEntity, AlgoOrderRootType } from "@paxeer-network/types";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { TPSLValidationStrategy } from "./validators/TPSLValidationStrategy";

/**
 * Validates bracket order (order with TP/SL)
 * Uses TPSLValidationStrategy to eliminate code duplication
 */
export async function bracketOrderValidator<
  T extends AlgoOrderEntity<
    AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
  >,
>(values: Partial<T>, config: ValuesDepConfig): Promise<OrderValidationResult> {
  const strategy = new TPSLValidationStrategy();
  return strategy.validate(values, config);
}
