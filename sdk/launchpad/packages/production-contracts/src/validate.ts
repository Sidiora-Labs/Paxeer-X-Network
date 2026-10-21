import type { ContractTypes } from './types.gen.js';
import { Ajv2020, type ValidateFunction, type ErrorObject } from 'ajv/dist/2020.js';
import addFormatsModule from 'ajv-formats';
import schemaDocument from './schemas.gen.json' with { type: 'json' };

// Unknown is the untrusted-input boundary, never a generated DTO fallback.
const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: true, allowUnionTypes: true });
const addFormats = addFormatsModule as unknown as (instance: Ajv2020) => void;
addFormats(ajv);
ajv.addSchema(schemaDocument, 'kindle-v1');
const validators = new Map<string, ValidateFunction>();
export class ContractValidationError extends Error {
  constructor(readonly contract: string, readonly violations: ReadonlyArray<ErrorObject>) {
    super(`Invalid contract ${contract}: ${violations.map(e => `${e.instancePath} ${e.message ?? e.keyword}`).join('; ')}`);
    this.name = 'ContractValidationError';
  }
}
export function validateContract(name: string, value: unknown, schemaMajor = 1): boolean {
  if (schemaMajor !== 1) throw new Error(`Unsupported schema major ${schemaMajor}`);
  if (!Object.hasOwn(schemaDocument.$defs, name)) throw new Error(`Unknown contract ${name}`);
  let validator = validators.get(name);
  if (!validator) {
    validator = ajv.compile({ $ref: `kindle-v1#/$defs/${name}` });
    validators.set(name, validator);
  }
  return validator(value);
}
export function decodeContract<N extends keyof ContractTypes>(name: N, value: unknown, schemaMajor = 1): ContractTypes[N] {
  if (!validateContract(name, value, schemaMajor)) throw new ContractValidationError(name, [...(validators.get(name)?.errors ?? [])]);
  // Caller can only assert a generated named type; runtime validation is authoritative.
  return value as ContractTypes[N];
}
