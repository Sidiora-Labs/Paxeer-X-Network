import {
  createPublicClient,
  http,
  encodeFunctionData,
  parseAbiItem,
  type AbiFunction,
  type Hex,
  type PublicClient,
} from 'viem';
import { hyperPaxeer } from './chain.js';
import { env } from './env.js';

/**
 * Read-only chain access for the agent lane.
 *
 * This module exposes a KEYLESS public client so balance / allowance / nonce /
 * simulation reads need no signing key. All helpers are pure reads — they never
 * sign or send.
 */

let cached: PublicClient | null = null;

export function publicClient(): PublicClient {
  if (cached) return cached;
  cached = createPublicClient({
    chain: hyperPaxeer,
    transport: http(env.HYPERPAXEER_RPC_URL),
  });
  return cached;
}

// Minimal ERC-20 ABI — only the views + writes the agent lane encodes/reads.
export const ERC20_ABI = [
  {
    type: 'function',
    name: 'balanceOf',
    stateMutability: 'view',
    inputs: [{ name: 'owner', type: 'address' }],
    outputs: [{ name: '', type: 'uint256' }],
  },
  {
    type: 'function',
    name: 'allowance',
    stateMutability: 'view',
    inputs: [
      { name: 'owner', type: 'address' },
      { name: 'spender', type: 'address' },
    ],
    outputs: [{ name: '', type: 'uint256' }],
  },
  {
    type: 'function',
    name: 'decimals',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'uint8' }],
  },
  {
    type: 'function',
    name: 'symbol',
    stateMutability: 'view',
    inputs: [],
    outputs: [{ name: '', type: 'string' }],
  },
  {
    type: 'function',
    name: 'transfer',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'to', type: 'address' },
      { name: 'amount', type: 'uint256' },
    ],
    outputs: [{ name: '', type: 'bool' }],
  },
  {
    type: 'function',
    name: 'approve',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'spender', type: 'address' },
      { name: 'amount', type: 'uint256' },
    ],
    outputs: [{ name: '', type: 'bool' }],
  },
] as const;

/** Encode `transfer(to, amount)` calldata. */
export function encodeErc20Transfer(to: `0x${string}`, amount: bigint): Hex {
  return encodeFunctionData({ abi: ERC20_ABI, functionName: 'transfer', args: [to, amount] });
}

/** Encode `approve(spender, amount)` calldata. */
export function encodeErc20Approve(spender: `0x${string}`, amount: bigint): Hex {
  return encodeFunctionData({ abi: ERC20_ABI, functionName: 'approve', args: [spender, amount] });
}

/** Native PAX balance (wei). */
export async function getNativeBalance(address: `0x${string}`): Promise<bigint> {
  return publicClient().getBalance({ address });
}

/** ERC-20 token balance (raw units). */
export async function getErc20Balance(
  token: `0x${string}`,
  owner: `0x${string}`,
): Promise<bigint> {
  return publicClient().readContract({
    address: token,
    abi: ERC20_ABI,
    functionName: 'balanceOf',
    args: [owner],
  }) as Promise<bigint>;
}

/** ERC-20 allowance owner->spender (raw units). */
export async function getErc20Allowance(
  token: `0x${string}`,
  owner: `0x${string}`,
  spender: `0x${string}`,
): Promise<bigint> {
  return publicClient().readContract({
    address: token,
    abi: ERC20_ABI,
    functionName: 'allowance',
    args: [owner, spender],
  }) as Promise<bigint>;
}

export interface Erc20Metadata {
  decimals: number | null;
  symbol: string | null;
}

/** Best-effort token metadata. Either field may be null on a non-standard token. */
export async function getErc20Metadata(token: `0x${string}`): Promise<Erc20Metadata> {
  const pc = publicClient();
  const [decimals, symbol] = await Promise.all([
    pc
      .readContract({ address: token, abi: ERC20_ABI, functionName: 'decimals' })
      .then((d) => Number(d))
      .catch(() => null),
    pc
      .readContract({ address: token, abi: ERC20_ABI, functionName: 'symbol' })
      .then((s) => String(s))
      .catch(() => null),
  ]);
  return { decimals, symbol };
}

/** Pending nonce for an address (transaction count incl. mempool). */
export async function getNonce(address: `0x${string}`): Promise<number> {
  return publicClient().getTransactionCount({ address, blockTag: 'pending' });
}

