import { OrderValidationResult, ValuesDepConfig } from "../interface";
import { BaseValidator } from "./BaseValidator";

/**
 * Validation chain implementing Chain of Responsibility pattern
 * Allows chaining multiple validators together for sequential validation
 */
export class ValidationChain {
  private validators: BaseValidator[] = [];
  private head?: BaseValidator;

  /**
   * Adds a validator to the chain
   * @param validator - The validator to add
   * @returns This chain instance for method chaining
   */
  addValidator(validator: BaseValidator): this {
    this.validators.push(validator);

    // Build chain by linking validators
    if (!this.head) {
      this.head = validator;
    } else {
      // Find the last validator in the chain and link the new one
      let current = this.head;
      while (current && (current as any).next) {
        current = (current as any).next;
      }
      if (current) {
        current.setNext(validator);
      }
    }

    return this;
  }

  /**
   * Adds multiple validators to the chain
   * @param validators - Array of validators to add
   * @returns This chain instance for method chaining
   */
  addValidators(validators: BaseValidator[]): this {
    validators.forEach((validator) => this.addValidator(validator));
    return this;
  }

  /**
   * Validates values using all validators in the chain
   * @param values - The values to validate
   * @param config - Configuration for validation
   * @returns Validation result with all errors found
   */
  validate(values: any, config: ValuesDepConfig): OrderValidationResult {
    const errors: OrderValidationResult = {};

    if (this.head) {
      return this.head.validate(values, config, errors);
    }

    return errors;
  }

  /**
   * Clears all validators from the chain
   */
  clear(): void {
    this.validators = [];
    this.head = undefined;
  }

  /**
   * Gets the number of validators in the chain
   * @returns The number of validators
   */
  get length(): number {
    return this.validators.length;
  }
}
