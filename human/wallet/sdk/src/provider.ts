import { concat, encodeAbiParameters, hashMessage, keccak256, toHex, toRlp } from 'viem';
import {
  ChainDisconnectedError,
  DisconnectedError,
  InvalidParamsError,
  PROVIDER_ERROR_CODES,
  ProviderRpcError,
  RpcResponseError,
  UnauthorizedError,
  UnsupportedMethodError,
  UserRejectedRequestError,
  gatewayRefusal,
} from './errors.js';
import type {
  ChainInfo,
  DigestConstruction,
  Eip1193Provider,
  Hex,
  ProviderEvent,
  ProviderListener,
  PublicWallet,
  RequestArguments,
  SendTxResponse,
  SignCustodyResponse,
  SignDigestResponse,
  SignMessageResponse,
  SignTypedDataResponse,
  SponsoredBatchConstruction,
  TypedDataPayload,
  UintInput,
  WireDigestConstruction,
  WireEip7702Authorization,
  WireSponsoredBatch,
} from './types.js';

export const PAXEER_CHAIN_ID = 125;

export type TokenSupplier = () => string | null | undefined | Promise<string | null | undefined>;

export interface ConfirmRequest {
  method: string;
  params: readonly unknown[];
}

export interface PaxeerProviderConfig {
  gatewayUrl: string;
  rpcUrl: string;
  token: TokenSupplier;
  chainId?: number;
  confirm?: (request: ConfirmRequest) => boolean | Promise<boolean>;
  fetch?: typeof fetch;
}

const UNSUPPORTED_METHODS = new Set([
  'eth_signTypedData',
  'eth_signTypedData_v1',
  'eth_signTypedData_v3',
  'eth_signTransaction',
  'wallet_addEthereumChain',
]);

const SIGNING_METHODS = new Set([
  'eth_sendTransaction',
  'eth_signTypedData_v4',
  'personal_sign',
  'eth_sign',
  'paxeer_signCustody',
]);

const QUOTE_TYPEHASH = keccak256(
  toHex(
    'Quote(uint256 chainId,address account,address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost)',
  ),
);
const BATCH_TYPEHASH = keccak256(toHex('SponsoredBatch(uint256 nonce,bytes32 callsHash,bytes32 quoteDigest)'));

const UINT256_MAX = (1n << 256n) - 1n;
const UINT64_MAX = (1n << 64n) - 1n;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const BYTES = /^0x(?:[0-9a-fA-F]{2})*$/;
const QUANTITY = /^0x[0-9a-fA-F]+$/;

export class BindingRefusedError extends ProviderRpcError {
  constructor(
    message: string,
    public readonly boundDid: string | null,
  ) {
    super(PROVIDER_ERROR_CODES.internal, message, { reason: 'binding_refused', bound_did: boundDid });
    this.name = 'BindingRefusedError';
  }
}

export class PaxeerProvider implements Eip1193Provider {
  readonly isPaxeer = true;
  private readonly gatewayUrl: string;
  private readonly rpcUrl: string;
  private readonly token: TokenSupplier;
  private readonly confirm?: PaxeerProviderConfig['confirm'];
  private readonly fetchImpl: typeof fetch;
  private readonly listeners = new Map<ProviderEvent, Set<ProviderListener>>();
  private accounts: Hex[] = [];
  private chainId: number;
  private connected = false;
  private rpcId = 0;

  constructor(config: PaxeerProviderConfig) {
    if (!config.gatewayUrl) throw new Error('PaxeerProvider: gatewayUrl required');
    if (!config.rpcUrl) throw new Error('PaxeerProvider: rpcUrl required');
    if (typeof config.token !== 'function') throw new Error('PaxeerProvider: token supplier required');
    this.gatewayUrl = config.gatewayUrl.replace(/\/$/, '');
    this.rpcUrl = config.rpcUrl;
    this.token = config.token;
    this.confirm = config.confirm;
    this.chainId = config.chainId ?? PAXEER_CHAIN_ID;
    this.fetchImpl = config.fetch ?? globalThis.fetch.bind(globalThis);
  }

