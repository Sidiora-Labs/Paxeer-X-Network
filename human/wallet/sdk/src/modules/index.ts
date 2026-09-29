import {
  abiEventTopic,
  sendPrecompileCall,
  type DecodedPrecompileEvent,
  type Eip1193Requester,
  type PrecompileCall,
  type PrecompileEventSpec,
  type PrecompileLog,
} from '@sidiora/layerx-sdk/browser';

export type ModuleProvider = Eip1193Requester;
export type ModuleTransaction = PrecompileCall;
export type ModuleLog = PrecompileLog;
export type ModuleEvent = DecodedPrecompileEvent;

export type ModuleErrorCode =
  | 'invalid_address'
  | 'invalid_value'
  | 'invalid_answer'
  | 'unknown_event'
  | 'malformed_log'
  | 'refused'
  | 'unavailable';

export class ModuleError extends Error {
  readonly code: ModuleErrorCode;
  readonly field: string;

  constructor(code: ModuleErrorCode, field: string) {
    super(`${code}: ${field}`);
    this.name = 'ModuleError';
    this.code = code;
    this.field = field;
  }
}

const ADDRESS = /^0x[0-9a-fA-F]{40}$/u;
const HEX = /^0x(?:[0-9a-fA-F]{2})*$/u;
const BYTES32 = /^0x[0-9a-fA-F]{64}$/u;
const WORD = 32;

export function moduleAddress(value: string, field = 'address'): string {
  if (typeof value !== 'string' || !ADDRESS.test(value)) {
    throw new ModuleError('invalid_address', field);
  }
  return value.toLowerCase();
}

export function retarget(call: PrecompileCall, address: string): ModuleTransaction {
  return { to: address, data: call.data, value: call.value };
}

export function retargetEvents(specs: readonly PrecompileEventSpec[], address: string): readonly PrecompileEventSpec[] {
  return specs.map((spec) => ({ name: spec.name, precompile: address, inputs: spec.inputs }));
}

export function sendModuleTransaction(provider: ModuleProvider, from: string, tx: ModuleTransaction): Promise<string> {
  return sendPrecompileCall(provider, moduleAddress(from, 'from'), tx);
}

export async function ethCall(provider: ModuleProvider, to: string, data: string): Promise<string> {
  const answer = await provider.request({ method: 'eth_call', params: [{ to, data }, 'latest'] });
  if (typeof answer !== 'string' || !HEX.test(answer)) {
    throw new ModuleError('invalid_answer', 'eth_call');
  }
  return answer.toLowerCase();
}

export async function ethQuantity(provider: ModuleProvider, method: string): Promise<bigint> {
  const answer = await provider.request({ method, params: [] });
  if (typeof answer !== 'string' || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/u.test(answer)) {
    throw new ModuleError('invalid_answer', method);
  }
  return BigInt(answer);
}

export function hexBytes(value: string, field: string): Uint8Array {
  if (typeof value !== 'string' || !HEX.test(value)) {
    throw new ModuleError('invalid_value', field);
  }
  const body = value.slice(2);
  return Uint8Array.from({ length: body.length / 2 }, (_, index) => Number.parseInt(body.slice(index * 2, index * 2 + 2), 16));
}

export function bytesHex(bytes: Uint8Array): string {
  return `0x${Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')}`;
}

export function uintWordHex(value: bigint, bits = 256, field = 'word'): string {
  if (typeof value !== 'bigint' || value < 0n || value >= 1n << BigInt(bits)) {
    throw new ModuleError('invalid_value', field);
  }
  return value.toString(16).padStart(64, '0');
}

function word(data: Uint8Array, offset: number, field: string): Uint8Array {
  if (!Number.isSafeInteger(offset) || offset < 0 || offset + WORD > data.length) {
    throw new ModuleError('malformed_log', field);
  }
  return data.subarray(offset, offset + WORD);
}

function integer(bytes: Uint8Array): bigint {
  return bytes.reduce((value, byte) => (value << 8n) | BigInt(byte), 0n);
}

function index(data: Uint8Array, offset: number, field: string): number {
  const value = integer(word(data, offset, field));
  if (value > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new ModuleError('malformed_log', field);
  }
  return Number(value);
}

function dynamicBytes(data: Uint8Array, headOffset: number, field: string): Uint8Array {
  const offset = index(data, headOffset, field);
  const length = index(data, offset, field);
  const start = offset + WORD;
  if (start + length > data.length) {
    throw new ModuleError('malformed_log', field);
  }
  return data.subarray(start, start + length);
}

