import { Agent, request as httpsRequest } from 'node:https';
import { createHash, createPublicKey, randomUUID, verify as edVerify } from 'node:crypto';
import { readFileSync } from 'node:fs';
import {
  bytesToHex,
  decodeAbiParameters,
  decodeFunctionResult,
  encodeFunctionData,
  getAddress,
  hexToBytes,
  keccak256,
  parseAbi,
  recoverTransactionAddress,
  serializeTransaction,
  type Hex,
  type TransactionSerializableEIP1559,
} from 'viem';
import { publicKeyToAddress } from 'viem/accounts';
import { RpcPool, RpcResponseError } from '../rpc/pool.js';

export const ADDR_PRECOMPILE = '0x0000000000000000000000000000000000001004' as const;
export const ANCHOR_PRECOMPILE = '0x0000000000000000000000000000000000001014' as const;
export const BIND_DOMAIN = 'LX:PAXEER-BIND:v1';
export const DID_PREFIX = 'did:layerx:';

export const addrAbi = parseAbi([
  'function bindLayerX(bytes32 didPublicKey, bytes signature)',
  'function layerXBindNonce(address evm) view returns (uint64)',
  'function getUnifiedAccount(address evm) view returns (address evm, string paxAddr, bytes32 didPublicKey, bytes32 layerxMainAccountId)',
  'event LayerXBound(address indexed evm, bytes32 indexed didPublicKey, uint64 nonce)',
]);

export const anchorAbi = parseAbi(['function latestFinalized() view returns (uint64 batchNumber, bool exists)']);

const ZERO32 = `0x${'00'.repeat(32)}` as Hex;

function strip(hex: string): string {
  return hex.startsWith('0x') || hex.startsWith('0X') ? hex.slice(2) : hex;
}

export function normalisePublicKey(hex: string): string {
  const raw = strip(hex).toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(raw)) throw new Error('an Ed25519 public key is 32 bytes of hex');
  return raw;
}

export function didFromPublicKey(publicKeyHex: string): string {
  return `${DID_PREFIX}${normalisePublicKey(publicKeyHex)}`;
}

export function publicKeyFromDid(did: string): string {
  if (!did.startsWith(DID_PREFIX)) throw new Error('not a did:layerx identifier');
  return normalisePublicKey(did.slice(DID_PREFIX.length));
}

export function mainAccountName(did: string): string {
  return `agent:${did}:main`;
}

export function mainAccountId(did: string): string {
  const name = Buffer.from(mainAccountName(did), 'utf8');
  const len = Buffer.alloc(4);
  len.writeUInt32BE(name.length, 0);
  return createHash('sha256').update(Buffer.from('LX:ACCOUNT:v1', 'utf8')).update(len).update(name).digest('hex');
}

export function bindMessage(chainId: bigint | number, address: string, nonce: bigint | number): Buffer {
  const chain = Buffer.alloc(32);
  const c = BigInt(chainId);
  if (c <= 0n || c >= 1n << 256n) throw new Error('chain id out of range');
  chain.write(c.toString(16).padStart(64, '0'), 'hex');
  const addr = Buffer.from(strip(getAddress(address)), 'hex');
  const n = Buffer.alloc(8);
  n.writeBigUInt64BE(BigInt(nonce), 0);
  return Buffer.concat([Buffer.from(BIND_DOMAIN, 'utf8'), chain, addr, n]);
}

const ED25519_SPKI_PREFIX = Buffer.from('302a300506032b6570032100', 'hex');

export function verifyEd25519(publicKeyHex: string, message: Buffer, signatureHex: string): boolean {
  const sig = Buffer.from(strip(signatureHex), 'hex');
  if (sig.length !== 64) return false;
  const key = createPublicKey({
    key: Buffer.concat([ED25519_SPKI_PREFIX, Buffer.from(normalisePublicKey(publicKeyHex), 'hex')]),
    format: 'der',
    type: 'spki',
  });
  return edVerify(null, message, key, sig);
}

export function bindLayerXCalldata(publicKeyHex: string, signatureHex: string): Hex {
  const sig = strip(signatureHex);
  if (!/^[0-9a-fA-F]{128}$/.test(sig)) throw new Error('a binding signature is 64 bytes of hex');
  return encodeFunctionData({
    abi: addrAbi,
    functionName: 'bindLayerX',
    args: [`0x${normalisePublicKey(publicKeyHex)}`, `0x${sig}`],
  });
}

export interface UnifiedAccount {
  evm: `0x${string}`;
  paxAddr: string;
  didPublicKey: string | null;
  mainAccountId: string | null;
}

