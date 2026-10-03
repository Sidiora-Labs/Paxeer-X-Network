import { concat, decodeAbiParameters, decodeFunctionData, encodeAbiParameters, encodeFunctionData, parseAbi, recoverAddress, recoverMessageAddress, hashMessage, keccak256, toHex, toRlp } from 'viem';
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
  private readonly custodyApprovals = new Map<string, { bytes: Hex; signature: Hex }>();
  private capsGeneration = 0;
  private readonly capsRequests = new Set<AbortController>();

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

  invalidateCapsSession(): void {
    this.capsGeneration += 1;
    for (const pending of this.capsRequests) pending.abort();
    this.capsRequests.clear();
    this.emit('message', { type: 'wallet_caps_invalidated' });
  }

  disconnect(): void {
    this.invalidateCapsSession();
    const hadAccounts = this.accounts.length > 0;
    this.accounts = [];
    this.custodyApprovals.clear();
    this.connected = false;
    if (hadAccounts) this.emit('accountsChanged', []);
    this.emit('disconnect', new DisconnectedError('the embedded wallet was disconnected'));
  }

  async request(args: RequestArguments): Promise<unknown> {
    if (!args || typeof args !== 'object' || typeof args.method !== 'string' || args.method.length === 0) {
      throw new InvalidParamsError('method', 'request requires a method name');
    }
    const method = args.method;
    let params = positional(args.params);
    if (SIGNING_METHODS.has(method)) {
      try { params = freezeRequest(structuredClone(params)); } catch { throw new InvalidParamsError('params', 'signing parameters must be immutable data'); }
    }
    if (UNSUPPORTED_METHODS.has(method)) throw new UnsupportedMethodError(method);
    if (SIGNING_METHODS.has(method) && this.confirm) {
      const approved = await this.confirm({ method, params });
      if (!approved) throw new UserRejectedRequestError();
    }
    switch (method) {
      case 'lx_getWalletCaps':
        if (params.length !== 0) throw new InvalidParamsError('params', 'caps use the connected account');
        return this.walletCaps();
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
      case 'paxeer_prepareCustody':
        return this.prepareCustody(params);
      case 'paxeer_custodyStatus':
        return this.custodyStatus(params);
      case 'paxeer_restoreCustody':
        return this.restoreCustody(params);
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
    if (accountsChanged || chainChanged) this.invalidateCapsSession();
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
    let custody: { bytes: Hex; signature: Hex } | undefined;
    if (typeof wire.to === 'string' && wire.to.toLowerCase() === CUSTODY_TARGET) {
      custody = this.custodyApprovals.get(custodyCallKey(account, BigInt(this.chainId), wire.to as Hex,
        BigInt(String(wire.value ?? '0')), (wire.data ?? '0x') as Hex));
      if (!custody) throw new UnauthorizedError('missing_construction', 'approve this exact custody call before sending');
      const approved = decodeCustodyAuthorization(custody.bytes);
      for (const field of ['nonce', 'gas', 'maxFeePerGas', 'maxPriorityFeePerGas'] as const) {
        if (wire[field] !== undefined && BigInt(String(wire[field])) !== approved[field]) {
          throw new InvalidParamsError(field, 'transaction differs from the signed custody authorization');
        }
      }
      wire.chainId = this.chainId; wire.nonce = safeInteger(approved.nonce, 'nonce');
      wire.gas = approved.gas.toString(); wire.maxFeePerGas = approved.maxFeePerGas.toString();
      wire.maxPriorityFeePerGas = approved.maxPriorityFeePerGas.toString();
    }
    const response = await this.gateway<SendTxResponse>('POST', '/v1/wallet/send', { tx: wire, ...(custody ? { custody } : {}) });
    if (!/^0x[0-9a-fA-F]{64}$/.test(response.tx_hash)) throw new InvalidParamsError('tx_hash', 'gateway returned an invalid transaction hash');
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
    if (params.length !== 3) throw new InvalidParamsError('params', 'eth_sign requires account, digest and complete construction');
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
    if (wire.kind === 'sponsored_batch' && BigInt(wire.quote.deadline) <= BigInt(Math.floor(Date.now() / 1000))) throw new InvalidParamsError('deadline', 'sponsored consent has expired');
    const recomputed = constructionDigest(wire);
    if (recomputed.toLowerCase() !== digest.toLowerCase()) {
      throw new UnauthorizedError('digest_mismatch', 'the supplied digest does not match its construction');
    }
    const response = await this.gateway<SignDigestResponse>('POST', '/v1/wallet/sign-digest', { construction: wire });
    this.requireSameAccount(response.address, account, 'response.address');
    const signature = signatureField(response.signature);
    this.requireSameAccount(await recoverAddress({ hash: recomputed, signature }), account, 'signature');
    return signature;
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

  private async prepareCustody(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount(); const call = params[0];
    if (params.length !== 1 || !isRecord(call)) throw new InvalidParamsError('custody', 'a custody call is required');
    exactKeys(call, ['account', 'chainId', 'to', 'value', 'data'], 'custody');
    if (call.account !== undefined) this.requireSameAccount(call.account, account, 'account');
    const chainId = call.chainId === undefined ? BigInt(this.chainId) : uintField(call.chainId as UintInput, 'chainId');
    this.requireChain(chainId.toString());
    const to = addressField(call.to, 'to'); const value = uintField(call.value as UintInput, 'value');
    const data = bytesField(call.data, 'data');
    if (to.toLowerCase() !== CUSTODY_TARGET) throw new InvalidParamsError('to', 'custody target required');
    validateCustodyCall(value, data);
    const network = await this.proxy('eth_chainId', []);
    if (typeof network !== 'string' || !QUANTITY.test(network)) throw new InvalidParamsError('chainId', 'RPC returned an invalid chain');
    if (BigInt(network) !== chainId) throw new ChainDisconnectedError(Number(chainId), Number(BigInt(network)));
    const [nonceRaw, gasRaw, priorityRaw, head] = await Promise.all([
      this.proxy('eth_getTransactionCount', [account, 'pending']),
      this.proxy('eth_estimateGas', [{ from: account, to, value: toQuantity(value), data }]),
      this.proxy('eth_maxPriorityFeePerGas', []), this.proxy('eth_getBlockByNumber', ['latest', false]),
    ]);
    const quantity = (v: unknown, field: string): bigint => {
      if (typeof v !== 'string' || !QUANTITY.test(v)) throw new InvalidParamsError(field, 'RPC returned an invalid quantity');
      return uintField(v, field);
    };
    if (!isRecord(head)) throw new InvalidParamsError('block', 'RPC returned no fee block');
    const maxPriorityFeePerGas = quantity(priorityRaw, 'maxPriorityFeePerGas');
    const maxFeePerGas = quantity(head.baseFeePerGas, 'baseFeePerGas') * 2n + maxPriorityFeePerGas;
    return encodeCustodyAuthorization({ account, chainId, to, value, data, nonce: quantity(nonceRaw, 'nonce'),
      gas: quantity(gasRaw, 'gas'), maxFeePerGas, maxPriorityFeePerGas, deadline: BigInt(Math.floor(Date.now() / 1000) + 600) });
  }

  private async custodyStatus(params: readonly unknown[]): Promise<CustodySubmissionStatus> {
    const input = params[0]; const account = this.requireAccount();
    if (params.length !== 1 || !isRecord(input)) throw new InvalidParamsError('custody', 'custody bytes required');
    exactKeys(input, ['custody'], 'custody');
    const bytes = bytesField(input.custody, 'custody'); const authorization = decodeCustodyAuthorization(bytes);
    this.requireSameAccount(authorization.account, account, 'account'); this.requireChain(authorization.chainId.toString());
    const id = keccak256(bytes);
    const response = await this.gateway<unknown>('GET', `/v1/wallet/custody/${id}`);
    return decodeCustodyStatus(response, id);
  }

  private async restoreCustody(params: readonly unknown[]): Promise<null> {
    const account = this.requireAccount(); const input = params[0];
    if (params.length !== 1 || !isRecord(input)) throw new InvalidParamsError('custody', 'retained custody proof is required');
    exactKeys(input, ['custody', 'signature'], 'custody');
    const bytes = bytesField(input.custody, 'custody'); const authorization = decodeCustodyAuthorization(bytes);
    this.requireSameAccount(authorization.account, account, 'account'); this.requireChain(authorization.chainId.toString());
    const signature = signatureField(input.signature);
    this.requireSameAccount(await recoverMessageAddress({ message: { raw: bytes }, signature }), account, 'signature');
    this.custodyApprovals.set(custodyCallKey(account, authorization.chainId, authorization.to, authorization.value, authorization.data), { bytes, signature });
    return null;
  }

  private async signCustody(params: readonly unknown[]): Promise<Hex> {
    const account = this.requireAccount(); const first = params[0];
    if (params.length !== 1) throw new InvalidParamsError('custody', 'one custody authorization is required');
    const custody = bytesField(isRecord(first) ? first.custody : first, 'custody');
    if (isRecord(first)) exactKeys(first, ['custody'], 'custody');
    const authorization = decodeCustodyAuthorization(custody);
    this.requireSameAccount(authorization.account, account, 'account'); this.requireChain(authorization.chainId.toString());
    const now = BigInt(Math.floor(Date.now() / 1000));
    if (authorization.deadline <= now || authorization.deadline > now + 600n) throw new InvalidParamsError('deadline', 'custody consent is expired or exceeds ten minutes');
    const response = await this.gateway<SignCustodyResponse>('POST', '/v1/wallet/sign-custody', { custody: custody.toLowerCase() });
    this.requireSameAccount(response.address, account, 'response.address');
    await this.restoreCustody([{ custody, signature: response.signature }]);
    return signatureField(response.signature);
  }

  private async walletCaps(): Promise<unknown> {
    const address = this.accounts[0]?.toLowerCase();
    const chainId = this.chainId;
    const generation = this.capsGeneration;
    if (!this.connected || !address) throw new UnauthorizedError('not_connected', 'connect a wallet first');
    const token = await this.token();
    if (!token) throw new UnauthorizedError('no_token', 'no signed-in session');
    if (generation !== this.capsGeneration) throw new UnauthorizedError('session_changed', 'the wallet session changed');
    const digest = await globalThis.crypto.subtle.digest('SHA-256', new TextEncoder().encode('LXP/wallet-caps/session/v1\0' + token));
    const sessionId = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
    if (generation !== this.capsGeneration) throw new UnauthorizedError('session_changed', 'the wallet session changed');
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15_000);
    this.capsRequests.add(controller);
    const id = ++this.rpcId;
    try {
      const response = await this.fetchImpl(this.rpcUrl, {
        method: 'POST', signal: controller.signal, cache: 'no-store',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ jsonrpc: '2.0', id, method: 'lx_getWalletCaps', params: [{ address, chain_id: chainId }] }),
      });
      const reader = response.body?.getReader();
      if (!reader) throw new RpcResponseError(-32603, 'caps response has no body');
      const decoder = new TextDecoder('utf-8', { fatal: true });
      let text = '', bytes = 0;
      try {
        for (;;) {
          const chunk = await reader.read();
          if (chunk.done) break;
          bytes += chunk.value.byteLength;
          if (bytes > 4 * 1024 * 1024) { await reader.cancel(); throw new RpcResponseError(-32603, 'caps response exceeds its bound'); }
          text += decoder.decode(chunk.value, { stream: true });
        }
        text += decoder.decode();
      } finally { reader.releaseLock(); }
      const payload: unknown = JSON.parse(text);
      if (generation !== this.capsGeneration || token !== await this.token() || address !== this.accounts[0]?.toLowerCase() || chainId !== this.chainId) {
        throw new UnauthorizedError('session_changed', 'the wallet session changed');
      }
      if (!isRecord(payload) || payload.jsonrpc !== '2.0' || payload.id !== id) throw new RpcResponseError(-32603, 'invalid caps response');
      if (isRecord(payload.error)) throw new RpcResponseError(typeof payload.error.code === 'number' ? payload.error.code : -32603,
        typeof payload.error.message === 'string' ? payload.error.message : 'caps refused', payload.error.data);
      if (!response.ok || !('result' in payload)) throw new RpcResponseError(-32001, 'caps unavailable');
      if (!isRecord(payload.result) || !isRecord(payload.result.context) || payload.result.context.session_id !== sessionId) {
        throw new UnauthorizedError('session_changed', 'caps evidence belongs to another session');
      }
      return payload.result;
    } finally {
      clearTimeout(timeout);
      this.capsRequests.delete(controller);
    }
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
    this.custodyApprovals.clear();
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
  exactKeys(construction as unknown as Record<string, unknown>, ['kind','chainId','account','nonce','calls','quote'], 'construction');
  if (!Array.isArray(construction.calls) || construction.calls.length === 0) {
    throw new InvalidParamsError('calls', 'a sponsored batch requires at least one call');
  }
  const quote = construction.quote;
  if (!isRecord(quote)) throw new InvalidParamsError('quote', 'a sponsored batch requires a quote');
  exactKeys(quote, ['sponsor','token','maxTokenAmount','tokenAmount','deadline','quoteNonce','gasCost'], 'quote');
  return {
    kind: 'sponsored_batch',
    chainId: uintField(construction.chainId, 'chainId').toString(),
    account: addressField(construction.account, 'account'),
    nonce: uintField(construction.nonce, 'nonce').toString(),
    calls: construction.calls.map((call, index) => {
      if (!isRecord(call)) throw new InvalidParamsError(`calls.${index}`, 'each call must be an object');
      exactKeys(call, ['to','value','data'], `calls.${index}`);
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
  if (construction.kind !== 'eip7702_authorization') throw new InvalidParamsError('kind', 'unknown digest construction');
  exactKeys(construction as unknown as Record<string, unknown>, ['kind','chainId','address','nonce'], 'construction');
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

export const CUSTODY_TARGET = '0x0000000000000000000000000000000000001013' as const;
const CUSTODY_DOMAIN = toHex('LX:CUSTODY:v2');
const CUSTODY_FIELDS = [
  { type: 'address' }, { type: 'uint256' }, { type: 'address' }, { type: 'uint256' }, { type: 'bytes' },
  { type: 'uint64' }, { type: 'uint64' }, { type: 'uint64' }, { type: 'uint256' }, { type: 'uint256' },
] as const;
const CUSTODY_ABI = parseAbi(['function deposit(bytes32 beneficiary) payable returns (bytes32 depositId)',
  'function depositToken(address pointer,uint256 amount,bytes32 beneficiary) returns (bytes32 depositId)']);
export interface CustodyAuthorization {
  readonly account: Hex; readonly chainId: bigint; readonly to: Hex; readonly value: bigint; readonly data: Hex;
  readonly nonce: bigint; readonly deadline: bigint; readonly gas: bigint;
  readonly maxFeePerGas: bigint; readonly maxPriorityFeePerGas: bigint;
}
export function encodeCustodyAuthorization(input: CustodyAuthorization): Hex {
  const account = addressField(input.account, 'account'); const to = addressField(input.to, 'to');
  if (to.toLowerCase() !== CUSTODY_TARGET || /^0x0{40}$/i.test(account)) throw new InvalidParamsError('custody', 'invalid custody account or target');
  for (const field of ['chainId','value','nonce','deadline','gas','maxFeePerGas','maxPriorityFeePerGas'] as const) uintField(input[field], field);
  if (input.chainId === 0n || input.gas === 0n || input.maxFeePerGas === 0n || input.maxPriorityFeePerGas > input.maxFeePerGas ||
    input.nonce >= UINT64_MAX || input.deadline > UINT64_MAX || input.gas > UINT64_MAX) throw new InvalidParamsError('custody', 'invalid custody bounds');
  validateCustodyCall(input.value, input.data);
  return concat([CUSTODY_DOMAIN, encodeAbiParameters(CUSTODY_FIELDS, [account,input.chainId,to,input.value,input.data,
    input.nonce,input.deadline,input.gas,input.maxFeePerGas,input.maxPriorityFeePerGas])]);
}
export function decodeCustodyAuthorization(bytes: Hex): CustodyAuthorization {
  if (typeof bytes !== 'string' || !BYTES.test(bytes) || !bytes.toLowerCase().startsWith(CUSTODY_DOMAIN) || bytes.length > 4096) throw new InvalidParamsError('custody', 'canonical v2 custody bytes are required');
  try {
    const [account,chainId,to,value,data,nonce,deadline,gas,maxFeePerGas,maxPriorityFeePerGas] = decodeAbiParameters(CUSTODY_FIELDS, `0x${bytes.slice(CUSTODY_DOMAIN.length)}`);
    const out = { account,chainId,to,value,data,nonce,deadline,gas,maxFeePerGas,maxPriorityFeePerGas };
    if (encodeCustodyAuthorization(out).toLowerCase() !== bytes.toLowerCase()) throw new Error('noncanonical');
    return Object.freeze(out);
  } catch { throw new InvalidParamsError('custody', 'malformed or noncanonical custody authorization'); }
}
function validateCustodyCall(value: bigint, data: Hex): void {
  try {
    const call = decodeFunctionData({ abi: CUSTODY_ABI, data });
    if (encodeFunctionData({ abi: CUSTODY_ABI, functionName: call.functionName, args: call.args }).toLowerCase() !== data.toLowerCase()) throw new Error('noncanonical');
    const beneficiary = call.functionName === 'deposit' ? call.args[0] : call.args[2];
    if (/^0x0{64}$/i.test(beneficiary) || (call.functionName === 'deposit' ? value <= 0n : value !== 0n || call.args[1] <= 0n || /^0x0{40}$/i.test(call.args[0]))) throw new Error('invalid deposit');
  } catch { throw new InvalidParamsError('data', 'only canonical positive custody deposits are accepted'); }
}
function custodyCallKey(account: Hex, chain: bigint, to: Hex, value: bigint, data: Hex): string {
  return keccak256(encodeAbiParameters([{type:'address'},{type:'uint256'},{type:'address'},{type:'uint256'},{type:'bytes'}], [account,chain,to,value,data]));
}
function signatureField(value: unknown): Hex {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{130}$/.test(value)) throw new InvalidParamsError('signature', 'invalid signature');
  return value.toLowerCase() as Hex;
}
function exactKeys(value: Record<string, unknown>, allowed: readonly string[], field: string): void {
  if (Object.keys(value).some(key => !allowed.includes(key))) throw new InvalidParamsError(field, 'unexpected fields are not accepted');
}

export interface CustodySubmissionStatus {
  readonly custody_id: Hex; readonly tx_hash: Hex | null;
  readonly status: 'pending' | 'confirmed' | 'reverted'; readonly receipt: Readonly<Record<string, unknown>> | null;
}
export function decodeCustodyStatus(input: unknown, expected: Hex): CustodySubmissionStatus {
  if (!isRecord(input) || input.custody_id !== expected || (input.status !== 'pending' && input.status !== 'confirmed' && input.status !== 'reverted') ||
    (input.tx_hash !== null && (typeof input.tx_hash !== 'string' || !/^0x[0-9a-fA-F]{64}$/.test(input.tx_hash)))) throw new InvalidParamsError('status', 'invalid custody status');
  const receipt = input.receipt;
  if (receipt !== null && (input.tx_hash === null || !isRecord(receipt) || (receipt.status !== '0x0' && receipt.status !== '0x1') || receipt.transactionHash !== input.tx_hash || typeof receipt.blockHash !== 'string' ||
    !/^0x[0-9a-fA-F]{64}$/.test(receipt.blockHash) || /^0x0{64}$/.test(receipt.blockHash) || typeof receipt.blockNumber !== 'string' || !QUANTITY.test(receipt.blockNumber))) throw new InvalidParamsError('receipt', 'invalid custody receipt evidence');
  if (input.status !== 'pending' && (!isRecord(receipt) || receipt.status !== (input.status === 'confirmed' ? '0x1' : '0x0'))) throw new InvalidParamsError('receipt', 'completion requires matching receipt evidence');
  return Object.freeze(input as unknown as CustodySubmissionStatus);
}

function freezeRequest<T>(value: T): T {
  if (typeof value === 'object' && value !== null) { for (const child of Object.values(value)) freezeRequest(child); Object.freeze(value); }
  return value;
}