function utf8(bytes: Uint8Array, field: string): string {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    throw new ModuleError('malformed_log', field);
  }
}

export function decodeAbiUint(answer: string, bits = 256): bigint {
  const data = hexBytes(answer, 'answer');
  if (data.length !== WORD) {
    throw new ModuleError('invalid_answer', 'uint');
  }
  const value = integer(data);
  if (value >= 1n << BigInt(bits)) {
    throw new ModuleError('invalid_answer', 'uint');
  }
  return value;
}

export function decodeAbiString(answer: string): string {
  const data = hexBytes(answer, 'answer');
  try {
    return utf8(dynamicBytes(data, 0, 'string'), 'string');
  } catch {
    throw new ModuleError('invalid_answer', 'string');
  }
}

export type WordType = 'address' | 'bool' | 'bytes32' | 'uint8' | 'uint32' | 'uint64' | 'uint256';
export type ModuleEventType = WordType | 'bytes' | 'string';

export interface ModuleEventInput {
  readonly name: string;
  readonly type: ModuleEventType;
  readonly indexed: boolean;
}

export interface ModuleEventSpec {
  readonly name: string;
  readonly inputs: readonly ModuleEventInput[];
}

export type ModuleEventFields = Readonly<Record<string, bigint | boolean | string>>;

export interface DecodedModuleEvent {
  readonly event: string;
  readonly precompile: string;
  readonly topic0: string;
  readonly fields: ModuleEventFields;
}

export function moduleEventTopic(spec: ModuleEventSpec): string {
  return abiEventTopic(`${spec.name}(${spec.inputs.map((input) => input.type).join(',')})`);
}

function wordValue(type: WordType, bytes: Uint8Array, field: string): bigint | boolean | string {
  switch (type) {
    case 'address':
      if (bytes.subarray(0, 12).some((byte) => byte !== 0)) {
        throw new ModuleError('malformed_log', field);
      }
      return bytesHex(bytes.subarray(12));
    case 'bytes32':
      return bytesHex(bytes);
    case 'bool': {
      const value = integer(bytes);
      if (value > 1n) {
        throw new ModuleError('malformed_log', field);
      }
      return value === 1n;
    }
    default: {
      const value = integer(bytes);
      if (value >= 1n << BigInt(Number(type.slice(4)))) {
        throw new ModuleError('malformed_log', field);
      }
      return value;
    }
  }
}

export function decodeModuleEvent(specs: readonly ModuleEventSpec[], address: string, log: ModuleLog): DecodedModuleEvent {
  const topic0 = log.topics[0]?.toLowerCase();
  const spec = specs.find((candidate) => moduleEventTopic(candidate) === topic0);
  if (typeof log.address !== 'string' || log.address.toLowerCase() !== address || spec === undefined || topic0 === undefined) {
    throw new ModuleError('unknown_event', topic0 ?? 'topic0');
  }
  const indexed = spec.inputs.filter((input) => input.indexed);
  if (log.topics.length !== indexed.length + 1) {
    throw new ModuleError('malformed_log', 'topics');
  }
  const data = hexBytes(log.data, 'data');
  const fields: Record<string, bigint | boolean | string> = {};
  let topic = 1;
  let head = 0;
  let dynamic = false;
  for (const input of spec.inputs) {
    if (input.indexed) {
      const value = log.topics[topic] ?? '';
      if (!BYTES32.test(value) || input.type === 'bytes' || input.type === 'string') {
        throw new ModuleError('malformed_log', input.name);
      }
      fields[input.name] = wordValue(input.type, hexBytes(value, input.name), input.name);
      topic += 1;
      continue;
    }
    if (input.type === 'bytes') {
      fields[input.name] = bytesHex(dynamicBytes(data, head, input.name));
      dynamic = true;
    } else if (input.type === 'string') {
      fields[input.name] = utf8(dynamicBytes(data, head, input.name), input.name);
      dynamic = true;
    } else {
      fields[input.name] = wordValue(input.type, word(data, head, input.name), input.name);
    }
    head += WORD;
  }
  if (!dynamic && data.length !== head) {
    throw new ModuleError('malformed_log', 'data');
  }
  return { event: spec.name, precompile: address, topic0, fields };
}

export * from './exchange.js';
export * from './bridge.js';
export * from './launchpad.js';
export * from './fee-token.js';
export * from './web-data.js';
export * from './gas-station.js';
export * from './fee-choice.js';
