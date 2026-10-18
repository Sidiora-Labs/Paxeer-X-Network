import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  OrderType,
} from "@paxeer-network/types";
import {
  OrderCreator,
  ValuesDepConfig,
  OrderValidationItem,
  OrderValidationResult,
} from "./interface";
import { TPSLValidationStrategy } from "./validators/TPSLValidationStrategy";

export type AlgoOrderUpdateEntity = {
  trigger_price?: number;
  price?: number;
  order_id: number;
  quantity?: number;
  is_activated?: boolean;
  order_type?: OrderType;
};

/**
 * Base creator for algo orders (TP/SL orders)
 * Uses TPSLValidationStrategy to eliminate code duplication
 */
export abstract class BaseAlgoOrderCreator<
  T extends AlgoOrderEntity<
    AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
  >,
> implements OrderCreator<T> {
  private tpslValidationStrategy = new TPSLValidationStrategy();

  abstract create(values: T, config: ValuesDepConfig): T;

  /**
   * Validates TP/SL order using TPSLValidationStrategy
   * Consolidates validation logic from baseBracketOrderCreator
   */
  validate(
    values: Partial<T>,
    config: ValuesDepConfig,
  ): Promise<{
    [P in keyof T]?: OrderValidationItem;
  }> {
    return Promise.resolve(
      this.tpslValidationStrategy.validate(values, config) as {
        [P in keyof T]?: OrderValidationItem;
      },
    );
  }

  abstract get type(): OrderType;
}
