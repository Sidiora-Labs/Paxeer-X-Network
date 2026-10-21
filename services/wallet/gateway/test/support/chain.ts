import { createHash, createPublicKey, verify as edVerify } from 'node:crypto';
import { createServer as createHttpServer, type Server } from 'node:http';
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  decodeFunctionData,
  encodeAbiParameters,
  encodeFunctionResult,
  getAddress,
  keccak256,
  parseTransaction,
  recoverTransactionAddress,
  toFunctionSelector,
  type Abi,
  type Hex,
} from 'viem';

const here = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(here, '../../../../..');
export const addrAbiJson = JSON.parse(readFileSync(join(repoRoot, 'precompiles/addr/abi.json'), 'utf8')) as Abi;
export const anchorAbiJson = JSON.parse(readFileSync(join(repoRoot, 'precompiles/layerxanchor/abi.json'), 'utf8')) as Abi;

export const ADDR = '0x0000000000000000000000000000000000001004';
export const ANCHOR = '0x0000000000000000000000000000000000001014';
export const BIND_SELECTOR = toFunctionSelector('bindLayerX(bytes32,bytes)');
const ED_SPKI = Buffer.from('302a300506032b6570032100', 'hex');
const PRECOMPILE_RANGE = /^0x0{36}1[0-9a-f]{3}$/;

export function chainMainAccountId(pubHex: string): string {
  const name = Buffer.from(`agent:did:layerx:${pubHex}:main`, 'utf8');
  const len = Buffer.alloc(4);
  len.writeUInt32BE(name.length, 0);
  return createHash('sha256').update('LX:ACCOUNT:v1').update(len).update(name).digest('hex');
}

export interface Receipt {
  transactionHash: Hex;
  status: Hex;
  gasUsed: Hex;
  blockNumber: Hex;
  logs: unknown[];
}

export interface Fault {
  method: string;
  mode: 'error' | 'apply-then-error';
  remaining: number;
}

export interface SentTx {
  hash: Hex;
  from: string;
  to: string;
  value: bigint;
  data: Hex | undefined;
}

export class ChainRpcError extends Error {
  readonly code: number;
  constructor(code: number, message: string) {
    super(message);
    this.name = 'ChainRpcError';
    this.code = code;
  }
}

export class TestChain {
  readonly chainId: number;
  readonly balances = new Map<string, bigint>();
  readonly nonces = new Map<string, number>();
  readonly bindNonces = new Map<string, bigint>();
  readonly bindings = new Map<string, string>();
  readonly bindingsByDid = new Map<string, string>();
  readonly receipts = new Map<string, Receipt>();
  readonly sent: SentTx[] = [];
  readonly faults: Fault[] = [];
  baseFee = 1_000_000_000n;
  tip = 2_000_000n;
  block = 1n;
  private server: Server | null = null;
  url = '';

  constructor(chainId: number) {
    this.chainId = chainId;
  }

  fail(method: string, mode: Fault['mode'] = 'error', times = 1): void {
    this.faults.push({ method, mode, remaining: times });
  }

  private takeFault(method: string): Fault | null {
    const f = this.faults.find((x) => x.method === method && x.remaining > 0);
    if (!f) return null;
    f.remaining -= 1;
    return f;
  }

  static gasFor(to: string | undefined, data: Hex | undefined): bigint {
    let gas = 21_000n;
    const bytes = data ? Buffer.from(data.slice(2), 'hex') : Buffer.alloc(0);
    for (const b of bytes) gas += b === 0 ? 4n : 16n;
    if (to && to.toLowerCase() === ADDR && data && data.startsWith(BIND_SELECTOR)) gas += 4_000n + 44_100n;
    return gas;
  }

  private bal(a: string): bigint {
    return this.balances.get(a.toLowerCase()) ?? 0n;
  }

