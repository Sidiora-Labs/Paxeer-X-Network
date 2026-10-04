import {
  SIDIORA_DECIMALS,
  gasStationQuoteUrl,
  SIDIORA_TOKEN,
  assembleEip7702Authorization,
  eip7702AuthorizationDigest,
  requestGasQuote,
  sponsoredBatchCall,
  sponsoredConsent,
  discoverSponsoredNonce,
  readNativeFeePreference,
  type NativeFeePreference,
  type SponsoredConsent,
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
  WireSponsoredBatch,
} from '../types.js';
import {
  ModuleError,
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
  readonly submissionStore?: GasSubmissionStore;
}

export interface GasSubmissionStore {
  getItem(key: string): string | null | Promise<string | null>;
  setItem(key: string, value: string): void | Promise<void>;
}
export interface SponsoredAdapterRequest extends SponsoredSubmitRequest {
  readonly quote_decimals: 6;
  readonly authorization: { readonly chainId: string; readonly address: `0x${string}`; readonly nonce: string;
    readonly yParity: 0 | 1; readonly r: `0x${string}`; readonly s: `0x${string}` };
}
export interface SponsoredSubmissionEvidence {
  readonly tx_hash: `0x${string}` | null;
  readonly status: 'pending' | 'confirmed' | 'reverted' | 'cancelled';
  readonly station: Readonly<Record<string, unknown>> | null;
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
  readonly confirm?: (consent: SponsoredConsent) => boolean | Promise<boolean>;
}

