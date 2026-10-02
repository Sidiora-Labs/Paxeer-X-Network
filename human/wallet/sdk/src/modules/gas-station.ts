import {
  SIDIORA_DECIMALS,
  GAS_STATION_QUOTE_URL,
  SIDIORA_TOKEN,
  abiSelector,
  assembleEip7702Authorization,
  eip7702AuthorizationDigest,
  submitSponsoredGasBatch,
  requestGasQuote,
  sponsoredBatchCall,
  type GasQuoteRequest,
  type GasRefusal,
  type GasResult,
  type GasStationConfig,
  type SignedGasQuote,
  type SponsoredBatch,
} from '@sidiora/layerx-sdk/browser';

import { sponsoredBatchDigest, wireSponsoredBatch } from '../provider.js';
import type {
  Hex,
  SponsoredBatchConstruction,
  SponsoredSubmitRequest,
  SponsoredSubmitResponse,
  WireSponsoredBatch,
} from '../types.js';
import {
  ModuleError,
  decodeAbiUint,
  ethQuantity,
  moduleAddress,
  type ModuleProvider,
  type ModuleTransaction,
} from './index.js';

export const SPONSORED_SUBMIT_PATH = '/v1/wallet/sponsored/submit';

const RATE = /^(0|[1-9][0-9]*)(?:\.([0-9]{1,18}))?$/u;
const RATE_SCALE = 10n ** 18n;
const SIGNATURE = /^0x[0-9a-fA-F]{130}$/u;
const HASH = /^0x[0-9a-fA-F]{64}$/u;

export class GasStationError extends Error {
  readonly refusal: GasRefusal;

  constructor(refusal: GasRefusal) {
    super(`${refusal.code}: ${refusal.field}`);
    this.name = 'GasStationError';
    this.refusal = refusal;
  }
}

export interface GasStationOptions {
  readonly chainId: bigint;
  readonly sponsor: string;
  readonly paymaster: string;
  readonly quoteUrl?: string;
  readonly gatewayUrl?: string;
  readonly accessToken?: () => Promise<string | null> | string | null;
  readonly fetch?: typeof fetch;
}

export interface GasBudget {
  readonly gasLimit: bigint;
  readonly rate: string;
  readonly gasPrice?: bigint;
}

export interface GasBudgetQuote {
  readonly gasLimit: bigint;
  readonly gasPrice: bigint;
  readonly gasCost: bigint;
  readonly rate: string;
  readonly tokenAmount: bigint;
  readonly token: string;
  readonly symbol: 'SID';
  readonly decimals: number;
}

export interface SponsoredSubmitOptions {
  readonly now?: bigint;
  readonly signal?: AbortSignal;
}

export interface GasStationModule {
  readonly config: GasStationConfig;
  quote(budget: GasBudget): Promise<GasBudgetQuote>;
  requestQuote(request: GasQuoteRequest, options?: { readonly signal?: AbortSignal; readonly now?: bigint }): Promise<SignedGasQuote>;
  batchNonce(account: string): Promise<bigint>;
  construction(batch: SponsoredBatch): WireSponsoredBatch;
  digest(batch: SponsoredBatch): Hex;
  sign(batch: SponsoredBatch): Promise<string>;
  executeCall(batch: SponsoredBatch, accountSignature: string, relayerSignature: string, now?: bigint): ModuleTransaction;
  submitRequest(batch: SponsoredBatch, accountSignature: string, relayerSignature: string, now?: bigint): SponsoredSubmitRequest;
  submit(batch: SponsoredBatch, relayerSignature: string, options?: SponsoredSubmitOptions): Promise<string>;
  submitFirstUse(batch: SponsoredBatch, relayerSignature: string, options?: SponsoredSubmitOptions): Promise<string>;
}

function unwrap<T>(result: GasResult<T>): T {
  if (!result.ok) {
    throw new GasStationError(result.refusal);
  }
  return result.value;
}

function rateScaled(rate: string): bigint {
  const match = typeof rate === 'string' ? RATE.exec(rate) : null;
  if (match === null) {
    throw new ModuleError('invalid_value', 'rate');
  }
  const whole = BigInt(match[1] ?? '0');
  const fraction = BigInt((match[2] ?? '').padEnd(18, '0'));
  const scaled = whole * RATE_SCALE + fraction;
  if (scaled === 0n) {
    throw new ModuleError('invalid_value', 'rate');
  }
  return scaled;
}

