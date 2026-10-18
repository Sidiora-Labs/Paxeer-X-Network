import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { explorerApiBase } from '@/core/backend-origins';

const EXPLORER_WEB = 'https://paxscan.paxeer.app';

// ── Types ────────────────────────────────────────────────────

export interface AddressCounters {
  transactionsCount: number;
  tokenTransfersCount: number;
  gasUsageCount: number;
}

export interface ExplorerAddressParam {
  hash: string;
  implementation_name?: string;
  is_contract?: boolean;
  is_verified?: boolean;
  name?: string;
  private_tags?: { label: string; display_name: string }[];
  public_tags?: { label: string; display_name: string }[];
}

export interface ExplorerTransaction {
  hash: string;
  block_number?: number;
  timestamp: string;
  from: ExplorerAddressParam;
  to: ExplorerAddressParam | null;
  value: string;
  fee: { type: string; value: string };
  method?: string;
  status?: string;
  result?: string;
  tx_types?: string[];
}

export interface ExplorerTokenTransfer {
  block_number: number;
  log_index?: number;
  from: ExplorerAddressParam;
  to: ExplorerAddressParam;
  timestamp: string;
  token: {
    address: string;
    decimals: string | null;
    exchange_rate: string | null;
    icon_url: string | null;
    name: string | null;
    symbol: string | null;
    type: string;
  };
  total: { decimals: string | null; value: string };
  tx_hash: string;
  type: string;
}

export interface ExplorerTokenInfo {
  address: string;
  circulating_market_cap?: string;
  decimals?: string;
  exchange_rate?: string;
  holders_count?: string;
  icon_url?: string;
  name?: string;
  symbol?: string;
  total_supply?: string;
  type?: string;
}

export interface ExplorerSearchItem {
  type: string;
  address?: string;
  address_hash?: string;
  name?: string;
  symbol?: string;
  icon_url?: string;
  token_url?: string;
  token_type?: string;
  total_supply?: string;
  exchange_rate?: string;
  is_smart_contract_verified?: boolean;
  url?: string;
}

export interface ExplorerPage<T> {
  items: T[];
  nextPageParams: Record<string, string> | null;
}

export type TokenTransferType = 'ERC-20' | 'ERC-721' | 'ERC-1155';

export interface TokenTransferOptions {
  /** Restrict to one token standard; the ledger uses ERC-20 only. */
  type?: TokenTransferType;
  signal?: AbortSignal;
}

/** The explorer answered with a non-2xx status, or did not answer at all (status null). */
export class ExplorerUnavailableError extends Error {
  constructor(readonly status: number | null, readonly path: string) {
    super(status === null ? `Paxscan explorer unreachable: ${path}` : `Paxscan explorer answered ${status}: ${path}`);
    this.name = 'ExplorerUnavailableError';
  }
}

// ── Fetchers ─────────────────────────────────────────────────