/**
 * Latest-vs-pending nonce gap for an address. A gap means unmined
 * transactions sit in the mempool; if the oldest of them never mines
 * (underpriced / dropped), EVERY later send from this wallet queues behind it
 * and the wallet is wedged until the gap nonce is filled or replaced.
 */
export async function getNonceGap(
  address: `0x${string}`,
): Promise<{ latest: number; pending: number; stuck: number }> {
  const pc = publicClient();
  const [latest, pending] = await Promise.all([
    pc.getTransactionCount({ address, blockTag: 'latest' }),
    pc.getTransactionCount({ address, blockTag: 'pending' }),
  ]);
  return { latest, pending, stuck: Math.max(0, pending - latest) };
}
/** Current gas price (wei). */
export async function getGasPrice(): Promise<bigint> {
  return publicClient().getGasPrice();
}

export interface ResolveGasRequest {
  from: `0x${string}`;
  to?: `0x${string}`;
  data?: Hex;
  value?: bigint;
  /** Caller overrides — when present they are honoured verbatim and not estimated. */
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
}

export interface ResolvedGas {
  gas: bigint;
  maxFeePerGas: bigint;
  maxPriorityFeePerGas: bigint;
}

/** Headroom on an estimated gas LIMIT so a tx isn't starved if a block fills
 *  between estimate and inclusion. Fees already carry viem's baseFee multiplier. */
function withGasHeadroom(gas: bigint): bigint {
  return (gas * 120n) / 100n;
}

/**
 * Resolve the gas limit + EIP-1559 fees for a single transaction from the live
 * chain, per transaction. The Paxeer chain (125) exposes baseFeePerGas,
 * eth_maxPriorityFeePerGas and eth_feeHistory, so we estimate dynamically
 * instead of hard-coding — while still honouring any value the caller pins.
 *
 * Used by every signing path so a sign-only request (which never round-trips
 * the RPC inside viem) gets the same RPC-derived gas as a broadcast.
 */
export async function resolveGas(req: ResolveGasRequest): Promise<ResolvedGas> {
  const pc = publicClient();
  const needFees = req.maxFeePerGas === undefined || req.maxPriorityFeePerGas === undefined;

  const [fees, gasLimit] = await Promise.all([
    needFees ? pc.estimateFeesPerGas() : Promise.resolve(null),
    req.gas !== undefined
      ? Promise.resolve(req.gas)
      : pc
          .estimateGas({ account: req.from, to: req.to, data: req.data, value: req.value })
          .then(withGasHeadroom),
  ]);

  return {
    gas: gasLimit,
    maxFeePerGas: req.maxFeePerGas ?? fees!.maxFeePerGas,
    maxPriorityFeePerGas: req.maxPriorityFeePerGas ?? fees!.maxPriorityFeePerGas,
  };
}

export interface SimRequest {
  from: `0x${string}`;
  to?: `0x${string}`;
  data?: Hex;
  value?: bigint;
}

export interface SimResult {
  /** Whether the call would succeed (no revert) at the current head. */
  ok: boolean;
  /** Gas estimate when ok; null when the call reverts or estimation fails. */
  gas: bigint | null;
  /** Raw return data from eth_call when ok. */
  returnData: Hex | null;
  /** Revert / failure reason when not ok. */
  error: string | null;
}

/**
 * Dry-run a transaction against the live head WITHOUT signing or spending:
 *   1. eth_call to learn the return data / detect a revert,
 *   2. estimateGas for a cost preview.
 * Returns a structured result so the agent can decide before committing.
 */
export async function simulate(req: SimRequest): Promise<SimResult> {
  const pc = publicClient();
  try {
    const callRes = await pc.call({
      account: req.from,
      to: req.to,
      data: req.data,
      value: req.value,
    });
    let gas: bigint | null = null;
    try {
      gas = await pc.estimateGas({
        account: req.from,
        to: req.to,
        data: req.data,
        value: req.value,
      });
    } catch {
      // Call succeeded but estimateGas hiccuped — still a "would succeed".
      gas = null;
    }
    return { ok: true, gas, returnData: (callRes.data as Hex) ?? null, error: null };
  } catch (err) {
    return {
      ok: false,
      gas: null,
      returnData: null,
      error: err instanceof Error ? err.message : 'call_failed',
    };
  }
}

export interface TxReceiptSummary {
  status: 'success' | 'reverted';
  block_number: string;
  gas_used: string;
  tx_hash: Hex;
}