  private call(c: { from?: string; to: string; data?: Hex; value?: Hex }, method: string): Hex {
    const to = c.to.toLowerCase();
    const data = c.data ?? '0x';
    const value = c.value ? BigInt(c.value) : 0n;
    if (value > 0n && this.bal(c.from ?? '0x0000000000000000000000000000000000000000') < value) {
      throw new Error('insufficient funds for transfer');
    }
    if (to === ADDR) {
      const decoded = decodeFunctionData({ abi: addrAbiJson, data });
      if (decoded.functionName === 'getUnifiedAccount') {
        if (this.takeFault('getUnifiedAccount')) throw new Error('injected getUnifiedAccount failure');
        const evm = (decoded.args![0] as string).toLowerCase();
        const did = this.bindings.get(evm);
        return encodeFunctionResult({
          abi: addrAbiJson,
          functionName: 'getUnifiedAccount',
          result: [getAddress(evm), '', did ? `0x${did}` : `0x${'00'.repeat(32)}`, did ? `0x${chainMainAccountId(did)}` : `0x${'00'.repeat(32)}`],
        });
      }
      if (decoded.functionName === 'layerXBindNonce') {
        const evm = (decoded.args![0] as string).toLowerCase();
        return encodeFunctionResult({ abi: addrAbiJson, functionName: 'layerXBindNonce', result: this.bindNonces.get(evm) ?? 0n });
      }
      throw new Error(`addr method ${decoded.functionName} is not served`);
    }
    if (to === ANCHOR) {
      const decoded = decodeFunctionData({ abi: anchorAbiJson, data });
      if (decoded.functionName === 'latestFinalized') return encodeAbiParameters([{ type: 'uint64' }, { type: 'bool' }], [0n, false]);
      throw new Error(`anchor method ${decoded.functionName} is not served`);
    }
    if (PRECOMPILE_RANGE.test(to)) throw new Error(`${method}: precompile ${to} is not served`);
    return '0x';
  }

  private applyBind(from: string, data: Hex): boolean {
    const decoded = decodeFunctionData({ abi: addrAbiJson, data });
    if (decoded.functionName !== 'bindLayerX') return false;
    const [didKey, signature] = decoded.args as [Hex, Hex];
    const pub = didKey.slice(2).toLowerCase();
    const sig = Buffer.from(signature.slice(2), 'hex');
    if (sig.length !== 64 || /^0+$/.test(pub)) return false;
    if (this.bindings.has(from) || this.bindingsByDid.has(pub)) return false;
    const nonce = this.bindNonces.get(from) ?? 0n;
    const chain = Buffer.alloc(32);
    chain.write(BigInt(this.chainId).toString(16).padStart(64, '0'), 'hex');
    const n = Buffer.alloc(8);
    n.writeBigUInt64BE(nonce, 0);
    const message = Buffer.concat([Buffer.from('LX:PAXEER-BIND:v1'), chain, Buffer.from(from.slice(2), 'hex'), n]);
    let ok = false;
    try {
      const key = createPublicKey({ key: Buffer.concat([ED_SPKI, Buffer.from(pub, 'hex')]), format: 'der', type: 'spki' });
      ok = edVerify(null, message, key, sig);
    } catch {
      ok = false;
    }
    if (!ok) return false;
    this.bindings.set(from, pub);
    this.bindingsByDid.set(pub, from);
    this.bindNonces.set(from, nonce + 1n);
    return true;
  }

