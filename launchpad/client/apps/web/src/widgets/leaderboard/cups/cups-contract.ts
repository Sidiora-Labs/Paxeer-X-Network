import type { Operations, V2ContractTypes } from '@kindle/sdk';
import type { BadgeStatus } from '@/ui/primitives';

export type Cup = V2ContractTypes['Cup'];
export type CupStatus = Cup['status'];
export type CupPrizeTier = V2ContractTypes['CupPrizeTier'];
export type CupStanding = V2ContractTypes['CupStanding'];
export type CupCurrentResponse = V2ContractTypes['CupCurrentResponse'];
export type CupGetResponse = V2ContractTypes['CupGetResponse'];
export type CupStandingsResponse = V2ContractTypes['CupStandingsResponse'];
export type CupResultsResponse = V2ContractTypes['CupResultsResponse'];

export interface CupStandingsPage { items: CupStanding[]; nextCursor: string | null; generatedAt: string }
export interface CupResults { cup: Cup; items: CupStanding[]; generatedAt: string }

/** Contract operation ids for `GET /v2/cups/current`, `/v2/cups/{id}`, `/v2/cups/{id}/standings`, `/v2/cups/{id}/results`. */
export const cupOperations = { current: 'IF_CUP_CURRENT', get: 'IF_CUP_GET', standings: 'IF_CUP_STANDINGS', results: 'IF_CUP_RESULTS' } as const;

export const cupStandingsLimit = 100;
export const cupHighlightCount = 5;

const walletPattern = /^0x[0-9a-fA-F]{40}$/;
const unsignedPattern = /^(0|[1-9][0-9]{0,77})$/;
const signedPattern = /^-?(0|[1-9][0-9]{0,77})$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

interface Envelope { quality: 'complete' | 'partial' | 'stale' | 'quarantined'; watermark: { integrity: string } | null }

function assertEnvelope(response: Envelope, operation: string): void {
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error(`${operation} integrity quarantined`);
}

function parseTime(value: string, field: string): number {
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) throw new Error(`Cup ${field} is not a timestamp`);
  return ms;
}

/** Shape and arithmetic guards on a cup record; the server remains the authority for status. */
export function validateCup(cup: Cup): Cup {
  if (!uuidPattern.test(cup.cupId)) throw new Error('Cup identity disagreement');
  if (cup.name.trim().length === 0) throw new Error('Cup name missing');
  if (parseTime(cup.endsAt, 'endsAt') <= parseTime(cup.startsAt, 'startsAt')) throw new Error('Cup schedule disagreement');
  if (!unsignedPattern.test(cup.prizePoolRaw)) throw new Error('Cup prize pool shape disagreement');
  const ranks = new Set<number>();
  let ladder = 0n;
  for (const tier of cup.prizeLadder) {
    if (!Number.isSafeInteger(tier.rank) || tier.rank < 1 || ranks.has(tier.rank)) throw new Error('Cup prize ladder rank disagreement');
    if (!unsignedPattern.test(tier.amountRaw)) throw new Error('Cup prize ladder amount disagreement');
    ranks.add(tier.rank);
    ladder += BigInt(tier.amountRaw);
  }
  if (ladder > BigInt(cup.prizePoolRaw)) throw new Error('Cup prize ladder exceeds the prize pool');
  return cup;
}

/** Standings and results share one row shape: unique ranks and wallets, signed metric, optional unsigned prize. */
export function validateStandings(items: CupStanding[], limit: number): CupStanding[] {
  if (items.length > limit) throw new Error('Cup standings page bound exceeded');
  const wallets = new Set<string>();
  const ranks = new Set<number>();
  for (const row of items) {
    if (!walletPattern.test(row.wallet) || !signedPattern.test(row.metricSigned)) throw new Error('Cup standing shape disagreement');
    if (row.prizeRaw !== undefined && !unsignedPattern.test(row.prizeRaw)) throw new Error('Cup standing prize disagreement');
    if (!Number.isSafeInteger(row.rank) || row.rank < 1 || ranks.has(row.rank)) throw new Error('Cup standing rank disagreement');
    const wallet = row.wallet.toLowerCase();
    if (wallets.has(wallet)) throw new Error('Cup standing identity disagreement');
    wallets.add(wallet);
    ranks.add(row.rank);
  }
  return [...items].sort((a, b) => a.rank - b.rank);
}

