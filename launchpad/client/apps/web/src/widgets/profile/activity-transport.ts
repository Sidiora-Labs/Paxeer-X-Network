'use client';

import { useMemo } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { toUnsupported, unsupported, type Unsupported } from '@/core/market/unsupported';
import {
  ExplorerUnavailableError,
  fetchAddressTokenTransfers,
  type ExplorerPage,
  type ExplorerTokenTransfer,
} from '@/core/clients/explorer-api';
import { rawToDecimalString } from '@/utils/format';

export type ActivityKind = 'send' | 'receive';
export const activityKinds: readonly ActivityKind[] = ['send', 'receive'];

/** Value of the event: exact USDL for USDL transfers, or a USD estimate from the explorer's token rate. */
export type ActivityValue = { unit: 'usdl'; raw: bigint } | { unit: 'usd'; value: number };

export interface ActivityEvent {
  id: string;
  kind: ActivityKind;
  source: 'transfer';
  tokenAddress: string;
  /** Token identity as the explorer reported it; the tab overlays launchpad metadata. */
  tokenName: string | null;
  tokenSymbol: string | null;
  tokenIcon: string | null;
  decimals: number | null;
  /** Signed raw token amount: negative for send. */
  amountRaw: bigint;
  value: ActivityValue | null;
  txHash: string;
  /** Unix milliseconds. */
  occurredAt: number;
  poolAddress: string | null;
}

export const transfersOperation = 'GET /addresses/{address}/token-transfers?type=ERC-20 (Paxscan)';

/**
 * Per-wallet buys and sells have no production route: the only per-wallet
 * trade list was the legacy pnl-tracker GET /users/{address}/trades, which is
 * not served when NODE_ENV=production (its tables are absent from the
 * production migration set). The production portfolio snapshot
 * (GET /v3/wallets/{wallet}/portfolio) carries positions, not trades, and
 * GET /v2/stats/{pool}/transactions lists a pool's trades without a wallet
 * filter. The ledger therefore lists token transfers only and says so.
 */
export const tradesUnavailableNotice = "Buys and sells don't show here yet. Trades show up as the coin and USDL transfers they made.";

const USDL = CORE_DEPLOYMENT.usdl.toLowerCase();
const USDL_DECIMALS = CORE_DEPLOYMENT.usdlDecimals;

/* ----------------------------------------------------------------- paging */

/** Paxscan keyset cursor; 'first' asks for the first page. */
export type ActivityCursor = Record<string, string> | 'first';
export const firstActivityCursor: ActivityCursor = 'first';

export interface ActivityPage { rows: ExplorerTokenTransfer[]; next: Record<string, string> | null }

/** The explorer did not answer the first page; carries its real status for the unavailable state. */
export class ActivityUnavailableError extends Error {
  readonly status: number | null;
  constructor(readonly marker: Unsupported) {
    super(marker.message);
    this.name = 'ActivityUnavailableError';
    this.status = marker.status;
  }
}

function statusOf(error: unknown): number | null {
  if (error instanceof ExplorerUnavailableError) return error.status;
  const status = (error as { status?: unknown } | null)?.status;
  return typeof status === 'number' ? status : null;
}

/** One page of the wallet's ERC-20 transfers from the Paxscan explorer. */
export async function fetchActivityPage(wallet: string, cursor: ActivityCursor, signal?: AbortSignal): Promise<ActivityPage> {
  try {
    const page: ExplorerPage<ExplorerTokenTransfer> = await fetchAddressTokenTransfers(wallet.toLowerCase(), cursor === 'first' ? null : cursor, { type: 'ERC-20', signal });
    return { rows: page.items, next: page.nextPageParams };
  } catch (error) {
    if (cursor !== 'first') throw error;
    const status = statusOf(error);
    const marker = toUnsupported(transfersOperation, error) ?? (status === null || status >= 500 ? unsupported(transfersOperation, status, error instanceof Error ? error.message : 'No response') : null);
    if (marker) throw new ActivityUnavailableError(marker);
    throw error;
  }
}

/* ------------------------------------------------------------- mapping */

function parseDecimals(value: string | null | undefined): number | null {
  if (value === null || value === undefined || !/^\d{1,2}$/.test(value)) return null;
  return Number(value);
}

function isRawInteger(value: string): boolean {
  return /^\d+$/.test(value);
}

/** Explorer token rate (USD per whole token) applied to the amount; null when the rate or the scale is unknown. */
function transferValue(token: string, raw: bigint, decimals: number | null, rate: string | null): ActivityValue | null {
  if (token === USDL) return { unit: 'usdl', raw };
  if (decimals === null || rate === null) return null;
  const price = Number(rate);
  if (!Number.isFinite(price) || price <= 0) return null;
  const amount = Number(rawToDecimalString(raw, decimals));
  return Number.isFinite(amount) ? { unit: 'usd', value: amount * price } : null;
}

