import type { ContractTypes } from './types.gen.js';
import type { ErrorObject } from 'ajv/dist/2020.js';
import { contractValidators } from './validators-v1.gen.js';

// Unknown is the untrusted-input boundary, never a generated DTO fallback.
// Validators are compiled ahead of time so no code generation happens under a browser content security policy.
export function errorsText(errors: ReadonlyArray<ErrorObject> | null | undefined): string {
  return (errors ?? []).map(error => `data${error.instancePath} ${error.message ?? error.keyword}`).join(', ');
}
export class ContractValidationError extends Error {
  constructor(readonly contract: string, readonly violations: ReadonlyArray<ErrorObject>) {
    super(`Invalid contract ${contract}: ${violations.map(e => `${e.instancePath} ${e.message ?? e.keyword}`).join('; ')}`);
    this.name = 'ContractValidationError';
  }
}
export function validateContract(name: string, value: unknown, schemaMajor = 1): boolean {
  if (schemaMajor !== 1) throw new Error(`Unsupported schema major ${schemaMajor}`);
  const validator = Object.hasOwn(contractValidators, name) ? contractValidators[name] : undefined;
  if (!validator) throw new Error(`Unknown contract ${name}`);
  return validator(value);
}
export function decodeContract<N extends keyof ContractTypes>(name: N, value: unknown, schemaMajor = 1): ContractTypes[N] {
  if (!validateContract(name, value, schemaMajor)) throw new ContractValidationError(name, [...(contractValidators[name]?.errors ?? [])]);
  // Caller can only assert a generated named type; runtime validation is authoritative.
  return value as ContractTypes[N];
}