export function feeTokenAmount(gasCost: bigint, rate: string): bigint {
  if (typeof gasCost !== 'bigint' || gasCost < 0n) {
    throw new ModuleError('invalid_value', 'gasCost');
  }
  const numerator = gasCost * rateScaled(rate);
  const denominator = RATE_SCALE * RATE_SCALE;
  const quotient = numerator / denominator;
  const amount = numerator % denominator === 0n ? quotient : quotient + 1n;
  if (amount >= 1n << 256n) {
    throw new ModuleError('invalid_value', 'gasCost');
  }
  return amount;
}

function decimal(value: bigint): string {
  return value.toString(10);
}

function hexAddress(value: string, field: string): `0x${string}` {
  return moduleAddress(value, field) as `0x${string}`;
}

function hexData(value: string, field: string): `0x${string}` {
  if (typeof value !== 'string' || !/^0x(?:[0-9a-fA-F]{2})*$/u.test(value)) {
    throw new ModuleError('invalid_value', field);
  }
  return value.toLowerCase() as `0x${string}`;
}

function signature(value: unknown, field: string): `0x${string}` {
  if (typeof value !== 'string' || !SIGNATURE.test(value)) {
    throw new ModuleError('invalid_answer', field);
  }
  return value.toLowerCase() as `0x${string}`;
}