  on(event: ProviderEvent, listener: ProviderListener): this {
    let set = this.listeners.get(event);
    if (!set) {
      set = new Set();
      this.listeners.set(event, set);
    }
    set.add(listener);
    return this;
  }

  removeListener(event: ProviderEvent, listener: ProviderListener): this {
    this.listeners.get(event)?.delete(listener);
    return this;
  }

  isConnected(): boolean {
    return this.connected;
  }

  disconnect(): void {
    const hadAccounts = this.accounts.length > 0;
    this.accounts = [];
    this.connected = false;
    if (hadAccounts) this.emit('accountsChanged', []);
    this.emit('disconnect', new DisconnectedError('the embedded wallet was disconnected'));
  }

  async request(args: RequestArguments): Promise<unknown> {
    if (!args || typeof args !== 'object' || typeof args.method !== 'string' || args.method.length === 0) {
      throw new InvalidParamsError('method', 'request requires a method name');
    }
    const params = positional(args.params);
    const method = args.method;
    if (UNSUPPORTED_METHODS.has(method)) throw new UnsupportedMethodError(method);
    if (SIGNING_METHODS.has(method) && this.confirm) {
      const approved = await this.confirm({ method, params });
      if (!approved) throw new UserRejectedRequestError();
    }
    switch (method) {
      case 'eth_requestAccounts':
        return this.requestAccounts();
      case 'eth_accounts':
        return [...this.accounts];
      case 'eth_chainId':
        return toQuantity(BigInt(this.chainId));
      case 'net_version':
        return String(this.chainId);
      case 'wallet_switchEthereumChain':
        return this.switchChain(params);
      case 'eth_sendTransaction':
        return this.sendTransaction(params);
      case 'eth_signTypedData_v4':
        return this.signTypedData(params);
      case 'personal_sign':
        return this.personalSign(params);
      case 'eth_sign':
        return this.ethSign(params);
      case 'paxeer_signCustody':
        return this.signCustody(params);
      default:
        return this.proxy(method, args.params);
    }
  }

  private async requestAccounts(): Promise<Hex[]> {
    let me: { wallet: PublicWallet; chain: ChainInfo } | null;
    try {
      me = await this.gateway<{ wallet: PublicWallet; chain: ChainInfo }>('GET', '/v1/wallet/me');
    } catch (error) {
      if (!(error instanceof ProviderRpcError) || !isNoWallet(error)) throw error;
      me = null;
    }
    let address: Hex;
    let chainId = this.chainId;
    if (me) {
      address = me.wallet.address;
      chainId = me.chain.id;
      if (needsBinding(me.wallet)) await this.completeBinding();
    } else {
      const provisioned = await this.gateway<{ wallet: PublicWallet }>('POST', '/v1/wallet/provision');
      address = provisioned.wallet.address;
      chainId = provisioned.wallet.chain_id;
    }
    if (!ADDRESS.test(address)) throw new InvalidParamsError('wallet.address', 'the gateway returned an invalid address');
    const wasConnected = this.connected;
    const accountsChanged = this.accounts.length !== 1 || this.accounts[0]?.toLowerCase() !== address.toLowerCase();
    const chainChanged = chainId !== this.chainId;
    this.accounts = [address];
    this.chainId = chainId;
    this.connected = true;
    if (!wasConnected) this.emit('connect', { chainId: toQuantity(BigInt(chainId)) });
    if (chainChanged) this.emit('chainChanged', toQuantity(BigInt(chainId)));
    if (accountsChanged) this.emit('accountsChanged', [...this.accounts]);
    return [...this.accounts];
  }

  private async completeBinding(): Promise<void> {
    try {
      await this.gateway<{ wallet: PublicWallet }>('POST', '/v1/wallet/provision');
    } catch (error) {
      if (error instanceof ProviderRpcError && isBindingRefused(error)) {
        const body = (error.data as { body: Record<string, unknown> }).body;
        throw new BindingRefusedError(
          typeof body.message === 'string' ? body.message : 'the account binding was refused',
          typeof body.bound_did === 'string' ? body.bound_did : null,
        );
      }
      throw error;
    }
  }

