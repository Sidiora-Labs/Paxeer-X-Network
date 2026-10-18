import { FetchPolicyError, fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";
/**
 * Typed REST client for the public SPOT data API at `https://api.paxeer-network.xyz`.
 *
 * Every numeric field that comes back from this service is a **decimal
 * string** (raw units, not floats). The shapes here mirror `swagger_spot.json`
 * exactly — callers convert to `number` / `bigint` at the edge where they
 * decide on a precision policy.
 *
 * Address-keyed endpoints lower-case the address before sending so the
 * server's case-insensitive cache keys match.
 */

const SPOT_API_BASE =
  (typeof process !== "undefined" && process.env?.NEXT_PUBLIC_SPOT_API_BASE) ||
  "https://api.paxeer-network.xyz";

// ── Decimal string helpers ────────────────────────────────────────────

/**
 * Convert a swagger "decimal string" to JS number. Loses precision past
 * ~15 digits — only use for display, never for sums or comparisons.
 */
export function dStr(v: string | null | undefined, fallback = 0): number {
  if (v == null) return fallback;
  const n = Number(v);
  return Number.isFinite(n) ? n : fallback;
}

/**
 * Convert a swagger "raw token amount" (decimal string in token's smallest
 * unit) to a display float using the token's `decimals` count.
 */
export function dStrToToken(
  raw: string | null | undefined,
  decimals: number | null | undefined,
  fallback = 0,
): number {
  if (raw == null) return fallback;
  const n = Number(raw);
  if (!Number.isFinite(n)) return fallback;
  const d = typeof decimals === "number" && decimals >= 0 ? decimals : 0;
  return n / Math.pow(10, d);
}

/**
 * Convert a USD raw amount (6-decimal stable) to dollars. The SPOT API
 * uses 6-decimal precision for `*Usd` fields per swagger.
 */
export function dStrToUsd(raw: string | null | undefined, fallback = 0): number {
  if (raw == null) return fallback;
  const n = Number(raw);
  if (!Number.isFinite(n)) return fallback;
  return n / 1_000_000;
}

// ── Response types ────────────────────────────────────────────────────

export interface SpotHoldingsResponse {
  address: string;
  native: {
    symbol: "PAX";
    decimals: 18;
    balance: string;
  };
  erc20: Array<{
    token: string;
    balance: string;
    blockNumber: string | null;
    symbol: string | null;
    name: string | null;
    decimals: number | string | null;
    holderCount: number | null;
    type: string;
  }>;
}

export interface SpotPosition {
  token: string;
  symbol: string | null;
  name: string | null;
  decimals: number | null;
  isStablecoin: boolean;
  derived: {
    qty: string;
    avgEntryPrice: string;
    avgExitPrice: string | null;
    totalBought: string;
    totalSold: string;
    totalCostBasis: string;
    totalProceeds: string;
    realizedPnl: string;
    realizedPnlUsd: string;
    tradeCount: number;
    firstTradeAt: string | null;
    lastTradeAt: string | null;
  };
  paxscan: { qty: string };
  externalActivity: boolean;
  externalQtyDelta: string;
}

export interface SpotPositionsResponse {
  address: string;
  positions: SpotPosition[];
}

export type SpotOrderType = "limit" | "stop_limit" | "stop_loss";
export type SpotOrderStatus = "open" | "executed" | "cancelled" | "activated";

export interface SpotOrder {
  id: number | string;
  orderId: string;
  type: SpotOrderType;
  status: SpotOrderStatus;
  token: string | null;
  stablecoin: string | null;
  amount: string | null;
  targetPrice: string | null;
  stopPrice: string | null;
  limitPrice: string | null;
  triggerPrice: string | null;
  executionPrice: string | null;
  isBuy: boolean | null;
  placedAt: string | null;
  executedAt: string | null;
  cancelledAt: string | null;
  placedTxHash: string | null;
  executedTxHash: string | null;
  symbol: string | null;
  decimals: number | null;
}

export interface SpotOrdersResponse {
  address: string;
  count: number;
  items: SpotOrder[];
}

export interface SpotTrade {
  id: number | string;
  txHash: string;
  logIndex: number;
  blockNumber: string | number;
  blockTimestamp: string;
  user: string;
  tokenIn: string;
  tokenOut: string;
  amountIn: string;
  amountOut: string;
  executionPrice: string;
  volumeUsd: string;
  source: string;
  contract: string;
  createdAt: string;
  feeAmount?: string | null;
  feeBps?: number | null;
  hops?: number | null;
  priceImpactAmount?: string | null;
  priceImpactBps?: number | null;
  routeAdapterId?: string | null;
  twapPrice?: string | null;
}

export interface SpotTradesResponse {
  address: string;
  count: number;
  limit: number;
  items: SpotTrade[];
  nextCursor: { afterBlock: string; afterIndex: number } | null;
}

export interface SpotClosedLeg {
  id: number;
  token: string;
  qty: string;
  entryPrice: string;
  exitPrice: string;
  pnl: string;
  pnlUsd: string;
  pnlPct: string | null;
  holdingSeconds: number | string | null;
  openedAt: string | null;
  closedAt: string;
  closeTxHash: string;
  fullyClosed: boolean;
  symbol: string | null;
  name: string | null;
  decimals: number | null;
}

export interface SpotClosedLegsResponse {
  address: string;
  count: number;
  limit: number;
  items: SpotClosedLeg[];
  nextCursor: { afterId: number } | null;
}

export interface SpotPnlSummary {
  address: string;
  realizedUsd: string;
  unrealizedUsd?: string;
  totalUsd?: string;
  tradeCount?: number;
  closedLegCount?: number;
  // Server may return additional aggregate fields — keep open.
  [key: string]: unknown;
}

export interface SpotIdentity {
  address: string;
  // Server keeps adding fields — keep this loose. Common ones below.
  paxscan?: unknown;
  trading24h?: unknown;
  latestSnapshot?: unknown;
  [key: string]: unknown;
}

export interface SpotTradesStats {
  address: string;
  totalTrades?: number;
  bySource?: Record<string, number>;
  byContract?: Record<string, number>;
  [key: string]: unknown;
}

// ── Error type ────────────────────────────────────────────────────────

export class SpotApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.name = "SpotApiError";
    this.status = status;
    this.body = body;
  }
}

