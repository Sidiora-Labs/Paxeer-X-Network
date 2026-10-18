import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { IValidationStrategy } from "./IValidationStrategy";

/**
 * Strategy for validating slippage in market orders
 * Compares user's slippage tolerance with estimated slippage
 */
export class SlippageValidationStrategy
  implements
    IValidationStrategy<{
      slippage?: number | string;
    }>
{
  /**
   * Validates slippage against estimated slippage
   * @param values - Object containing slippage value
   * @param config - Configuration with estimated slippage
   * @returns Validation error if slippage exceeds estimated, undefined otherwise
   */
  validate(
    values: { slippage?: number | string },
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    const slippage = Number(values.slippage);
    const estSlippage = Number.isNaN(config.estSlippage)
      ? 0
      : Number(config.estSlippage) * 100;

    if (!isNaN(slippage) && estSlippage > slippage) {
      return {
        type: "max",
        message: "Estimated slippage exceeds your maximum allowed slippage.",
        value: estSlippage,
      } as OrderValidationItem;
    }

    return undefined;
  }
}