/** Fetch a receipt if mined; null if still pending / unknown. */
export async function getReceipt(hash: Hex): Promise<TxReceiptSummary | null> {
  try {
    const r = await publicClient().getTransactionReceipt({ hash });
    return {
      status: r.status,
      block_number: r.blockNumber.toString(),
      gas_used: r.gasUsed.toString(),
      tx_hash: r.transactionHash,
    };
  } catch {
    return null;
  }
}

// -----------------------------------------------------------------------------
// Confirmation + liveness helpers for the durable-action orchestrator.
// -----------------------------------------------------------------------------

export type ReceiptOutcome =
  | { state: 'confirmed'; receipt: TxReceiptSummary }
  | { state: 'reverted'; receipt: TxReceiptSummary }
  | { state: 'pending' } // broadcast + still in mempool (proven live)
  | { state: 'unknown' } // no receipt AND not in mempool — ambiguous
  | { state: 'timeout' }; // gave up waiting; caller parks in `reconciling`

/**
 * Poll for a tx receipt until it either reaches `confirmations` blocks of depth,
 * reverts, or the deadline elapses. Never throws — every terminal-or-not
 * condition is a returned discriminated state so the orchestrator can persist a
 * precise phase/code instead of interpreting a raw RPC error.
 *
 * On timeout the caller must NOT resend; it parks the action in `reconciling`
 * and lets the reconciler keep polling (the tx may yet mine).
 */
export async function waitForReceipt(
  hash: Hex,
  opts?: { confirmations?: number; timeoutMs?: number; pollMs?: number },
): Promise<ReceiptOutcome> {
  const confirmations = Math.max(1, opts?.confirmations ?? env.ACTION_CONFIRMATIONS);
  const timeoutMs = opts?.timeoutMs ?? env.ACTION_RECEIPT_TIMEOUT_MS;
  const pollMs = opts?.pollMs ?? 1_500;
  const pc = publicClient();
  const deadline = Date.now() + timeoutMs;

  for (;;) {
    const receipt = await getReceipt(hash);
    if (receipt) {
      if (receipt.status === 'reverted') return { state: 'reverted', receipt };
      // Confirmed: ensure enough depth so a 1-block reorg can't un-mine it.
      if (confirmations <= 1) return { state: 'confirmed', receipt };
      const head = await pc.getBlockNumber().catch(() => null);
      if (head !== null && head - BigInt(receipt.block_number) + 1n >= BigInt(confirmations)) {
        return { state: 'confirmed', receipt };
      }
    }
    if (Date.now() >= deadline) {
      // Distinguish "still live in mempool" from "vanished" so a caller can
      // decide between poll_action and reconcile.
      if (!receipt) {
        const live = await isTxKnown(hash);
        return live ? { state: 'pending' } : { state: 'timeout' };
      }
      return { state: 'timeout' };
    }
    await sleep(pollMs);
  }
}

/** True when the node still knows the tx (mined OR in mempool). */
export async function isTxKnown(hash: Hex): Promise<boolean> {
  const pc = publicClient();
  try {
    await pc.getTransaction({ hash });
    return true;
  } catch {
    // getTransaction throws when the node has never seen (or dropped) the tx.
    const r = await getReceipt(hash);
    return r !== null;
  }
}

/**
 * Encode an arbitrary function call from a human method signature + string args.
 * Supports the generic allowance-and-call route: e.g.
 *   encodeCall('depositUSDL(uint256,bytes32)', ['250000000', '0xabc…'])
 * Numeric/bytes/address args are coerced to the types viem expects. Throws on a
 * malformed signature or arg (surfaced as invalid_request, never a silent send).
 */
export function encodeCall(signature: string, args: string[]): { data: Hex; selector: string } {
  const item = parseAbiItem(`function ${signature}`) as AbiFunction;
  const coerced = item.inputs.map((input, i) => coerceArg(input.type, args[i]));
  const data = encodeFunctionData({ abi: [item], functionName: item.name, args: coerced });
  return { data, selector: data.slice(0, 10) };
}

function coerceArg(type: string, raw: string | undefined): unknown {
  if (raw === undefined) throw new Error(`missing argument for ${type}`);
  if (type.endsWith('[]')) {
    const inner = type.slice(0, -2);
    const parsed = JSON.parse(raw) as unknown[];
    return parsed.map((v) => coerceArg(inner, String(v)));
  }
  if (type.startsWith('uint') || type.startsWith('int')) return BigInt(raw);
  if (type === 'bool') return raw === 'true' || raw === '1';
  // address, bytes, bytesN, string all pass through as-is.
  return raw;
}

const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));