// ── Internal fetch helper ─────────────────────────────────────────────

async function getJson<T>(path: string, signal?: AbortSignal): Promise<T> {
  const url = `${SPOT_API_BASE}${path}`;
  let res: Response;
  try {
    res = await fetch(url, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal,
    });
  } catch (err) {
    if (err instanceof FetchPolicyError) throw err;
    throw new SpotApiError(0, `[spot] network error: ${(err as Error).message}`, null);
  }
  let body: unknown = null;
  try {
    body = await res.json();
  } catch (error) {
    throw error;
  }
  if (!res.ok) {
    const msg =
      (body && typeof body === "object" && "error" in body && typeof (body as { error?: unknown }).error === "string"
        ? String((body as { error: string }).error)
        : `HTTP ${res.status}`);
    throw new SpotApiError(res.status, `[spot] ${path} → ${msg}`, body);
  }
  return body as T;
}

function lc(addr: string): string {
  return addr.toLowerCase();
}

// ── Public API ────────────────────────────────────────────────────────

export const spotApi = {
  base: SPOT_API_BASE,

  /** Live ERC-20 + native PAX balances, PAXSCAN-truthed. */
  getHoldings(address: string, signal?: AbortSignal): Promise<SpotHoldingsResponse> {
    return getJson(`/v1/users/${lc(address)}/holdings`, signal);
  },

  /** Derived per-token positions (qty / WAC entry / realized PnL). */
  getPositions(address: string, signal?: AbortSignal): Promise<SpotPositionsResponse> {
    return getJson(`/v1/users/${lc(address)}/positions`, signal);
  },

  /**
   * Limit / stop-limit / stop-loss orders.
   * `status="open"` returns only open + activated orders (default `"all"` returns full history).
   */
  getOrders(
    address: string,
    opts?: { status?: "open" | "all"; limit?: number; signal?: AbortSignal },
  ): Promise<SpotOrdersResponse> {
    const params = new URLSearchParams();
    if (opts?.status) params.set("status", opts.status);
    if (opts?.limit) params.set("limit", String(opts.limit));
    const qs = params.toString();
    return getJson(`/v1/users/${lc(address)}/orders${qs ? `?${qs}` : ""}`, opts?.signal);
  },

  /** Keyset-paginated trade tape. */
  getTrades(
    address: string,
    opts?: {
      limit?: number;
      afterBlock?: string;
      afterIndex?: number;
      signal?: AbortSignal;
    },
  ): Promise<SpotTradesResponse> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.afterBlock) params.set("afterBlock", opts.afterBlock);
    if (opts?.afterIndex != null) params.set("afterIndex", String(opts.afterIndex));
    const qs = params.toString();
    return getJson(`/v1/users/${lc(address)}/trades${qs ? `?${qs}` : ""}`, opts?.signal);
  },

  /** Aggregate trade statistics. */
  getTradesStats(address: string, signal?: AbortSignal): Promise<SpotTradesStats> {
    return getJson(`/v1/users/${lc(address)}/trades/stats`, signal);
  },

  /** Closed legs (realized PnL slices, keyset-paginated). */
  getClosedLegs(
    address: string,
    opts?: { limit?: number; afterId?: number; signal?: AbortSignal },
  ): Promise<SpotClosedLegsResponse> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.afterId != null) params.set("afterId", String(opts.afterId));
    const qs = params.toString();
    return getJson(`/v1/users/${lc(address)}/closed-legs${qs ? `?${qs}` : ""}`, opts?.signal);
  },

  /** Compact PnL summary card. */
  getPnlSummary(address: string, signal?: AbortSignal): Promise<SpotPnlSummary> {
    return getJson(`/v1/users/${lc(address)}/pnl/summary`, signal);
  },

  /** Full PnL series. */
  getPnl(address: string, signal?: AbortSignal): Promise<unknown> {
    return getJson(`/v1/users/${lc(address)}/pnl`, signal);
  },

  /** Trader identity dossier (paxscan + 24h activity + snapshot). */
  getIdentity(address: string, signal?: AbortSignal): Promise<SpotIdentity> {
    return getJson(`/v1/users/${lc(address)}/identity`, signal);
  },

  /** Leaderboards. */
  getLeaderboard(
    kind: "pnl" | "volume" | "win-rate",
    signal?: AbortSignal,
  ): Promise<unknown> {
    return getJson(`/v1/leaderboards/${kind}`, signal);
  },
};

// ── Field-level helpers used by widgets ──────────────────────────────

/**
 * Resolve a position's USD valuation given a current price feed. Used by
 * the holdings table and total-value computations.
 */
export function positionUsdValue(p: SpotPosition, currentPrice: number): number {
  const qty = dStrToToken(p.derived.qty, p.decimals);
  return qty > 0 && currentPrice > 0 ? qty * currentPrice : 0;
}

/**
 * Best-effort symbol resolution: prefer the API-provided `symbol`, fall
 * back to a short address if missing.
 */
export function symbolOrShort(token: string, symbol: string | null | undefined): string {
  if (symbol && symbol.length > 0) return symbol;
  return `${token.slice(0, 6)}…${token.slice(-4)}`;
}