  private switchChain(params: readonly unknown[]): null {
    const target = params[0];
    if (!isRecord(target) || typeof target.chainId !== 'string' || !QUANTITY.test(target.chainId)) {
      throw new InvalidParamsError('chainId', 'wallet_switchEthereumChain requires a hex chainId');
    }
    const requested = Number(BigInt(target.chainId));
    if (requested !== this.chainId) throw new ChainDisconnectedError(requested, this.chainId);
    return null;
  }

  private async sendTransaction(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount();
    const tx = params[0];
    if (!isRecord(tx)) throw new InvalidParamsError('tx', 'eth_sendTransaction requires a transaction object');
    if (tx.from !== undefined) this.requireSameAccount(tx.from, account, 'from');
    const wire: Record<string, unknown> = {};
    if (tx.to !== undefined && tx.to !== null) wire.to = addressField(tx.to, 'to');
    if (tx.data !== undefined) wire.data = bytesField(tx.data, 'data');
    else if (tx.input !== undefined) wire.data = bytesField(tx.input, 'input');
    for (const field of ['value', 'gas', 'maxFeePerGas', 'maxPriorityFeePerGas'] as const) {
      const value = tx[field] ?? (field === 'gas' ? tx.gasLimit : undefined);
      if (value !== undefined) wire[field] = uintField(value as UintInput, field).toString();
    }
    if (tx.nonce !== undefined) wire.nonce = safeInteger(uintField(tx.nonce as UintInput, 'nonce'), 'nonce');
    if (tx.chainId !== undefined) {
      const chainId = safeInteger(uintField(tx.chainId as UintInput, 'chainId'), 'chainId');
      if (chainId !== this.chainId) throw new ChainDisconnectedError(chainId, this.chainId);
      wire.chainId = chainId;
    }
    const response = await this.gateway<SendTxResponse>('POST', '/v1/wallet/send', { tx: wire });
    return response.tx_hash;
  }

  private async signTypedData(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount();
    this.requireSameAccount(params[0], account, 'address');
    const raw = params[1];
    let parsed: unknown = raw;
    if (typeof raw === 'string') {
      try {
        parsed = JSON.parse(raw);
      } catch {
        throw new InvalidParamsError('typedData', 'typed data is not valid JSON');
      }
    }
    if (
      !isRecord(parsed) ||
      !isRecord(parsed.types) ||
      typeof parsed.primaryType !== 'string' ||
      parsed.primaryType.length === 0 ||
      !isRecord(parsed.message) ||
      (parsed.domain !== undefined && !isRecord(parsed.domain))
    ) {
      throw new InvalidParamsError('typedData', 'typed data requires types, primaryType and message');
    }
    if (isRecord(parsed.domain) && parsed.domain.chainId !== undefined) {
      const chainId = safeInteger(uintField(parsed.domain.chainId as UintInput, 'domain.chainId'), 'domain.chainId');
      if (chainId !== this.chainId) throw new ChainDisconnectedError(chainId, this.chainId);
    }
    const typedData = parsed as unknown as TypedDataPayload;
    const response = await this.gateway<SignTypedDataResponse>('POST', '/v1/wallet/sign-typed-data', { typedData });
    return response.signature;
  }

  private async personalSign(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount();
    const [first, second] = params;
    if (typeof first !== 'string') throw new InvalidParamsError('message', 'personal_sign requires a message');
    this.requireSameAccount(second, account, 'address');
    const message = decodeMessage(first);
    const response = await this.gateway<SignMessageResponse>('POST', '/v1/wallet/sign-message', { message });
    return response.signature;
  }