export async function readUnifiedAccount(rpc: RpcPool, address: `0x${string}`): Promise<UnifiedAccount> {
  const data = encodeFunctionData({ abi: addrAbi, functionName: 'getUnifiedAccount', args: [address] });
  const out = await rpc.request<Hex>('eth_call', [{ to: ADDR_PRECOMPILE, data }, 'latest']);
  const [evm, paxAddr, did, main] = decodeFunctionResult({ abi: addrAbi, functionName: 'getUnifiedAccount', data: out });
  return {
    evm,
    paxAddr,
    didPublicKey: did === ZERO32 ? null : strip(did).toLowerCase(),
    mainAccountId: main === ZERO32 ? null : strip(main).toLowerCase(),
  };
}

export async function readBindNonce(rpc: RpcPool, address: `0x${string}`): Promise<bigint> {
  const data = encodeFunctionData({ abi: addrAbi, functionName: 'layerXBindNonce', args: [address] });
  const out = await rpc.request<Hex>('eth_call', [{ to: ADDR_PRECOMPILE, data }, 'latest']);
  return decodeFunctionResult({ abi: addrAbi, functionName: 'layerXBindNonce', data: out });
}

export type KernelAvailability =
  | { state: 'available'; finalized_batch: string }
  | { state: 'unavailable'; reason: string };

export async function readKernelAvailability(rpc: RpcPool): Promise<KernelAvailability> {
  try {
    const data = encodeFunctionData({ abi: anchorAbi, functionName: 'latestFinalized' });
    const out = await rpc.request<Hex>('eth_call', [{ to: ANCHOR_PRECOMPILE, data }, 'latest']);
    const [batch, exists] = decodeAbiParameters(
      [{ type: 'uint64' }, { type: 'bool' }],
      out,
    );
    if (!exists) return { state: 'unavailable', reason: 'no kernel checkpoint is finalized on chain' };
    return { state: 'available', finalized_batch: batch.toString() };
  } catch (err) {
    return { state: 'unavailable', reason: `kernel anchor could not be read: ${(err as Error).message}` };
  }
}

export class AttestorRefusal extends Error {
  readonly status: number;
  readonly category: string;
  readonly code: string;
  readonly nodeUrl: string;
  constructor(nodeUrl: string, status: number, category: string, code: string, message: string) {
    super(`${category}: ${code}: ${message}`);
    this.name = 'AttestorRefusal';
    this.nodeUrl = nodeUrl;
    this.status = status;
    this.category = category;
    this.code = code;
  }
}

export class AttestorUnavailable extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AttestorUnavailable';
  }
}

export interface GeneratedKey {
  keyId: string;
  curve: 'secp256k1' | 'ed25519';
  publicKey: string;
  address: `0x${string}` | null;
  did: string | null;
}

export interface SignedPayload {
  signature: string;
  recoveryId: number | null;
  signers: string[];
}

export type SignBody =
  | { kind: 'evm_tx'; transaction: Hex }
  | { kind: 'lx_bind'; message: Hex };

interface NodeHealthAnswer {
  node_id: string;
  ready: boolean;
}

export interface AttestorDaemonOptions {
  endpoints: string[];
  cert: string | Buffer;
  key: string | Buffer;
  ca: string | Buffer;
  quorum: number;
  timeoutMs: number;
}

export class AttestorDaemonClient {
  private readonly agent: Agent;
  private readonly opts: AttestorDaemonOptions;

  constructor(opts: AttestorDaemonOptions) {
    if (opts.endpoints.length < opts.quorum) throw new Error('fewer attestor endpoints than the signing quorum');
    this.opts = opts;
    this.agent = new Agent({ cert: opts.cert, key: opts.key, ca: opts.ca, keepAlive: true, minVersion: 'TLSv1.3' });
  }

  close(): void {
    this.agent.destroy();
  }

  private call(base: string, method: 'GET' | 'POST', path: string, body: unknown, headers: Record<string, string>): Promise<{ status: number; json: unknown }> {
    const url = new URL(path, base.endsWith('/') ? base : `${base}/`);
    const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    return new Promise((resolve, reject) => {
      const req = httpsRequest(
        url,
        {
          method,
          agent: this.agent,
          headers: {
            ...headers,
            ...(payload ? { 'content-type': 'application/json', 'content-length': String(payload.length) } : {}),
          },
          timeout: this.opts.timeoutMs,
        },
        (res) => {
          const chunks: Buffer[] = [];
          res.on('data', (c: Buffer) => chunks.push(c));
          res.on('end', () => {
            const text = Buffer.concat(chunks).toString('utf8');
            let json: unknown = null;
            try {
              json = text.length > 0 ? JSON.parse(text) : null;
            } catch {
              reject(new AttestorUnavailable(`${base} answered non-JSON with status ${res.statusCode}`));
              return;
            }
            resolve({ status: res.statusCode ?? 0, json });
          });
          res.on('error', reject);
        },
      );
      req.on('timeout', () => req.destroy(new AttestorUnavailable(`${base} timed out`)));
      req.on('error', (err) => reject(err instanceof AttestorUnavailable ? err : new AttestorUnavailable(`${base}: ${err.message}`)));
      if (payload) req.write(payload);
      req.end();
    });
  }