export interface GasStationModule {
  readonly config: GasStationConfig;
  quote(budget: GasBudget): Promise<GasBudgetQuote>;
  requestQuote(request: GasQuoteRequest, options?: { readonly signal?: AbortSignal; readonly now?: bigint }): Promise<SignedGasQuote>;
  batchNonce(account: string): Promise<bigint>;
  nativeFeePreference(account: string, restUrl: string, signal?: AbortSignal): Promise<NativeFeePreference>;
  construction(batch: SponsoredBatch): WireSponsoredBatch;
  digest(batch: SponsoredBatch): Hex;
  sign(batch: SponsoredBatch): Promise<string>;
  executeCall(batch: SponsoredBatch, accountSignature: string, relayerSignature: string, now?: bigint): ModuleTransaction;
  submitRequest(batch: SponsoredBatch, accountSignature: string, relayerSignature: string, now?: bigint): SponsoredSubmitRequest;
  adapterRequest(batch: SponsoredBatch, accountSignature: string, relayerSignature: string, authorization: SponsoredAdapterRequest['authorization'], now?: bigint): SponsoredAdapterRequest;
  submit(batch: SponsoredBatch, relayerSignature: string, options?: SponsoredSubmitOptions): Promise<string>;
  submitFirstUse(batch: SponsoredBatch, relayerSignature: string, options?: SponsoredSubmitOptions): Promise<string>;
  status(batch: SponsoredBatch, relayerSignature: string, signal?: AbortSignal): Promise<SponsoredSubmissionEvidence>;
  resume(batch: SponsoredBatch, relayerSignature: string, signal?: AbortSignal): Promise<SponsoredSubmissionEvidence>;
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
  const config: GasStationConfig = Object.freeze({
    quoteUrl: gasStationQuoteUrl(options.quoteUrl, options.gatewayUrl),
    chainId: options.chainId,
    sponsor: moduleAddress(options.sponsor, 'sponsor'),
    token: SIDIORA_TOKEN,
    decimals: SIDIORA_DECIMALS,
    paymaster: moduleAddress(options.paymaster, 'paymaster'),
  });
  const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);

  const batchNonce = async (account: string): Promise<bigint> =>
    unwrap(await discoverSponsoredNonce(config, account, args => provider.request(args)));

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

  const adapterRequest = (batch:SponsoredBatch,accountSignature:string,relayerSignature:string,authorization:SponsoredAdapterRequest['authorization'],now?:bigint):SponsoredAdapterRequest => {
    if(Object.keys(authorization).some(key=>!['chainId','address','nonce','yParity','r','s'].includes(key)))throw new ModuleError('invalid_value','authorization');
    if(authorization.chainId!==decimal(config.chainId)||authorization.address.toLowerCase()!==config.paymaster.toLowerCase()||
      !/^(0|[1-9][0-9]*)$/u.test(authorization.nonce)||(authorization.yParity!==0&&authorization.yParity!==1)||!HASH.test(authorization.r)||!HASH.test(authorization.s))throw new ModuleError('invalid_value','authorization');
    const signatureBytes=`${authorization.r}${authorization.s.slice(2)}${(authorization.yParity+27).toString(16)}`;
    unwrap(assembleEip7702Authorization(config,hexAddress(batch.account,'account'),BigInt(authorization.nonce),signatureBytes));
    return {...submitRequest(batch,accountSignature,relayerSignature,now),quote_decimals:6,authorization:{...authorization,address:hexAddress(authorization.address,'authorization.address')}};
  };
  const store = (): GasSubmissionStore => {
    if (options.submissionStore) return options.submissionStore;
    try { if (typeof globalThis.localStorage !== 'undefined') return globalThis.localStorage; } catch {}
    throw new ModuleError('unavailable', 'submissionStore');
  };
  const identityKey = (batch: SponsoredBatch): string =>
    `paxeer:sponsored:v1:${config.chainId}:${hexAddress(batch.account,'account')}:${hexAddress(batch.quote.sponsor,'sponsor')}:${decimal(batch.quote.quoteNonce)}`;
  const headers = async (): Promise<Record<string,string>> => {
    if (!options.gatewayUrl) throw new ModuleError('unavailable','gatewayUrl');
    const token = options.accessToken === undefined ? null : await options.accessToken();
    if (!token) throw new ModuleError('refused','accessToken');
    const accounts = await provider.request({ method:'eth_accounts',params:[] });
    const chain = await provider.request({ method:'eth_chainId',params:[] });
    if (!Array.isArray(accounts) || accounts.length !== 1 || typeof accounts[0] !== 'string' ||
      typeof chain !== 'string' || !/^0x[0-9a-fA-F]+$/u.test(chain) || BigInt(chain)!==config.chainId) throw new ModuleError('refused','session');
    return { 'content-type':'application/json',Authorization:`Bearer ${token}` };
  };
  const post = async (path: string, body: unknown, signal?: AbortSignal): Promise<unknown> => {
    const authorization = await headers();
    const account = (body as {account?:unknown}).account;
    const accounts = await provider.request({method:'eth_accounts',params:[]});
    if (!Array.isArray(accounts)||typeof accounts[0]!=='string'||typeof account!=='string'||accounts[0].toLowerCase()!==account.toLowerCase()) throw new ModuleError('refused','account');
    let response:Response;
    try { response=await fetchImpl(`${options.gatewayUrl!.replace(/\/$/u,'')}${path}`,{
      method:'POST',headers:authorization,body:JSON.stringify(body),redirect:'error',credentials:'omit',mode:'cors',
      signal:signal ? AbortSignal.any([signal,AbortSignal.timeout(30_000)]) : AbortSignal.timeout(30_000),
    }); } catch {
      if (signal?.aborted) throw new GasStationError({code:'cancelled',field:'submission_status_unknown'});
      throw new ModuleError('unavailable','submission_status_unknown');
    }
    if (!response.ok) throw new ModuleError(response.status>=500?'unavailable':'refused',`status ${response.status}`);
    try { return await response.json(); } catch { throw new ModuleError('invalid_answer','sponsored submit'); }
  };
  const evidence = (input: unknown): SponsoredSubmissionEvidence => {
    if (typeof input!=='object'||input===null||Array.isArray(input)) throw new ModuleError('invalid_answer','status');
    const out=input as Record<string,unknown>;
    if ((out.tx_hash!==null&&(typeof out.tx_hash!=='string'||!HASH.test(out.tx_hash)))||
      !['pending','confirmed','reverted','cancelled'].includes(String(out.status))) throw new ModuleError('invalid_answer','status');
    if(out.station===null){if(out.status!=='pending'||typeof out.tx_hash!=='string'||!HASH.test(out.tx_hash))throw new ModuleError('invalid_answer','status');return Object.freeze(out as unknown as SponsoredSubmissionEvidence);}
    if(typeof out.station!=='object'||Array.isArray(out.station))throw new ModuleError('invalid_answer','station');
    const station=out.station as Record<string,unknown>;const completion=station.completion as Record<string,unknown>|null;
    const canonicalUint=(value:unknown):boolean=>typeof value==='string'&&/^(0|[1-9][0-9]*)$/u.test(value)&&BigInt(value)<(1n<<256n);
    const transaction=(value:unknown):boolean=>value===null||(typeof value==='object'&&value!==null&&!Array.isArray(value)&&
      canonicalUint((value as Record<string,unknown>).sponsorNonce)&&typeof (value as Record<string,unknown>).transactionHash==='string'&&HASH.test((value as {transactionHash:string}).transactionHash));
    if(!['quoted','pending','replacing','completed'].includes(String(station.state))||!canonicalUint(station.deadline)||!transaction(station.submission)||!transaction(station.replacement)||
      (station.state==='completed')!==(completion!==null)||((station.state==='pending'||station.state==='replacing')&&station.submission===null)||
      (station.state==='replacing'&&station.replacement===null))throw new ModuleError('invalid_answer','station');
    if(completion!==null&&(typeof completion!=='object'||Array.isArray(completion)||!['consumed','included','reverted','cancelled'].includes(String(completion.outcome))))throw new ModuleError('invalid_answer','completion');
    if (out.status!=='pending') {
      const expected=out.status==='confirmed'?'included':out.status;
      if (station.state!=='completed'||!completion||completion.outcome!==expected||completion.transactionHash!==out.tx_hash||
        (out.status==='confirmed'&&(!canonicalUint(completion.blockNumber)||!canonicalUint(completion.sidCollected)||!canonicalUint(completion.paxSpent)))) throw new ModuleError('invalid_answer','completion');
    }
    return Object.freeze(out as unknown as SponsoredSubmissionEvidence);
  };
  const submissionStatus = async (batch:SponsoredBatch,relayerSignature:string,signal?:AbortSignal):Promise<SponsoredSubmissionEvidence> =>
    evidence(await post('/v1/wallet/sponsored/status',{account:hexAddress(batch.account,'account'),sponsor:hexAddress(batch.quote.sponsor,'sponsor'),
      quoteNonce:decimal(batch.quote.quoteNonce),relayerSignature:signature(relayerSignature,'relayerSignature')},signal));
  const retained = async (batch:SponsoredBatch,relayerSignature:string):Promise<SponsoredAdapterRequest|null> => {
    const raw=await store().getItem(identityKey(batch));if(raw===null)return null;
    let record:unknown;try{record=JSON.parse(raw);}catch{throw new ModuleError('invalid_answer','retained_submission');}
    if(typeof record!=='object'||record===null||Array.isArray(record))throw new ModuleError('invalid_answer','retained_submission');
    const saved=record as SponsoredAdapterRequest;
    if(JSON.stringify(saved.construction)!==JSON.stringify(construction(batch))||saved.relayer_signature!==signature(relayerSignature,'relayerSignature')||
      saved.account!==hexAddress(batch.account,'account')||saved.chain_id!==decimal(config.chainId)||saved.quote_decimals!==6||!saved.authorization)throw new ModuleError('refused','retained_submission_conflict');
    return saved;
  };
  const resumeSubmission = async (batch:SponsoredBatch,relayerSignature:string,signal?:AbortSignal):Promise<SponsoredSubmissionEvidence> => {
    const saved=await retained(batch,relayerSignature);if(!saved)throw new ModuleError('unavailable','retained_submission');
    const current=await submissionStatus(batch,relayerSignature,signal);
    if(current.tx_hash!==null||current.station?.state!=='quoted')return current;
    return evidence(await post(SPONSORED_SUBMIT_PATH,saved,signal));
  };
  const submitApproved = async (input:SponsoredBatch,relayerSignature:string,submitOptions:SponsoredSubmitOptions={}):Promise<string> => {
    const batch:SponsoredBatch=Object.freeze({...input,quote:Object.freeze({...input.quote}),calls:Object.freeze(input.calls.map(call=>Object.freeze({...call})))});
    const persistence=store();
    const prior=await retained(batch,relayerSignature);
    if(prior){const resumed=await resumeSubmission(batch,relayerSignature,submitOptions.signal);if(!resumed.tx_hash)throw new ModuleError('unavailable','submission_pending');return resumed.tx_hash;}
    const consent=unwrap(sponsoredConsent(config,batch,submitOptions.now));
    if(submitOptions.confirm===undefined||!await submitOptions.confirm(consent))throw new GasStationError({code:'refused',field:'consent'});
    if(unwrap(sponsoredConsent(config,batch,submitOptions.now)).batchDigest!==consent.batchDigest)throw new GasStationError({code:'refused',field:'consent_changed'});
    if(batch.nonce!==await batchNonce(batch.account))throw new GasStationError({code:'refused',field:'nonce'});
    const account=hexAddress(batch.account,'account');
    const pending=await provider.request({method:'eth_getTransactionCount',params:[account,'pending']});
    if(typeof pending!=='string'||!/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/u.test(pending))throw new ModuleError('invalid_answer','eth_getTransactionCount');
    const nonce=BigInt(pending);const authorizationFields={chainId:config.chainId,address:config.paymaster,nonce};
    const authorizationDigest=unwrap(eip7702AuthorizationDigest(authorizationFields));
    const accountSignature=await sign(batch);
    executeCall(batch,accountSignature,relayerSignature,submitOptions.now);
    const answer=await provider.request({method:'eth_sign',params:[account,authorizationDigest,{kind:'eip7702_authorization',chainId:decimal(config.chainId),address:hexAddress(config.paymaster,'paymaster'),nonce:decimal(nonce)}]});
    const authorization=unwrap(assembleEip7702Authorization(config,account,nonce,signature(answer,'authorization')));
    const body=adapterRequest(batch,accountSignature,relayerSignature,
      {chainId:decimal(authorization.chainId),address:hexAddress(authorization.address,'authorization.address'),nonce:decimal(authorization.nonce),yParity:authorization.yParity as 0|1,r:hexData(authorization.r,'r'),s:hexData(authorization.s,'s')},submitOptions.now);
    await persistence.setItem(identityKey(batch),JSON.stringify(body));
    const response=evidence(await post(SPONSORED_SUBMIT_PATH,body,submitOptions.signal));
    if(response.tx_hash===null)throw new ModuleError('unavailable','submission_pending');
    return response.tx_hash;
  };

  const submissions = new Map<string,{identity:string;promise:Promise<string>}>();
  const serializedSubmit = (batch:SponsoredBatch,relayerSignature:string,submitOptions:SponsoredSubmitOptions={}):Promise<string> => {
    const key=identityKey(batch);const identity=JSON.stringify(construction(batch))+signature(relayerSignature,'relayerSignature');
    const active=submissions.get(key);
    if(active){if(active.identity!==identity)return Promise.reject(new ModuleError('refused','submission_identity_conflict'));return active.promise;}
    const promise=submitApproved(batch,relayerSignature,submitOptions).finally(()=>submissions.delete(key));
    submissions.set(key,{identity,promise});return promise;
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
    requestQuote: async (request, quoteOptions = {}) => unwrap(await requestGasQuote(config, request, { ...quoteOptions, fetch: fetchImpl })),
    batchNonce,
    nativeFeePreference: (account, restUrl, signal) => readNativeFeePreference(account, { restUrl, request: args => provider.request(args), fetch: fetchImpl, ...(signal === undefined ? {} : { signal }) }),
    construction,
    digest,
    sign,
    executeCall,
    submitRequest,
    adapterRequest,
    submitFirstUse: serializedSubmit,
    submit: serializedSubmit,
    status: submissionStatus,
    resume: resumeSubmission,
  };
}