  private async sendRaw(raw: Hex): Promise<Hex> {
    const hash = keccak256(raw);
    if (this.receipts.has(hash)) throw new Error('already known');
    const tx = parseTransaction(raw);
    const from = (await recoverTransactionAddress({ serializedTransaction: raw as `0x02${string}` })).toLowerCase();
    if (tx.chainId !== this.chainId) throw new Error('wrong chain id');
    const expected = this.nonces.get(from) ?? 0;
    if (tx.nonce !== expected) throw new Error(`nonce ${tx.nonce} but account is at ${expected}`);
    const gas = tx.gas!;
    const maxFee = tx.maxFeePerGas!;
    const value = tx.value ?? 0n;
    if (maxFee < this.baseFee) throw new Error('max fee below base fee');
    if (this.bal(from) < value + gas * maxFee) throw new Error('insufficient funds for gas * price + value');
    const price = maxFee < this.baseFee + (tx.maxPriorityFeePerGas ?? 0n) ? maxFee : this.baseFee + (tx.maxPriorityFeePerGas ?? 0n);
    const to = (tx.to ?? '').toLowerCase();
    const needed = TestChain.gasFor(to, tx.data);
    this.nonces.set(from, expected + 1);
    this.block += 1n;
    let status = 1n;
    let used = needed;
    if (needed > gas) {
      status = 0n;
      used = gas;
    } else if (to === ADDR) {
      if (value !== 0n || !tx.data || !this.applyBind(from, tx.data)) status = 0n;
    } else {
      this.balances.set(to, this.bal(to) + value);
      this.balances.set(from, this.bal(from) - value);
    }
    this.balances.set(from, this.bal(from) - used * price);
    this.sent.push({ hash, from, to, value, data: tx.data });
    this.receipts.set(hash, {
      transactionHash: hash,
      status: `0x${status.toString(16)}`,
      gasUsed: `0x${used.toString(16)}`,
      blockNumber: `0x${this.block.toString(16)}`,
      logs: [],
    });
    return hash;
  }

  private async handle(method: string, params: unknown[]): Promise<unknown> {
    const fault = this.takeFault(method);
    if (fault && fault.mode === 'error') throw new Error(`injected ${method} failure`);
    switch (method) {
      case 'eth_chainId':
        return `0x${this.chainId.toString(16)}`;
      case 'eth_blockNumber':
        return `0x${this.block.toString(16)}`;
      case 'eth_getBlockByNumber':
        return { number: `0x${this.block.toString(16)}`, baseFeePerGas: `0x${this.baseFee.toString(16)}`, transactions: [] };
      case 'eth_maxPriorityFeePerGas':
        return `0x${this.tip.toString(16)}`;
      case 'eth_gasPrice':
        return `0x${(this.baseFee + this.tip).toString(16)}`;
      case 'eth_getBalance':
        return `0x${this.bal(params[0] as string).toString(16)}`;
      case 'eth_getTransactionCount':
        return `0x${(this.nonces.get((params[0] as string).toLowerCase()) ?? 0).toString(16)}`;
      case 'eth_estimateGas': {
        const c = params[0] as { to?: string; data?: Hex };
        return `0x${TestChain.gasFor(c.to, c.data).toString(16)}`;
      }
      case 'eth_call':
        return this.call(params[0] as { from?: string; to: string; data?: Hex; value?: Hex }, method);
      case 'eth_sendRawTransaction': {
        const hash = await this.sendRaw(params[0] as Hex);
        if (fault && fault.mode === 'apply-then-error') throw new Error('injected broadcast failure after the transaction applied');
        return hash;
      }
      case 'eth_getTransactionByHash': {
        const r = this.receipts.get(params[0] as string);
        return r ? { hash: r.transactionHash, blockNumber: r.blockNumber } : null;
      }
      case 'eth_getTransactionReceipt':
        return this.receipts.get(params[0] as string) ?? null;
      default:
        throw new ChainRpcError(-32601, `the method ${method} does not exist/is not available`);
    }
  }

  async start(): Promise<void> {
    this.server = createHttpServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on('data', (c: Buffer) => chunks.push(c));
      req.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
        this.handle(body.method, body.params ?? []).then(
          (result) => {
            res.writeHead(200, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, result }));
          },
          (err: Error) => {
            const code = err instanceof ChainRpcError ? err.code : -32000;
            res.writeHead(200, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, error: { code, message: err.message } }));
          },
        );
      });
    });
    await new Promise<void>((r) => this.server!.listen(0, '127.0.0.1', r));
    const addr = this.server.address();
    if (!addr || typeof addr === 'string') throw new Error('chain server did not bind');
    this.url = `http://127.0.0.1:${addr.port}`;
  }

  async stop(): Promise<void> {
    if (this.server) await new Promise<void>((r) => this.server!.close(() => r()));
  }
}