  private async ethSign(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount();
    const [address, digest, construction] = params;
    this.requireSameAccount(address, account, 'address');
    if (typeof digest !== 'string' || !/^0x[0-9a-fA-F]{64}$/.test(digest)) {
      throw new InvalidParamsError('digest', 'eth_sign requires a 32-byte digest');
    }
    if (!isRecord(construction)) {
      throw new UnauthorizedError(
        'missing_construction',
        'eth_sign is accepted only with a sponsored batch or EIP-7702 authorisation construction',
      );
    }
    const wire = this.wireConstruction(construction, account);
    const recomputed = constructionDigest(wire);
    if (recomputed.toLowerCase() !== digest.toLowerCase()) {
      throw new UnauthorizedError('digest_mismatch', 'the supplied digest does not match its construction');
    }
    const response = await this.gateway<SignDigestResponse>('POST', '/v1/wallet/sign-digest', { construction: wire });
    return response.signature;
  }

  private wireConstruction(construction: Record<string, unknown>, account: Hex): WireDigestConstruction {
    if (construction.kind !== 'sponsored_batch' && construction.kind !== 'eip7702_authorization') {
      throw new UnauthorizedError(
        'unknown_construction',
        'eth_sign construction must be sponsored_batch or eip7702_authorization',
      );
    }
    const wire = toWireConstruction(construction as unknown as DigestConstruction);
    if (wire.kind === 'sponsored_batch') this.requireSameAccount(wire.account, account, 'account');
    this.requireChain(wire.chainId);
    return wire;
  }

  private async signCustody(params: readonly unknown[]): Promise<Hex> {
    this.requireAccount();
    const first = params[0];
    const custody = isRecord(first) ? first.custody : first;
    if (typeof custody !== 'string' || !BYTES.test(custody) || custody.length <= 2) {
      throw new InvalidParamsError('custody', 'paxeer_signCustody requires non-empty custody bytes');
    }
    const response = await this.gateway<SignCustodyResponse>('POST', '/v1/wallet/sign-custody', {
      custody: custody.toLowerCase(),
    });
    return response.signature;
  }

  private async proxy(method: string, params: RequestArguments['params']): Promise<unknown> {
    this.rpcId += 1;
    let res: Response;
    try {
      res = await this.fetchImpl(this.rpcUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: this.rpcId, method, params: params ?? [] }),
      });
    } catch (error) {
      throw new DisconnectedError('the RPC base is unreachable', { cause: (error as Error).message });
    }
    let payload: unknown;
    try {
      payload = await res.json();
    } catch {
      throw new RpcResponseError(-32603, `the RPC base returned a non-JSON response: ${res.status}`);
    }
    if (!isRecord(payload)) throw new RpcResponseError(-32603, 'the RPC base returned a malformed response');
    if (isRecord(payload.error)) {
      const code = typeof payload.error.code === 'number' ? payload.error.code : -32603;
      const message = typeof payload.error.message === 'string' ? payload.error.message : 'RPC error';
      throw new RpcResponseError(code, message, payload.error.data);
    }
    if (!res.ok) throw new RpcResponseError(-32603, `the RPC base failed: ${res.status}`);
    if (!('result' in payload)) throw new RpcResponseError(-32603, 'the RPC base returned no result');
    return payload.result;
  }

  private async gateway<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
    const token = await this.token();
    if (!token) {
      this.dropAccounts();
      throw new UnauthorizedError('no_token', 'no signed-in session');
    }
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    let res: Response;
    try {
      res = await this.fetchImpl(`${this.gatewayUrl}${path}`, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      });
    } catch (error) {
      throw new DisconnectedError('the wallet gateway is unreachable', { cause: (error as Error).message });
    }
    let payload: unknown;
    try {
      payload = await res.json();
    } catch {
      payload = null;
    }
    if (!res.ok) {
      if (res.status === 401) this.dropAccounts();
      throw gatewayRefusal(res.status, payload);
    }
    return payload as T;
  }

  private dropAccounts(): void {
    if (this.accounts.length === 0) return;
    this.accounts = [];
    this.emit('accountsChanged', []);
  }

  private requireAccount(): Hex {
    const account = this.accounts[0];
    if (!account) throw new UnauthorizedError('not_connected', 'call eth_requestAccounts first');
    return account;
  }

  private requireSameAccount(value: unknown, account: Hex, field: string): void {
    const address = addressField(value, field);
    if (address.toLowerCase() !== account.toLowerCase()) {
      throw new UnauthorizedError('account_mismatch', `${field} is not the connected account`);
    }
  }

  private requireChain(chainId: string): void {
    const requested = Number(chainId);
    if (requested !== this.chainId) throw new ChainDisconnectedError(requested, this.chainId);
  }

  private emit(event: ProviderEvent, payload: unknown): void {
    const set = this.listeners.get(event);
    if (!set) return;
    for (const listener of [...set]) listener(payload);
  }
}

