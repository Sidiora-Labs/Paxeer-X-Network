import { isRecord } from './rpc.js';
import type { AnchorStatusName, ExplorerRung, ExplorerTransactionStatus, JourneyState } from './types.js';

export type LadderRung = 'instant' | 'sealed' | 'final';
export type LadderSource = 'receipt' | 'explorer' | 'journey' | 'anchor';

export interface LadderStep {
  rung: LadderRung | null;
  source: LadderSource;
  state: string;
}

export const LADDER_RUNGS: readonly LadderRung[] = ['instant', 'sealed', 'final'];

const EXPLORER_TABLE: Readonly<Record<ExplorerRung, LadderRung | null>> = Object.freeze({
  pending: null,
  instant: 'instant',
  sealed: 'sealed',
  final: 'final',
});

const JOURNEY_TABLE: Readonly<Record<JourneyState, LadderRung | null>> = Object.freeze({
  'getting-ready': null,
  sending: null,
  processing: 'instant',
  done: 'sealed',
  'done-finalised': 'final',
  'still-checking': null,
  refused: null,
  'waiting-for-you': null,
});

const ANCHOR_TABLE: Readonly<Record<AnchorStatusName, LadderRung | null>> = Object.freeze({
  unknown: null,
  submitted: 'sealed',
  final: 'final',
});

function lookup<K extends string>(
  table: Readonly<Record<K, LadderRung | null>>,
  source: LadderSource,
  state: string,
): LadderStep {
  if (!Object.prototype.hasOwnProperty.call(table, state)) {
    throw new RangeError(`${source} state ${state} is not in the ladder table`);
  }
  return { rung: table[state as K], source, state };
}

function rank(rung: LadderRung | null): number {
  return rung === null ? -1 : LADDER_RUNGS.indexOf(rung);
}

export const statusLadder = Object.freeze({
  rungs: LADDER_RUNGS,
  tables: Object.freeze({ explorer: EXPLORER_TABLE, journey: JOURNEY_TABLE, anchor: ANCHOR_TABLE }),
  fromExplorer(status: ExplorerTransactionStatus | ExplorerRung): LadderStep {
    return lookup(EXPLORER_TABLE, 'explorer', typeof status === 'string' ? status : status.rung);
  },
  fromJourney(state: JourneyState): LadderStep {
    return lookup(JOURNEY_TABLE, 'journey', state);
  },
  fromAnchor(state: AnchorStatusName): LadderStep {
    return lookup(ANCHOR_TABLE, 'anchor', state);
  },
  highest(steps: readonly LadderStep[]): LadderStep | null {
    let best: LadderStep | null = null;
    for (const step of steps) {
      if (best === null || rank(step.rung) > rank(best.rung)) best = step;
    }
    return best;
  },
});

function nullableCount(value: unknown, what: string): number | null {
  if (value === null) return null;
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new TypeError(`malformed explorer ${what}`);
  return value;
}

export function decodeExplorerStatus(value: unknown): ExplorerTransactionStatus {
  if (!isRecord(value)) throw new TypeError('malformed explorer status');
  const rung = value.rung;
  if (typeof rung !== 'string' || !Object.prototype.hasOwnProperty.call(EXPLORER_TABLE, rung)) {
    throw new TypeError('malformed explorer rung');
  }
  const checkpoint = value.checkpoint_id;
  if (checkpoint !== null && (typeof checkpoint !== 'string' || !/^0x[0-9a-f]{64}$/.test(checkpoint))) {
    throw new TypeError('malformed explorer checkpoint_id');
  }
  return {
    rung: rung as ExplorerRung,
    block_number: nullableCount(value.block_number, 'block_number'),
    sealed_batch_number: nullableCount(value.sealed_batch_number, 'sealed_batch_number'),
    finalized_batch_number: nullableCount(value.finalized_batch_number, 'finalized_batch_number'),
    checkpoint_id: checkpoint,
  };
}