async function explorerFetch<T>(path: string, signal?: AbortSignal): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${explorerApiBase()}${path}`, { headers: { accept: 'application/json' }, signal, cache: 'no-store' });
  } catch (error) {
    if ((error as { name?: string })?.name === 'AbortError') throw error;
    throw new ExplorerUnavailableError(null, path);
  }
  if (!res.ok) throw new ExplorerUnavailableError(res.status, path);
  try {
    return (await res.json()) as T;
  } catch {
    throw new ExplorerUnavailableError(res.status, path);
  }
}

function withQuery(path: string, params?: Record<string, string>): string {
  if (!params) return path;
  const qs = new URLSearchParams(params).toString();
  return qs ? `${path}?${qs}` : path;
}

function toPage<T>(data: { items?: T[]; next_page_params?: Record<string, unknown> | null } | null | undefined): ExplorerPage<T> {
  const next = data?.next_page_params;
  const nextPageParams = next && typeof next === 'object'
    ? Object.fromEntries(Object.entries(next).filter(([, value]) => value !== null && value !== undefined).map(([key, value]) => [key, String(value)]))
    : null;
  return { items: Array.isArray(data?.items) ? data.items : [], nextPageParams: nextPageParams && Object.keys(nextPageParams).length > 0 ? nextPageParams : null };
}

export async function fetchAddressCounters(address: string, signal?: AbortSignal): Promise<AddressCounters> {
  const data = await explorerFetch<{
    transactions_count: string;
    token_transfers_count: string;
    gas_usage_count: string;
  }>(`/addresses/${address.toLowerCase()}/counters`, signal);
  return {
    transactionsCount: parseInt(data.transactions_count ?? '0', 10),
    tokenTransfersCount: parseInt(data.token_transfers_count ?? '0', 10),
    gasUsageCount: parseInt(data.gas_usage_count ?? '0', 10),
  };
}

export async function fetchAddressTransactions(
  address: string,
  pageParams?: Record<string, string>,
  signal?: AbortSignal,
): Promise<ExplorerPage<ExplorerTransaction>> {
  const data = await explorerFetch<{ items: ExplorerTransaction[]; next_page_params: Record<string, unknown> | null }>(
    withQuery(`/addresses/${address.toLowerCase()}/transactions`, pageParams),
    signal,
  );
  return toPage(data);
}

/** Path for one page of an address's token transfers. The page cursor from Blockscout is merged with the type filter. */
export function tokenTransfersPath(address: string, pageParams?: Record<string, string> | null, type?: TokenTransferType): string {
  const params: Record<string, string> = { ...(type ? { type } : {}), ...(pageParams ?? {}) };
  return withQuery(`/addresses/${address.toLowerCase()}/token-transfers`, Object.keys(params).length > 0 ? params : undefined);
}

/**
 * Paxscan's current Blockscout answers `transaction_hash` and `token.address_hash`;
 * older releases answered `tx_hash` and `token.address`. Both are accepted and
 * normalised to the older names; rows carrying neither are dropped.
 */
interface RawTokenTransfer extends Omit<ExplorerTokenTransfer, 'tx_hash' | 'token'> {
  tx_hash?: string;
  transaction_hash?: string;
  token: Omit<ExplorerTokenTransfer['token'], 'address'> & { address?: string; address_hash?: string };
}

function normalizeTransfer(row: RawTokenTransfer): ExplorerTokenTransfer | null {
  const txHash = row.tx_hash ?? row.transaction_hash;
  const token = row.token?.address ?? row.token?.address_hash;
  if (typeof txHash !== 'string' || typeof token !== 'string') return null;
  const { transaction_hash: _transactionHash, ...rest } = row;
  const { address_hash: _addressHash, ...tokenRest } = row.token;
  return { ...rest, tx_hash: txHash, token: { ...tokenRest, address: token } };
}

export async function fetchAddressTokenTransfers(
  address: string,
  pageParams?: Record<string, string> | null,
  options: TokenTransferOptions = {},
): Promise<ExplorerPage<ExplorerTokenTransfer>> {
  const data = await explorerFetch<{ items: RawTokenTransfer[]; next_page_params: Record<string, unknown> | null }>(
    tokenTransfersPath(address, pageParams, options.type),
    options.signal,
  );
  const page = toPage(data);
  return { items: page.items.map(normalizeTransfer).filter((row): row is ExplorerTokenTransfer => row !== null), nextPageParams: page.nextPageParams };
}

export async function fetchTokenInfo(tokenAddress: string, signal?: AbortSignal): Promise<ExplorerTokenInfo | null> {
  try {
    return await explorerFetch<ExplorerTokenInfo>(`/tokens/${tokenAddress.toLowerCase()}`, signal);
  } catch (error) {
    if (error instanceof ExplorerUnavailableError && error.status === 404) return null;
    throw error;
  }
}

export async function explorerSearch(query: string, signal?: AbortSignal): Promise<ExplorerSearchItem[]> {
  const data = await explorerFetch<{ items: ExplorerSearchItem[]; next_page_params: Record<string, unknown> | null }>(
    `/search?q=${encodeURIComponent(query)}`,
    signal,
  );
  return toPage(data).items;
}

/** Approved Paxscan transaction page for a 0x hash, or null when the hash is malformed. */
export function explorerTransactionUrl(txHash: string): string | null {
  if (!/^0x[0-9a-fA-F]{64}$/.test(txHash)) return null;
  return safeExternalDestination(`${EXPLORER_WEB}/tx/${txHash}`);
}

/** Approved Paxscan address page for a 0x address, or null when malformed. */
export function explorerAddressUrl(address: string): string | null {
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) return null;
  return safeExternalDestination(`${EXPLORER_WEB}/address/${address}`);
}