export function transferToEvent(transfer: ExplorerTokenTransfer, wallet: string, index: number): ActivityEvent | null {
  const self = wallet.toLowerCase();
  const from = transfer.from?.hash?.toLowerCase();
  const to = transfer.to?.hash?.toLowerCase();
  const token = transfer.token?.address?.toLowerCase();
  if (!token || transfer.token.type !== 'ERC-20' || !isRawInteger(transfer.total?.value ?? '')) return null;
  const kind: ActivityKind | null = from === self ? 'send' : to === self ? 'receive' : null;
  if (!kind) return null;
  const occurredAt = Date.parse(transfer.timestamp);
  if (!Number.isFinite(occurredAt)) return null;
  const decimals = parseDecimals(transfer.total.decimals) ?? parseDecimals(transfer.token.decimals);
  const raw = BigInt(transfer.total.value);
  if (token === USDL && decimals !== null && decimals !== USDL_DECIMALS) return null;
  return {
    id: `transfer:${transfer.tx_hash}:${transfer.log_index ?? `i${index}`}:${kind}`,
    kind,
    source: 'transfer',
    tokenAddress: token,
    tokenName: transfer.token.name?.trim() || null,
    tokenSymbol: transfer.token.symbol?.trim() || null,
    tokenIcon: transfer.token.icon_url,
    decimals: token === USDL ? USDL_DECIMALS : decimals,
    amountRaw: kind === 'send' ? -raw : raw,
    value: transferValue(token, raw, token === USDL ? USDL_DECIMALS : decimals, transfer.token.exchange_rate),
    txHash: transfer.tx_hash,
    occurredAt,
    poolAddress: null,
  };
}

/* ---------------------------------------------------------------- ledger */

export interface ActivityLedger {
  /** Transfer events, newest first, deduplicated across pages. */
  events: ActivityEvent[];
  hasMore: boolean;
  next: ActivityCursor | undefined;
}

/** Flattens the explorer pages into one newest-first ledger. */
export function mergeActivity(pages: readonly ActivityPage[], wallet: string): ActivityLedger {
  const seen = new Set<string>();
  const events: ActivityEvent[] = [];
  let index = 0;
  for (const page of pages) {
    for (const row of page.rows) {
      const event = transferToEvent(row, wallet, index++);
      if (!event || seen.has(event.id)) continue;
      seen.add(event.id);
      events.push(event);
    }
  }
  events.sort((a, b) => b.occurredAt - a.occurredAt || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  const next = pages.length ? pages[pages.length - 1].next : null;
  return { events, hasMore: !!next, next: next ?? undefined };
}

/* --------------------------------------------------------------- filters */

export type ActivityFilter = ActivityKind | 'all';

export interface ActivityIdentity { name: string | null; symbol: string | null }

/** Case-insensitive match on token name, symbol, token address or transaction hash. */
export function filterActivity(events: readonly ActivityEvent[], filter: ActivityFilter, search: string, identity: (event: ActivityEvent) => ActivityIdentity): ActivityEvent[] {
  const needle = search.trim().toLowerCase();
  return events.filter(event => {
    if (filter !== 'all' && event.kind !== filter) return false;
    if (!needle) return true;
    const { name, symbol } = identity(event);
    return [name, symbol, event.tokenAddress, event.txHash].some(value => value?.toLowerCase().includes(needle));
  });
}

/* ------------------------------------------------------------------ hook */

export interface ActivityQuery {
  state: 'loading' | 'ready' | 'unsupported' | 'error';
  ledger: ActivityLedger;
  unavailable: Unsupported | null;
  error: Error | null;
  loadingMore: boolean;
  moreError: boolean;
  loadMore(): void;
  retry(): void;
}

const emptyLedger: ActivityLedger = { events: [], hasMore: false, next: undefined };

export function useActivityLedger(wallet: string): ActivityQuery {
  const address = wallet.toLowerCase();
  const query = useInfiniteQuery({
    queryKey: queryKeys.activity(address, null),
    queryFn: ({ pageParam, signal }) => fetchActivityPage(address, pageParam, signal),
    initialPageParam: firstActivityCursor,
    getNextPageParam: last => last.next ?? undefined,
    enabled: /^0x[0-9a-f]{40}$/.test(address),
    retry: false,
    staleTime: 15_000,
  });
  const ledger = useMemo(() => (query.data ? mergeActivity(query.data.pages, address) : emptyLedger), [query.data, address]);

  if (query.status === 'pending') return { state: 'loading', ledger, unavailable: null, error: null, loadingMore: false, moreError: false, loadMore: () => undefined, retry: () => undefined };
  if (query.status === 'error') {
    const error = query.error instanceof Error ? query.error : new Error(String(query.error));
    const marker = error instanceof ActivityUnavailableError ? error.marker : null;
    return { state: marker ? 'unsupported' : 'error', ledger, unavailable: marker, error, loadingMore: false, moreError: false, loadMore: () => undefined, retry: () => { void query.refetch(); } };
  }
  return {
    state: 'ready',
    ledger,
    unavailable: null,
    error: null,
    loadingMore: query.isFetchingNextPage,
    moreError: query.isFetchNextPageError,
    loadMore: () => { if (!query.isFetchingNextPage) void query.fetchNextPage(); },
    retry: () => { void query.refetch(); },
  };
}