const withSignal = (signal?: AbortSignal) => (signal ? { signal } : {});

export async function fetchCurrentCup(operations: Operations, signal?: AbortSignal): Promise<Cup | null> {
  const response = await operations.call(cupOperations.current, {}, withSignal(signal));
  assertEnvelope(response, cupOperations.current);
  return response.data.cup === null ? null : validateCup(response.data.cup);
}

export async function fetchCup(operations: Operations, cupId: string, signal?: AbortSignal): Promise<Cup> {
  const response = await operations.call(cupOperations.get, { path: { cupId } }, withSignal(signal));
  assertEnvelope(response, cupOperations.get);
  const cup = validateCup(response.data.cup);
  if (cup.cupId !== cupId) throw new Error('Cup identity disagreement');
  return cup;
}

export async function fetchCupStandings(operations: Operations, cupId: string, cursor: string | null, limit: number, signal?: AbortSignal): Promise<CupStandingsPage> {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > cupStandingsLimit) throw new RangeError('Invalid cup standings page bound');
  const response = await operations.call(cupOperations.standings, { path: { cupId }, query: { limit, ...(cursor ? { cursor } : {}) } }, withSignal(signal));
  assertEnvelope(response, cupOperations.standings);
  return { items: validateStandings(response.data.items, limit), nextCursor: response.nextCursor, generatedAt: response.generatedAt };
}

export async function fetchCupResults(operations: Operations, cupId: string, signal?: AbortSignal): Promise<CupResults> {
  const response = await operations.call(cupOperations.results, { path: { cupId } }, withSignal(signal));
  assertEnvelope(response, cupOperations.results);
  const cup = validateCup(response.data.cup);
  if (cup.cupId !== cupId) throw new Error('Cup identity disagreement');
  return { cup, items: validateStandings(response.data.items, Number.MAX_SAFE_INTEGER), generatedAt: response.generatedAt };
}

/** Badge role for a cup status (components.md 22: FINISHED danger-soft, LIVE accent, scheduled neutral). */
export function cupBadge(status: CupStatus): { status: BadgeStatus; label: string } {
  if (status === 'live') return { status: 'live', label: 'Live' };
  if (status === 'finished') return { status: 'finished', label: 'Finished' };
  return { status: 'neutral', label: 'Scheduled' };
}

/** Prize for a rank: the row's own prize when the server attached one, else the ladder tier. */
export function prizeForRank(cup: Cup, row: Pick<CupStanding, 'rank' | 'prizeRaw'>): string | null {
  if (row.prizeRaw !== undefined) return row.prizeRaw;
  return cup.prizeLadder.find(tier => tier.rank === row.rank)?.amountRaw ?? null;
}

export function ladderTotalRaw(cup: Cup): bigint {
  return cup.prizeLadder.reduce((sum, tier) => sum + BigInt(tier.amountRaw), 0n);
}

export function cupHighlights(items: readonly CupStanding[]): CupStanding[] {
  return [...items].sort((a, b) => a.rank - b.rank).slice(0, cupHighlightCount);
}

export type MetricTone = 'success' | 'danger' | 'neutral';

export function metricTone(signed: string): MetricTone {
  if (!signedPattern.test(signed)) return 'neutral';
  const value = BigInt(signed);
  return value > 0n ? 'success' : value < 0n ? 'danger' : 'neutral';
}

export const cupModeLabel: Record<Cup['mode'], string> = { solo: 'Solo cup', squad: 'Squad cup' };
export const cupScoringLabel: Record<Cup['scoring'], string> = { pnl: 'Profit made during the cup' };
