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
} from '@sidiora/layerx-sdk';

import {
  ModuleError,
  decodeAbiUint,
  decodeModuleEvent,
  ethCall,
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
  attestors(): Promise<XWebAttestorSet>;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): DecodedModuleEvent;
}

const REQUEST_SELECTOR = abiSelector('request(uint8,bytes,uint64)');

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
  return {
    address: target,
    events: WEB_DATA_EVENTS,
    request,
    fetch: (url, callbackGas, fee) => request(WEB_DATA_KIND_FETCH, url, callbackGas, fee),
    search: (query, callbackGas, fee) => request(WEB_DATA_KIND_SEARCH, query, callbackGas, fee),
    api: (built, callbackGas, fee) => retarget(xwebApiRequestCall(built.payload, callbackGas, fee), target),
    buildApiRequest: (call, attestors, options) => buildXWebApiRequest(call, attestors, options),
    refund: (requestId) => ({ to: target, data: encodeAbiCall('refund', ['uint64'], [requestId]), value: 0n }),
    fee: async () => decodeAbiUint(await ethCall(provider, target, abiSelector('fee()'))),
    attestors: async () => decodeXWebAttestors(await ethCall(provider, target, xwebGetAttestorsCallData())),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeModuleEvent(WEB_DATA_EVENTS, target, log),
  };
}