export function sponsoredBatchDigest(batch: WireSponsoredBatch): Hex {
  const quoteDigest = hashMessage({
    raw: keccak256(
      encodeAbiParameters(
        [
          { type: 'bytes32' },
          { type: 'uint256' },
          { type: 'address' },
          { type: 'address' },
          { type: 'address' },
          { type: 'uint256' },
          { type: 'uint256' },
          { type: 'uint256' },
          { type: 'uint256' },
          { type: 'uint256' },
        ],
        [
          QUOTE_TYPEHASH,
          BigInt(batch.chainId),
          batch.account,
          batch.quote.sponsor,
          batch.quote.token,
          BigInt(batch.quote.maxTokenAmount),
          BigInt(batch.quote.tokenAmount),
          BigInt(batch.quote.deadline),
          BigInt(batch.quote.quoteNonce),
          BigInt(batch.quote.gasCost),
        ],
      ),
    ),
  });
  const callsHash = keccak256(
    encodeAbiParameters(
      [
        {
          type: 'tuple[]',
          components: [
            { name: 'to', type: 'address' },
            { name: 'value', type: 'uint256' },
            { name: 'data', type: 'bytes' },
          ],
        },
      ],
      [batch.calls.map((call) => ({ to: call.to, value: BigInt(call.value), data: call.data }))],
    ),
  );
  return hashMessage({
    raw: keccak256(
      encodeAbiParameters(
        [{ type: 'bytes32' }, { type: 'uint256' }, { type: 'bytes32' }, { type: 'bytes32' }],
        [BATCH_TYPEHASH, BigInt(batch.nonce), callsHash, quoteDigest],
      ),
    ),
  });
}

export function eip7702AuthorizationDigest(authorization: WireEip7702Authorization): Hex {
  return keccak256(
    concat([
      '0x05',
      toRlp([
        rlpInteger(BigInt(authorization.chainId)),
        authorization.address.toLowerCase() as Hex,
        rlpInteger(BigInt(authorization.nonce)),
      ]),
    ]),
  );
}

export function constructionDigest(construction: WireDigestConstruction): Hex {
  return construction.kind === 'sponsored_batch'
    ? sponsoredBatchDigest(construction)
    : eip7702AuthorizationDigest(construction);
}

export function wireSponsoredBatch(construction: SponsoredBatchConstruction): WireSponsoredBatch {
  if (!Array.isArray(construction.calls) || construction.calls.length === 0) {
    throw new InvalidParamsError('calls', 'a sponsored batch requires at least one call');
  }
  const quote = construction.quote;
  if (!isRecord(quote)) throw new InvalidParamsError('quote', 'a sponsored batch requires a quote');
  return {
    kind: 'sponsored_batch',
    chainId: uintField(construction.chainId, 'chainId').toString(),
    account: addressField(construction.account, 'account'),
    nonce: uintField(construction.nonce, 'nonce').toString(),
    calls: construction.calls.map((call, index) => {
      if (!isRecord(call)) throw new InvalidParamsError(`calls.${index}`, 'each call must be an object');
      return {
        to: addressField(call.to, `calls.${index}.to`),
        value: uintField(call.value as UintInput, `calls.${index}.value`).toString(),
        data: bytesField(call.data, `calls.${index}.data`),
      };
    }),
    quote: {
      sponsor: addressField(quote.sponsor, 'quote.sponsor'),
      token: addressField(quote.token, 'quote.token'),
      maxTokenAmount: uintField(quote.maxTokenAmount, 'quote.maxTokenAmount').toString(),
      tokenAmount: uintField(quote.tokenAmount, 'quote.tokenAmount').toString(),
      deadline: uintField(quote.deadline, 'quote.deadline').toString(),
      quoteNonce: uintField(quote.quoteNonce, 'quote.quoteNonce').toString(),
      gasCost: uintField(quote.gasCost, 'quote.gasCost').toString(),
    },
  };
}

