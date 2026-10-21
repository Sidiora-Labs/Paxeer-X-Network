import { PAXEER_CHAIN_ID } from '../provider.js';
import {
  XWEB_KIND_API,
  XWEB_PRECOMPILE,
  abiSelector,
  buildXWebApiRequest,
  decodeXWebAttestors,
  encodeAbiCall,
  xwebApiRequestCall,
  xwebGetAttestorsCallData,
  type XWebApiBuildOptions,
  type XWebApiCall,
  type XWebApiRequest,
  type XWebAttestorSet,
} from '@sidiora/layerx-sdk/browser';

import {
  ModuleError,
  bytesHex,
  decodeAbiUint,
  decodeModuleEvent,
  ethCall,
  hexBytes,
  moduleAddress,
  retarget,
  sendModuleTransaction,
  uintWordHex,
  type DecodedModuleEvent,
  type ModuleEventSpec,
  type ModuleLog,
  type ModuleProvider,
  type ModuleTransaction,
} from './index.js';

export const WEB_DATA_KIND_FETCH = 1;
export const WEB_DATA_KIND_SEARCH = 2;
export const WEB_DATA_KIND_API = XWEB_KIND_API;

export type WebDataKind = typeof WEB_DATA_KIND_FETCH | typeof WEB_DATA_KIND_SEARCH | typeof WEB_DATA_KIND_API;

export const WEB_DATA_EVENTS: readonly ModuleEventSpec[] = [
  {
    name: 'XWebRequested',
    inputs: [
      { name: 'requestId', type: 'uint64', indexed: true },
      { name: 'requester', type: 'address', indexed: true },
      { name: 'kind', type: 'uint8', indexed: false },
      { name: 'payload', type: 'bytes', indexed: false },
      { name: 'callbackGas', type: 'uint64', indexed: false },
      { name: 'paid', type: 'uint256', indexed: false },
      { name: 'timeoutHeight', type: 'uint64', indexed: false },
    ],
  },
  {
    name: 'XWebFulfilled',
    inputs: [
      { name: 'requestId', type: 'uint64', indexed: true },
      { name: 'requester', type: 'address', indexed: true },
      { name: 'contentDigest', type: 'bytes32', indexed: false },
      { name: 'fullLength', type: 'uint32', indexed: false },
      { name: 'level', type: 'uint8', indexed: false },
      { name: 'callback', type: 'uint8', indexed: false },
      { name: 'callbackGasUsed', type: 'uint64', indexed: false },
    ],
  },
  {
    name: 'XWebRefunded',
    inputs: [
      { name: 'requestId', type: 'uint64', indexed: true },
      { name: 'requester', type: 'address', indexed: true },
      { name: 'refunded', type: 'uint256', indexed: false },
    ],
  },
];

export const LEDGER_PRECOMPILE = '0x000000000000000000000000000000000000101a';
export const PAYER_GRANT_BYTES = 346;

// The lowest block each known chain can carry a ledger precompile log at. The merge plan that brings the ledger
// precompile online follows the v6.12.0 plan, applied on Paxeer mainnet at 37333101, so no ledger log sits below it;
// raise the entry to the merge plan height once that plan is applied.
export const LEDGER_PRECOMPILE_LOG_FLOOR: Readonly<Record<number, bigint>> = Object.freeze({ [PAXEER_CHAIN_ID]: 37_333_101n });

export function ledgerLogFloor(chainId: number): bigint {
  return LEDGER_PRECOMPILE_LOG_FLOOR[chainId] ?? 0n;
}

export interface WebPayerGrant {
  readonly grantId: string;
  readonly from: string;
  readonly recipient: string;
  readonly asset: string;
  readonly perDrawMaximum: bigint;
  readonly allowance: bigint;
  readonly recurring: boolean;
  readonly windowLength: bigint;
  readonly expiration: bigint;
  readonly purposeHash: string;
  readonly hasReference: boolean;
  readonly referenceHash: string;
  readonly revocationSequence: bigint;
  readonly publicKey: string;
  readonly signature: string;
  readonly drawnTotal: bigint;
  readonly drawnThisPeriod: bigint;
  readonly windowStart: bigint;
  readonly revokedAt: bigint;
  readonly revoked: boolean;
  readonly invoiceSettled: boolean;
}

export interface WebDataCaps {
  readonly fee: bigint;
  readonly grants: readonly WebPayerGrant[];
}

export interface WebDataModule {
  readonly address: string;
  readonly events: readonly ModuleEventSpec[];
  request(kind: WebDataKind, payload: Uint8Array | string, callbackGas: bigint, fee: bigint): ModuleTransaction;
  fetch(url: string, callbackGas: bigint, fee: bigint): ModuleTransaction;
  search(query: string, callbackGas: bigint, fee: bigint): ModuleTransaction;
  api(request: XWebApiRequest, callbackGas: bigint, fee: bigint): ModuleTransaction;
  buildApiRequest(call: XWebApiCall, attestors: XWebAttestorSet, options?: XWebApiBuildOptions): XWebApiRequest;
  refund(requestId: bigint): ModuleTransaction;
  fee(): Promise<bigint>;
  caps(grantIds: readonly string[]): Promise<WebDataCaps>;
  attestors(): Promise<XWebAttestorSet>;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): DecodedModuleEvent;
}

const REQUEST_SELECTOR = abiSelector('request(uint8,bytes,uint64)');
const FEE_SELECTOR = abiSelector('fee()');
const GET_PAYER_GRANT_SELECTOR = abiSelector('getPayerGrant(bytes32)');
const WORD = 32;
const GRANT_ANSWER_HEAD = 7 * WORD;
const GRANT_ANSWER_BYTES = GRANT_ANSWER_HEAD + WORD + Math.ceil(PAYER_GRANT_BYTES / WORD) * WORD;
const BYTES32 = /^0x[0-9a-fA-F]{64}$/u;

