import { PaxeerNetworkOrder } from "@paxeer-network/types";
import {
  OrderValidationItem,
  OrderValidationResult,
  ValuesDepConfig,
} from "../interface";

/**
 * Base validator class implementing Chain of Responsibility pattern
 * Each validator can pass validation to the next validator in the chain
 */
export abstract class BaseValidator {
  protected next?: BaseValidator;

  /**
   * Sets the next validator in the chain
   * @param validator - The next validator to call
   * @returns The next validator for method chaining
   */
  setNext(validator: BaseValidator): BaseValidator {
    this.next = validator;
    return validator;
  }

  /**
   * Validates values and passes to next validator in chain
   * @param values - The values to validate
   * @param config - Configuration for validation
   * @param errors - Accumulated validation errors
   * @returns Updated validation result
   */
  validate(
    values: any,
    config: ValuesDepConfig,
    errors: OrderValidationResult,
  ): OrderValidationResult {
    // Perform this validator's validation
    const result = this.doValidate(values, config);
    if (result) {
      errors[this.getFieldName()] = result;
    }

    // Pass to next validator in chain, or return if no next validator
    return this.next?.validate(values, config, errors) ?? errors;
  }

  /**
   * Performs the actual validation logic
   * Must be implemented by concrete validators
   * @param values - The values to validate
   * @param config - Configuration for validation
   * @returns Validation error if validation fails, undefined otherwise
   */
  protected abstract doValidate(
    values: any,
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined;

  /**
   * Returns the field name this validator validates
   * Must be implemented by concrete validators
   * @returns The field name as a key of PaxeerNetworkOrder
   */
  protected abstract getFieldName(): keyof PaxeerNetworkOrder;
}