export function toWireConstruction(construction: DigestConstruction): WireDigestConstruction {
  if (construction.kind === 'sponsored_batch') return wireSponsoredBatch(construction);
  const nonce = uintField(construction.nonce, 'nonce');
  if (nonce >= UINT64_MAX) throw new InvalidParamsError('nonce', 'authorisation nonce exceeds 2^64 - 2');
  return {
    kind: 'eip7702_authorization',
    chainId: uintField(construction.chainId, 'chainId').toString(),
    address: addressField(construction.address, 'address'),
    nonce: nonce.toString(),
  };
}

function rlpInteger(value: bigint): Hex {
  if (value === 0n) return '0x';
  const body = value.toString(16);
  return `0x${body.length % 2 === 0 ? body : `0${body}`}`;
}

function toQuantity(value: bigint): Hex {
  return `0x${value.toString(16)}`;
}

function positional(params: RequestArguments['params']): readonly unknown[] {
  if (params === undefined) return [];
  if (Array.isArray(params)) return params;
  return [params];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function needsBinding(wallet: PublicWallet): boolean {
  const state = (wallet as { binding_state?: unknown }).binding_state;
  return state === 'unbound' || state === 'pending';
}

function isBindingRefused(error: ProviderRpcError): boolean {
  return (
    isRecord(error.data) &&
    error.data.status === 409 &&
    isRecord(error.data.body) &&
    error.data.body.error === 'binding_refused'
  );
}

function isNoWallet(error: ProviderRpcError): boolean {
  return isRecord(error.data) && error.data.reason === 'no_wallet' && error.data.status === 404;
}

function addressField(value: unknown, field: string): Hex {
  if (typeof value !== 'string' || !ADDRESS.test(value)) {
    throw new InvalidParamsError(field, `${field} must be a 20-byte address`);
  }
  return value as Hex;
}

function bytesField(value: unknown, field: string): Hex {
  if (typeof value !== 'string' || !BYTES.test(value)) {
    throw new InvalidParamsError(field, `${field} must be 0x-prefixed bytes`);
  }
  return value as Hex;
}

function uintField(value: UintInput | undefined, field: string): bigint {
  let parsed: bigint;
  if (typeof value === 'bigint') parsed = value;
  else if (typeof value === 'number' && Number.isSafeInteger(value)) parsed = BigInt(value);
  else if (typeof value === 'string' && (QUANTITY.test(value) || /^(0|[1-9][0-9]*)$/.test(value))) parsed = BigInt(value);
  else throw new InvalidParamsError(field, `${field} must be an unsigned integer`);
  if (parsed < 0n || parsed > UINT256_MAX) throw new InvalidParamsError(field, `${field} is out of range`);
  return parsed;
}

function safeInteger(value: bigint, field: string): number {
  if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw new InvalidParamsError(field, `${field} is out of range`);
  return Number(value);
}

function decodeMessage(message: string): string {
  if (!BYTES.test(message) || message.length <= 2) return message;
  const bytes = new Uint8Array((message.length - 2) / 2);
  for (let i = 0; i < bytes.length; i += 1) bytes[i] = Number.parseInt(message.slice(2 + i * 2, 4 + i * 2), 16);
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    throw new InvalidParamsError('message', 'personal_sign bytes must be UTF-8 text');
  }
}