  private async post<T>(base: string, path: string, body: unknown, headers: Record<string, string> = {}): Promise<T> {
    const { status, json } = await this.call(base, 'POST', path, body, headers);
    if (status !== 200) {
      const err = (json as { error?: { category?: string; code?: string; message?: string } } | null)?.error;
      throw new AttestorRefusal(base, status, err?.category ?? 'unknown', err?.code ?? `http_${status}`, err?.message ?? '');
    }
    return json as T;
  }

  async health(): Promise<Array<{ url: string; nodeId: string | null; ready: boolean }>> {
    return Promise.all(
      this.opts.endpoints.map(async (url) => {
        try {
          const { json } = await this.call(url, 'GET', 'health', undefined, {});
          const h = json as NodeHealthAnswer;
          return { url, nodeId: typeof h?.node_id === 'string' ? h.node_id : null, ready: h?.ready === true };
        } catch {
          return { url, nodeId: null, ready: false };
        }
      }),
    );
  }

  async generate(keyId: string, curve: 'secp256k1' | 'ed25519', owner: string, account?: string): Promise<GeneratedKey> {
    const body: Record<string, string> = { session_id: randomUUID(), key_id: keyId, curve, owner };
    if (account) body.account = account;
    const settled = await Promise.allSettled(
      this.opts.endpoints.map((url) =>
        this.post<{ key_id: string; curve: string; public_key: string; address?: string; did?: string }>(url, 'v1/keys/generate', body),
      ),
    );
    const failures = settled.filter((s): s is PromiseRejectedResult => s.status === 'rejected');
    if (failures.length > 0) {
      const first = failures[0]!.reason as Error;
      if (first instanceof AttestorRefusal) throw first;
      throw new AttestorUnavailable(`key generation failed on ${failures.length} attestors: ${first.message}`);
    }
    const answers = settled.map((s) => (s as PromiseFulfilledResult<{ key_id: string; curve: string; public_key: string; address?: string; did?: string }>).value);
    const pub = strip(answers[0]!.public_key).toLowerCase();
    if (answers.some((a) => strip(a.public_key).toLowerCase() !== pub || a.key_id !== keyId || a.curve !== curve)) {
      throw new AttestorUnavailable('attestors disagree on the generated public key');
    }
    if (curve === 'secp256k1') {
      if (pub.length !== 130 || !pub.startsWith('04')) throw new AttestorUnavailable('secp256k1 public key is not uncompressed');
      const address = publicKeyToAddress(`0x${pub}`);
      if (answers[0]!.address && getAddress(answers[0]!.address) !== address) {
        throw new AttestorUnavailable('attestor address does not match the public key');
      }
      return { keyId, curve, publicKey: pub, address, did: null };
    }
    const did = didFromPublicKey(pub);
    if (answers[0]!.did && answers[0]!.did !== did) throw new AttestorUnavailable('attestor DID does not match the public key');
    return { keyId, curve, publicKey: pub, address: null, did };
  }

  async sign(keyId: string, body: SignBody, bearerToken: string): Promise<SignedPayload> {
    const ready = (await this.health()).filter((h) => h.ready && h.nodeId);
    if (ready.length < this.opts.quorum) {
      throw new AttestorUnavailable(`only ${ready.length} attestors are ready, ${this.opts.quorum} are needed`);
    }
    const chosen = ready.slice(0, this.opts.quorum);
    const signers = chosen.map((c) => c.nodeId!) ;
    const request = { session_id: randomUUID(), key_id: keyId, signers, ...body };
    const settled = await Promise.allSettled(
      chosen.map((c) =>
        this.post<{ signature: string; recovery_id?: number }>(c.url, 'v1/sign', request, { authorization: `Bearer ${bearerToken}` }),
      ),
    );
    const failure = settled.find((s): s is PromiseRejectedResult => s.status === 'rejected');
    if (failure) throw failure.reason;
    const answers = settled.map((s) => (s as PromiseFulfilledResult<{ signature: string; recovery_id?: number }>).value);
    const sig = strip(answers[0]!.signature).toLowerCase();
    if (answers.some((a) => strip(a.signature).toLowerCase() !== sig)) {
      throw new AttestorUnavailable('attestors returned different signatures');
    }
    return { signature: sig, recoveryId: answers[0]!.recovery_id ?? null, signers };
  }
}