function bigEndian(bytes: Uint8Array): bigint {
  return bytes.reduce((value, byte) => (value << 8n) | BigInt(byte), 0n);
}

function grantIdHex(value: string): string {
  if (typeof value !== 'string' || !BYTES32.test(value)) {
    throw new ModuleError('invalid_value', 'grantId');
  }
  return value.toLowerCase();
}

function decodePayerGrant(answer: string, grantId: string): WebPayerGrant {
  const data = hexBytes(answer, 'getPayerGrant');
  const malformed = (): ModuleError => new ModuleError('invalid_answer', 'getPayerGrant');
  if (data.length !== GRANT_ANSWER_BYTES) {
    throw malformed();
  }
  const word = (slot: number, bits: number): bigint => {
    const value = bigEndian(data.subarray(slot * WORD, (slot + 1) * WORD));
    if (value >= 1n << BigInt(bits)) {
      throw malformed();
    }
    return value;
  };
  const end = GRANT_ANSWER_HEAD + WORD + PAYER_GRANT_BYTES;
  if (word(0, 256) !== BigInt(GRANT_ANSWER_HEAD) || word(7, 256) !== BigInt(PAYER_GRANT_BYTES) || data.subarray(end).some((byte) => byte !== 0)) {
    throw malformed();
  }
  const grant = data.subarray(GRANT_ANSWER_HEAD + WORD, end);
  let offset = 0;
  const take = (length: number): Uint8Array => grant.subarray(offset, (offset += length));
  const flag = (bytes: Uint8Array): boolean => {
    if (bytes[0] !== 0 && bytes[0] !== 1) {
      throw malformed();
    }
    return bytes[0] === 1;
  };
  const decoded: WebPayerGrant = {
    grantId: bytesHex(take(32)),
    from: bytesHex(take(32)),
    recipient: bytesHex(take(32)),
    asset: bytesHex(take(32)),
    perDrawMaximum: bigEndian(take(16)),
    allowance: bigEndian(take(16)),
    recurring: flag(take(1)),
    windowLength: bigEndian(take(8)),
    expiration: bigEndian(take(8)),
    purposeHash: bytesHex(take(32)),
    hasReference: flag(take(1)),
    referenceHash: bytesHex(take(32)),
    revocationSequence: bigEndian(take(8)),
    publicKey: bytesHex(take(32)),
    signature: bytesHex(take(64)),
    drawnTotal: word(1, 128),
    drawnThisPeriod: word(2, 128),
    windowStart: word(3, 64),
    revokedAt: word(4, 64),
    revoked: word(5, 1) === 1n,
    invoiceSettled: word(6, 1) === 1n,
  };
  if (decoded.grantId !== grantId) {
    throw malformed();
  }
  return Object.freeze(decoded);
}

function payloadBytes(payload: Uint8Array | string): Uint8Array {
  return typeof payload === 'string' ? new TextEncoder().encode(payload) : payload;
}

function requestCall(kind: WebDataKind, payload: Uint8Array | string, callbackGas: bigint, fee: bigint): ModuleTransaction {
  if (kind !== WEB_DATA_KIND_FETCH && kind !== WEB_DATA_KIND_SEARCH && kind !== WEB_DATA_KIND_API) {
    throw new ModuleError('invalid_value', 'kind');
  }
  const bytes = payloadBytes(payload);
  if (bytes.length === 0) {
    throw new ModuleError('invalid_value', 'payload');
  }
  const call = xwebApiRequestCall(bytes, callbackGas, fee);
  const kindWord = uintWordHex(BigInt(kind), 8, 'kind');
  const head = REQUEST_SELECTOR.length;
  return { to: call.to, data: REQUEST_SELECTOR + kindWord + call.data.slice(head + 64), value: call.value };
}

export function webData(provider: ModuleProvider, address: string = XWEB_PRECOMPILE): WebDataModule {
  const target = moduleAddress(address);
  const request = (kind: WebDataKind, payload: Uint8Array | string, callbackGas: bigint, fee: bigint): ModuleTransaction =>
    retarget(requestCall(kind, payload, callbackGas, fee), target);
  const fee = async (): Promise<bigint> => decodeAbiUint(await ethCall(provider, target, FEE_SELECTOR));
  const grant = async (grantId: string): Promise<WebPayerGrant> =>
    decodePayerGrant(await ethCall(provider, LEDGER_PRECOMPILE, GET_PAYER_GRANT_SELECTOR + grantId.slice(2)), grantId);
  return {
    address: target,
    caps: async (grantIds) => {
      const ids = grantIds.map(grantIdHex);
      const [paid, grants] = await Promise.all([fee(), Promise.all(ids.map(grant))]);
      return Object.freeze({ fee: paid, grants: Object.freeze(grants) });
    },
    events: WEB_DATA_EVENTS,
    request,
    fetch: (url, callbackGas, fee) => request(WEB_DATA_KIND_FETCH, url, callbackGas, fee),
    search: (query, callbackGas, fee) => request(WEB_DATA_KIND_SEARCH, query, callbackGas, fee),
    api: (built, callbackGas, fee) => retarget(xwebApiRequestCall(built.payload, callbackGas, fee), target),
    buildApiRequest: (call, attestors, options) => buildXWebApiRequest(call, attestors, options),
    refund: (requestId) => ({ to: target, data: encodeAbiCall('refund', ['uint64'], [requestId]), value: 0n }),
    fee,
    attestors: async () => decodeXWebAttestors(await ethCall(provider, target, xwebGetAttestorsCallData())),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeModuleEvent(WEB_DATA_EVENTS, target, log),
  };
}