export async function readExplorerStatus(
  explorerUrl: string,
  transactionHash: string,
  fetchImpl: typeof fetch = globalThis.fetch.bind(globalThis),
): Promise<ExplorerTransactionStatus> {
  if (!/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new RangeError('transaction hash must be 32 bytes of hex');
  const base = explorerUrl.replace(/\/$/, '');
  const response = await fetchImpl(`${base}/api/v2/transactions/${transactionHash}/status`, {
    headers: { accept: 'application/json' },
    signal: AbortSignal.timeout(10_000),
    cache: 'no-store',
  });
  if (!response.ok) throw new Error(`explorer status answered HTTP ${response.status}`);
  return decodeExplorerStatus(await response.json());
}

export type TransferTruth = 'submitted' | 'pending' | 'unknown' | 'replaced' | 'reverted' | 'included';

export interface TransferIntent {
  sender: string;
  recipient: string;
  amountRaw: string;
  tokenAddress?: string;
}

export interface TransferIdentity {
  hash: string;
  chainId: number;
  intent?: TransferIntent;
}

export interface TransferEvidence {
  blockNumber: number;
  blockHash: string;
  steps: readonly LadderStep[];
}

export interface TransferObservation {
  identity: TransferIdentity;
  truth: TransferTruth;
  steps: readonly LadderStep[];
  blockNumber: number | null;
  blockHash?: string;
  sender?: string;
  nonce?: number;
  replacementHash?: string;
  lastVerified?: TransferEvidence;
  warning?: string;
}

export type TransferRpc = (method: string, params: readonly unknown[]) => Promise<unknown>;
const HASH = /^0x[0-9a-fA-F]{64}$/;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

function quantity(value: unknown, what: string): number {
  if (typeof value !== 'string' || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/.test(value)) throw new TypeError(`malformed ${what}`);
  const result = Number.parseInt(value, 16);
  if (!Number.isSafeInteger(result)) throw new TypeError(`${what} exceeds the supported integer range`);
  return result;
}

function hash(value: unknown, what: string): string {
  if (typeof value !== 'string' || !HASH.test(value)) throw new TypeError(`malformed ${what}`);
  return value.toLowerCase();
}

export function submittedTransfer(identity: TransferIdentity): TransferObservation {
  hash(identity.hash, 'transaction hash');
  if (!Number.isSafeInteger(identity.chainId) || identity.chainId <= 0) throw new RangeError('chain id must be a positive integer');
  if (identity.intent) {
    const { sender, recipient, amountRaw, tokenAddress } = identity.intent;
    if (!ADDRESS.test(sender) || !ADDRESS.test(recipient) || !/^[1-9][0-9]{0,77}$/.test(amountRaw) ||
        BigInt(amountRaw) >= (1n << 256n) || (tokenAddress !== undefined && !ADDRESS.test(tokenAddress))) {
      throw new TypeError('malformed submitted transfer intent');
    }
  }
  return { identity, truth: 'submitted', steps: [], blockNumber: null };
}

export function recoverTransferObservation(identity: TransferIdentity, value: unknown): TransferObservation {
  const submitted = submittedTransfer(identity);
  if (!isRecord(value) || !isRecord(value.identity) || value.identity.chainId !== identity.chainId ||
      typeof value.identity.hash !== 'string' || value.identity.hash.toLowerCase() !== identity.hash.toLowerCase()) return submitted;
  const evidence = isRecord(value.lastVerified) ? value.lastVerified : value;
  if (Number.isSafeInteger(evidence.blockNumber) && Number(evidence.blockNumber) >= 0 &&
      typeof evidence.blockHash === 'string' && HASH.test(evidence.blockHash) && Array.isArray(evidence.steps)) {
    const steps: LadderStep[] = [];
    for (const step of evidence.steps) {
      if (!isRecord(step) || !LADDER_RUNGS.includes(step.rung as LadderRung) ||
          (step.source !== 'receipt' && step.source !== 'explorer') || typeof step.state !== 'string') return submitted;
      steps.push({ rung: step.rung as LadderRung, source: step.source, state: step.state });
    }
    submitted.lastVerified = { blockNumber: Number(evidence.blockNumber), blockHash: evidence.blockHash, steps };
  }
  return submitted;
}

function matchesTransfer(transaction: Record<string, unknown>, intent: TransferIntent): boolean {
  if (typeof transaction.from !== 'string' || transaction.from.toLowerCase() !== intent.sender.toLowerCase() ||
      typeof transaction.to !== 'string' || typeof transaction.value !== 'string' || !/^0x[0-9a-fA-F]+$/.test(transaction.value)) return false;
  if (!intent.tokenAddress) {
    return transaction.to.toLowerCase() === intent.recipient.toLowerCase() && BigInt(transaction.value) === BigInt(intent.amountRaw);
  }
  const input = `0xa9059cbb${intent.recipient.slice(2).toLowerCase().padStart(64, '0')}${BigInt(intent.amountRaw).toString(16).padStart(64, '0')}`;
  return transaction.to.toLowerCase() === intent.tokenAddress.toLowerCase() && BigInt(transaction.value) === 0n &&
    typeof transaction.input === 'string' && transaction.input.toLowerCase() === input;
}

function hasTransferLog(receipt: Record<string, unknown>, intent: TransferIntent): boolean {
  if (!intent.tokenAddress) return true;
  const topic = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';
  const sender = `0x${intent.sender.slice(2).toLowerCase().padStart(64, '0')}`;
  const recipient = `0x${intent.recipient.slice(2).toLowerCase().padStart(64, '0')}`;
  return Array.isArray(receipt.logs) && receipt.logs.some((log) => isRecord(log) && log.removed !== true &&
    log.transactionHash === receipt.transactionHash && log.blockHash === receipt.blockHash &&
    typeof log.address === 'string' && log.address.toLowerCase() === intent.tokenAddress?.toLowerCase() &&
    Array.isArray(log.topics) && log.topics.length === 3 && log.topics.every((value) => typeof value === 'string') && log.topics[0]?.toLowerCase() === topic &&
    log.topics[1]?.toLowerCase() === sender && log.topics[2]?.toLowerCase() === recipient &&
    typeof log.data === 'string' && /^0x[0-9a-fA-F]{64}$/.test(log.data) && BigInt(log.data) === BigInt(intent.amountRaw));
}

export async function observeTransfer(
  rpc: TransferRpc,
  previous: TransferObservation,
  explorer?: { url: string; fetchImpl?: typeof fetch },
): Promise<TransferObservation> {
  const { identity } = submittedTransfer(previous.identity);
  const chainId = quantity(await rpc('eth_chainId', []), 'eth_chainId');
  if (chainId !== identity.chainId) throw new Error(`connected to chain ${chainId}, transfer was submitted on chain ${identity.chainId}`);
  const unknown = (): TransferObservation => ({ ...previous, identity, truth: 'unknown', steps: [], blockNumber: null, blockHash: undefined });
  const receipt = await rpc('eth_getTransactionReceipt', [identity.hash]);
  if (receipt !== null) {
    if (!isRecord(receipt) || hash(receipt.transactionHash, 'receipt transactionHash') !== identity.hash.toLowerCase()) {
      throw new TypeError('receipt does not identify the submitted transaction');
    }
    const blockNumber = quantity(receipt.blockNumber, 'receipt blockNumber');
    const blockHash = hash(receipt.blockHash, 'receipt blockHash');
    if (receipt.status !== '0x0' && receipt.status !== '0x1') throw new TypeError('malformed receipt status');
    const canonical = await rpc('eth_getBlockByNumber', [receipt.blockNumber, false]);
    if (canonical === null) return unknown();
    if (!isRecord(canonical) || quantity(canonical.number, 'canonical block number') !== blockNumber) throw new TypeError('malformed canonical block');
    if (hash(canonical.hash, 'canonical block hash') !== blockHash) return unknown();
    if (!Array.isArray(canonical.transactions) || !canonical.transactions.some((transaction) =>
      typeof transaction === 'string' && transaction.toLowerCase() === identity.hash.toLowerCase())) {
      throw new TypeError('canonical block does not contain the submitted transaction');
    }
    if (receipt.status === '0x0') return { identity, truth: 'reverted', steps: [], blockNumber, blockHash, lastVerified: previous.lastVerified };
    let sender = previous.sender;
    let nonce = previous.nonce;
    if (identity.intent) {
      const transaction = await rpc('eth_getTransactionByHash', [identity.hash]);
      if (!isRecord(transaction) || hash(transaction.hash, 'included transaction hash') !== identity.hash.toLowerCase() ||
          !matchesTransfer(transaction, identity.intent) || !hasTransferLog(receipt, identity.intent)) {
        return { ...unknown(), warning: 'Successful execution has no matching transfer evidence' };
      }
      sender = identity.intent.sender;
      nonce = quantity(transaction.nonce, 'included transaction nonce');
    }
    const steps: LadderStep[] = [{ rung: 'instant', source: 'receipt', state: `success:${blockHash}` }];
    let warning: string | undefined;
    if (explorer) {
      try {
        const status = await readExplorerStatus(explorer.url, identity.hash, explorer.fetchImpl);
        if (status.block_number !== blockNumber) throw new Error('explorer status does not identify the receipt block');
        if ((status.rung === 'sealed' || status.rung === 'final') &&
            (status.sealed_batch_number === null || status.checkpoint_id === null)) throw new Error('explorer sealing evidence is incomplete');
        if (status.rung === 'final' && (status.finalized_batch_number === null || status.sealed_batch_number === null ||
            status.finalized_batch_number > status.sealed_batch_number)) throw new Error('explorer finality evidence is incomplete');
        const step = statusLadder.fromExplorer(status);
        if (step.rung !== null) steps.push({ ...step, state: `${status.rung}:${status.checkpoint_id ?? blockHash}` });
      } catch (error) {
        warning = error instanceof Error ? error.message : 'Explorer evidence unavailable';
      }
    }
    return { identity, truth: 'included', steps, blockNumber, blockHash, warning, sender, nonce,
      lastVerified: previous.lastVerified?.blockHash === blockHash &&
        rank(statusLadder.highest(previous.lastVerified.steps)?.rung ?? null) > rank(statusLadder.highest(steps)?.rung ?? null)
        ? previous.lastVerified : { blockNumber, blockHash, steps } };
  }
  const tx = await rpc('eth_getTransactionByHash', [identity.hash]);
  let known = previous.sender !== undefined && previous.nonce !== undefined
    ? { sender: previous.sender, nonce: previous.nonce } : null;
  if (tx !== null) {
    if (!isRecord(tx) || hash(tx.hash, 'transaction hash') !== identity.hash.toLowerCase() ||
        typeof tx.from !== 'string' || !ADDRESS.test(tx.from)) throw new TypeError('malformed submitted transaction');
    if (identity.intent && !matchesTransfer(tx, identity.intent)) throw new TypeError('transaction differs from the submitted transfer');
    known = { sender: tx.from, nonce: quantity(tx.nonce, 'transaction nonce') };
    if (tx.blockNumber !== null) return { ...unknown(), ...known };
  }
  if (known !== null) {
    const confirmed = quantity(await rpc('eth_getTransactionCount', [known.sender, 'latest']), 'account nonce');
    if (confirmed > known.nonce) {
      const tag = previous.truth === 'replaced' && previous.blockNumber !== null ? `0x${previous.blockNumber.toString(16)}` : 'latest';
      const block = await rpc('eth_getBlockByNumber', [tag, true]);
      if (!isRecord(block) || !Array.isArray(block.transactions)) throw new TypeError('malformed replacement block');
      const blockHash = hash(block.hash, 'replacement block hash');
      const blockNumber = quantity(block.number, 'replacement block number');
      for (const replacement of block.transactions) {
        if (isRecord(replacement) && typeof replacement.from === 'string' &&
            replacement.from.toLowerCase() === known.sender.toLowerCase() && quantity(replacement.nonce, 'replacement nonce') === known.nonce &&
            hash(replacement.hash, 'replacement hash') !== identity.hash.toLowerCase() &&
            hash(replacement.blockHash, 'replacement transaction block') === blockHash) {
          return { identity, truth: 'replaced', steps: [], blockNumber, blockHash, ...known,
            replacementHash: hash(replacement.hash, 'replacement hash'), lastVerified: previous.lastVerified };
        }
      }
      return { ...unknown(), ...known };
    }
  }
  return { identity, truth: tx === null ? 'unknown' : 'pending', steps: [], blockNumber: null,
    ...(known ?? {}), lastVerified: previous.lastVerified };
}
