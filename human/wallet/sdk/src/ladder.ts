import { isRecord } from './rpc.js';
import type { AnchorStatusName, ExplorerRung, ExplorerTransactionStatus, JourneyState } from './types.js';

export type LadderRung = 'instant' | 'sealed' | 'final';
export type LadderSource = 'explorer' | 'journey' | 'anchor';

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
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 0) throw new TypeError(`malformed explorer ${what}`);
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
  });
  if (!response.ok) throw new Error(`explorer status answered HTTP ${response.status}`);
  return decodeExplorerStatus(await response.json());
}
