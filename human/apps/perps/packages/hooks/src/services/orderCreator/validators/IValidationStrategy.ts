import { OrderValidationItem, OrderValidationResult, ValuesDepConfig } from "../interface";

/**
 * Strategy interface for validation operations
 * Implements Strategy Pattern to allow different validation algorithms
 * to be interchangeable
 */
export interface IValidationStrategy<T = any, R = OrderValidationItem | undefined> {
  /**
   * Validates the given values against the configuration
   * @param values - The values to validate
   * @param config - Configuration containing symbol info, mark price, etc.
   * @returns Validation error item if validation fails, undefined otherwise
   */
  validate(values: T, config: ValuesDepConfig): R;
}