export function gasStation(provider: ModuleProvider, options: GasStationOptions): GasStationModule {
  const config: GasStationConfig = {
    quoteUrl: options.quoteUrl ?? GAS_STATION_QUOTE_URL,
    chainId: options.chainId,
    sponsor: moduleAddress(options.sponsor, 'sponsor'),
    token: SIDIORA_TOKEN,
    decimals: SIDIORA_DECIMALS,
    paymaster: moduleAddress(options.paymaster, 'paymaster'),
  };
  const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);

  const batchNonce = async (account: string): Promise<bigint> => {
    const target = moduleAddress(account, 'account');
    const code = await provider.request({ method: 'eth_getCode', params: [target, 'pending'] });
    if (typeof code !== 'string' || !/^0x(?:[0-9a-fA-F]{2})*$/u.test(code)) {
      throw new ModuleError('invalid_answer', 'eth_getCode');
    }
    if (code === '0x') return 0n;
    if (code.toLowerCase() !== `0xef0100${config.paymaster.slice(2).toLowerCase()}`) {
      throw new GasStationError({ code: 'refused', field: 'delegation' });
    }
    const nonce = await provider.request({ method: 'eth_call', params: [{ to: target, data: abiSelector('nonce()') }, 'pending'] });
    if (typeof nonce !== 'string') throw new ModuleError('invalid_answer', 'nonce');
    return decodeAbiUint(nonce);
  };

  const construction = (batch: SponsoredBatch): WireSponsoredBatch => {
    if (batch.calls.length === 0) {
      throw new ModuleError('invalid_value', 'calls');
    }
    if (batch.chainId !== config.chainId) {
      throw new GasStationError({ code: 'chain_mismatch', field: 'chainId' });
    }
    const fields: SponsoredBatchConstruction = {
      kind: 'sponsored_batch',
      chainId: decimal(batch.chainId),
      account: hexAddress(batch.account, 'account'),
      nonce: decimal(batch.nonce),
      calls: batch.calls.map((call, index) => ({
        to: hexAddress(call.to, `calls[${index}].to`),
        value: decimal(call.value),
        data: hexData(call.data, `calls[${index}].data`),
      })),
      quote: {
        sponsor: hexAddress(batch.quote.sponsor, 'quote.sponsor'),
        token: hexAddress(batch.quote.token, 'quote.token'),
        maxTokenAmount: decimal(batch.quote.maxTokenAmount),
        tokenAmount: decimal(batch.quote.tokenAmount),
        deadline: decimal(batch.quote.deadline),
        quoteNonce: decimal(batch.quote.quoteNonce),
        gasCost: decimal(batch.quote.gasCost),
      },
    };
    return wireSponsoredBatch(fields);
  };

  const digest = (batch: SponsoredBatch): Hex => sponsoredBatchDigest(construction(batch));

  const sign = async (batch: SponsoredBatch): Promise<string> => {
    const fields = construction(batch);
    const answer = await provider.request({
      method: 'eth_sign',
      params: [fields.account, sponsoredBatchDigest(fields), fields],
    });
    return signature(answer, 'eth_sign');
  };

  const executeCall = (batch: SponsoredBatch, accountSignature: string, relayerSignature: string, now?: bigint): ModuleTransaction =>
    unwrap(
      now === undefined
        ? sponsoredBatchCall(config, batch, accountSignature, relayerSignature)
        : sponsoredBatchCall(config, batch, accountSignature, relayerSignature, now),
    );

  const submitRequest = (
    batch: SponsoredBatch,
    accountSignature: string,
    relayerSignature: string,
    now?: bigint,
  ): SponsoredSubmitRequest => {
    const call = executeCall(batch, accountSignature, relayerSignature, now);
    return {
      chain_id: decimal(config.chainId),
      account: hexAddress(batch.account, 'account'),
      to: hexAddress(call.to, 'to'),
      data: hexData(call.data, 'data'),
      value: decimal(call.value),
      construction: construction(batch),
      account_signature: signature(accountSignature, 'accountSignature'),
      relayer_signature: signature(relayerSignature, 'relayerSignature'),
    };
  };

  return {
    config,
    quote: async (budget) => {
      if (typeof budget.gasLimit !== 'bigint' || budget.gasLimit <= 0n) {
        throw new ModuleError('invalid_value', 'gasLimit');
      }
      const gasPrice = budget.gasPrice ?? (await ethQuantity(provider, 'eth_gasPrice'));
      if (gasPrice <= 0n) {
        throw new ModuleError('invalid_value', 'gasPrice');
      }
      const gasCost = budget.gasLimit * gasPrice;
      return {
        gasLimit: budget.gasLimit,
        gasPrice,
        gasCost,
        rate: budget.rate,
        tokenAmount: feeTokenAmount(gasCost, budget.rate),
        token: config.token,
        symbol: 'SID',
        decimals: config.decimals,
      };
    },
    requestQuote: async (request, quoteOptions = {}) => unwrap(await requestGasQuote(config, request, quoteOptions)),
    batchNonce,
    construction,
    digest,
    sign,
    executeCall,
    submitRequest,
    submitFirstUse: async (batch, relayerSignature, submitOptions = {}) => {
      if (batch.nonce !== await batchNonce(batch.account)) throw new GasStationError({ code: 'refused', field: 'nonce' });
      const account = hexAddress(batch.account, 'account');
      const pending = await provider.request({ method: 'eth_getTransactionCount', params: [account, 'pending'] });
      if (typeof pending !== 'string' || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/u.test(pending)) {
        throw new ModuleError('invalid_answer', 'eth_getTransactionCount');
      }
      const nonce = BigInt(pending);
      const authorizationFields = { chainId: config.chainId, address: config.paymaster, nonce };
      const authorizationDigest = unwrap(eip7702AuthorizationDigest(authorizationFields));
      const accountSignature = await sign(batch);
      executeCall(batch, accountSignature, relayerSignature, submitOptions.now);
      const answer = await provider.request({ method: 'eth_sign', params: [account, authorizationDigest, {
        kind: 'eip7702_authorization', chainId: decimal(config.chainId), address: hexAddress(config.paymaster, 'paymaster'), nonce: decimal(nonce),
      }] });
      const authorization = unwrap(assembleEip7702Authorization(config, account, nonce, signature(answer, 'authorization')));
      return unwrap(await submitSponsoredGasBatch(config, batch, accountSignature, relayerSignature, authorization, {
        ...submitOptions, fetch: fetchImpl,
      }));
    },
    submit: async (batch, relayerSignature, submitOptions = {}) => {
      if (options.gatewayUrl === undefined || options.gatewayUrl === '') {
        throw new ModuleError('unavailable', 'gatewayUrl');
      }
      const accountSignature = await sign(batch);
      const body = submitRequest(batch, accountSignature, relayerSignature, submitOptions.now);
      const headers: Record<string, string> = { 'content-type': 'application/json' };
      const token = options.accessToken === undefined ? null : await options.accessToken();
      if (token !== null && token !== '') {
        headers.Authorization = `Bearer ${token}`;
      }
      let response: Response;
      try {
        response = await fetchImpl(`${options.gatewayUrl.replace(/\/$/u, '')}${SPONSORED_SUBMIT_PATH}`, {
          method: 'POST',
          headers,
          body: JSON.stringify(body),
          ...(submitOptions.signal === undefined ? {} : { signal: submitOptions.signal }),
        });
      } catch {
        throw new ModuleError('unavailable', 'gatewayUrl');
      }
      if (!response.ok) {
        throw new ModuleError(response.status >= 500 ? 'unavailable' : 'refused', `status ${response.status}`);
      }
      let payload: unknown;
      try {
        payload = await response.json();
      } catch {
        throw new ModuleError('invalid_answer', 'sponsored submit');
      }
      const hash = (payload as Partial<SponsoredSubmitResponse> | null)?.tx_hash;
      if (typeof hash !== 'string' || !HASH.test(hash)) {
        throw new ModuleError('invalid_answer', 'tx_hash');
      }
      return hash;
    },
  };
}