export function attestorDaemonFromConfig(cfg: {
  ATTESTOR_ENDPOINTS: string[];
  ATTESTOR_CLIENT_CERT_FILE?: string;
  ATTESTOR_CLIENT_KEY_FILE?: string;
  ATTESTOR_CA_FILE?: string;
  ATTESTOR_QUORUM: number;
  ATTESTOR_TIMEOUT_MS: number;
}): AttestorDaemonClient | null {
  if (cfg.ATTESTOR_ENDPOINTS.length === 0) return null;
  return new AttestorDaemonClient({
    endpoints: cfg.ATTESTOR_ENDPOINTS,
    cert: readFileSync(cfg.ATTESTOR_CLIENT_CERT_FILE!),
    key: readFileSync(cfg.ATTESTOR_CLIENT_KEY_FILE!),
    ca: readFileSync(cfg.ATTESTOR_CA_FILE!),
    quorum: cfg.ATTESTOR_QUORUM,
    timeoutMs: cfg.ATTESTOR_TIMEOUT_MS,
  });
}

export async function signBindWithAttestors(
  client: AttestorDaemonClient,
  keyId: string,
  publicKeyHex: string,
  message: Buffer,
  bearerToken: string,
): Promise<string> {
  const out = await client.sign(keyId, { kind: 'lx_bind', message: bytesToHex(message) }, bearerToken);
  if (!verifyEd25519(publicKeyHex, message, out.signature)) {
    throw new AttestorUnavailable('attestor binding signature does not verify under the identity key');
  }
  return out.signature;
}

export async function signTransactionWithAttestors(
  client: AttestorDaemonClient,
  keyId: string,
  address: `0x${string}`,
  tx: TransactionSerializableEIP1559,
  bearerToken: string,
): Promise<Hex> {
  const envelope = serializeTransaction(tx, { r: '0x0', s: '0x0', yParity: 0 });
  const out = await client.sign(keyId, { kind: 'evm_tx', transaction: envelope }, bearerToken);
  const raw = hexToBytes(`0x${out.signature}`);
  if (raw.length !== 65 || out.recoveryId === null) throw new AttestorUnavailable('attestor transaction signature is malformed');
  const signed = serializeTransaction(tx, {
    r: bytesToHex(raw.slice(0, 32)),
    s: bytesToHex(raw.slice(32, 64)),
    yParity: out.recoveryId & 1,
  });
  const recovered = await recoverTransactionAddress({ serializedTransaction: signed as `0x02${string}` });
  if (getAddress(recovered) !== getAddress(address)) {
    throw new AttestorUnavailable('attestor transaction signature recovers another address');
  }
  return signed;
}

export function transactionHash(raw: Hex): Hex {
  return keccak256(raw);
}

export interface ChainReceipt {
  status: 'success' | 'reverted';
  gasUsed: bigint;
  blockNumber: bigint;
}

export async function readReceipt(rpc: RpcPool, hash: Hex): Promise<ChainReceipt | null> {
  const r = await rpc.request<{ status: Hex; gasUsed: Hex; blockNumber: Hex } | null>('eth_getTransactionReceipt', [hash]);
  if (!r) return null;
  return { status: BigInt(r.status) === 1n ? 'success' : 'reverted', gasUsed: BigInt(r.gasUsed), blockNumber: BigInt(r.blockNumber) };
}

export async function broadcastOnce(rpc: RpcPool, raw: Hex): Promise<Hex> {
  const hash = transactionHash(raw);
  if (await readReceipt(rpc, hash)) return hash;
  try {
    await rpc.sendRawTransaction(raw);
  } catch (err) {
    if (!(err instanceof RpcResponseError)) throw err;
    const known = await rpc.request<unknown>('eth_getTransactionByHash', [hash]).catch(() => null);
    if (!known) throw err;
  }
  return hash;
}

export async function waitForReceipt(rpc: RpcPool, hash: Hex, timeoutMs: number, pollMs: number): Promise<ChainReceipt> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const r = await readReceipt(rpc, hash);
    if (r) return r;
    if (Date.now() >= deadline) throw new Error(`transaction ${hash} has no receipt after ${timeoutMs} ms`);
    await new Promise((resolve) => setTimeout(resolve, pollMs));
  }
}

export interface BindTransactionInput {
  chainId: number;
  address: `0x${string}`;
  publicKey: string;
  signature: string;
  nonce: number;
  gas: bigint;
  maxFeePerGas: bigint;
  maxPriorityFeePerGas: bigint;
}

export function bindLayerX(input: BindTransactionInput): TransactionSerializableEIP1559 {
  return {
    type: 'eip1559',
    chainId: input.chainId,
    nonce: input.nonce,
    to: ADDR_PRECOMPILE,
    value: 0n,
    data: bindLayerXCalldata(input.publicKey, input.signature),
    gas: input.gas,
    maxFeePerGas: input.maxFeePerGas,
    maxPriorityFeePerGas: input.maxPriorityFeePerGas,
  };
}
