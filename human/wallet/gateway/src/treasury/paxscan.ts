import { z } from 'zod';
import { env } from '../env.js';

/**
 * Paxscan (Blockscout-based) explorer client.
 *
 * The funded-account evaluator uses this to value a wallet's full holdings
 * (native PAX + every ERC-20) in USD via a single HTTP call to Paxscan,
 * which already indexes every balance with a live exchange_rate.
 *
 * Why Paxscan and not direct RPC reads:
 *   - The previous USDL-only RPC valuation wrongly breached any user who
 *     swapped USDL → SID (or any other spot token) because the evaluator
 *     saw "USDL = 0" as "$0 equity".
 *   - Paxscan returns USD-priced balances for every token in one shot,
 *     making spot-trading-transparent valuation a 2-call problem instead of
 *     N-RPC-reads-with-N-decimal-conversions-and-N-oracle-reads.
 *   - Paxscan reads from its own indexed Postgres, not the load-balanced
 *     RPC, so it sidesteps the cross-region-lag class of bugs entirely.
 *
 * What Paxscan does NOT cover:
 *   - Open perp positions on the Diamond (collateral is held as internal
 *     accounting, not an ERC-20 balance, so Blockscout cannot see it).
 *     A Stage-2 QuoterFacet adapter in jobs/fundedEvaluator.ts will add
 *     this later. Paxscan + perp adapter together = full equity coverage.
 */

// -----------------------------------------------------------------------------
// Response schemas — only the fields we need; everything else passes through.
// We use safeParse so a Paxscan schema change surfaces as a typed error
// instead of a silent NaN somewhere in the equity math.
// -----------------------------------------------------------------------------

const AddressInfoSchema = z
  .object({
    coin_balance: z.string().nullable().optional(),
    exchange_rate: z.string().nullable().optional(),
  })
  .passthrough();

const TokenSchema = z
  .object({
    decimals: z.string().nullable().optional(),
    exchange_rate: z.string().nullable().optional(),
    address_hash: z.string().optional(),
    symbol: z.string().nullable().optional(),
  })
  .passthrough();

const TokenBalanceSchema = z
  .object({
    value: z.string().nullable().optional(),
    token: TokenSchema.nullable().optional(),
  })
  .passthrough();

const TokenBalancesSchema = z.array(TokenBalanceSchema);

type TokenBalance = z.infer<typeof TokenBalanceSchema>;

// -----------------------------------------------------------------------------
// In-process cache + inflight coalescing
// -----------------------------------------------------------------------------
//
// The evaluator runs on a ~10s tick. Across cluster workers the same wallet
// can be evaluated multiple times in quick succession (e.g. evaluator + a
// /v1/funded/status endpoint hit). A short TTL cache collapses the redundant
// fetches, and an inflight map ensures concurrent first-misses share one HTTP.

interface CacheEntry {
  ts: number;
  equityUsd: number;
}

const cache = new Map<string, CacheEntry>();
const inflight = new Map<string, Promise<number>>();

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

export class PaxscanError extends Error {
  constructor(message: string, override readonly cause?: unknown) {
    super(message);
    this.name = 'PaxscanError';
  }
}

/**
 * Total USD equity for a wallet, summed across native PAX and every indexed
 * ERC-20 holding.
 *
 * Tokens with null/zero/non-numeric exchange_rate are treated as $0 — Paxscan
 * marks them this way for unpriced or scam-flagged tokens, and counting them
 * would let a user inflate equity by holding a worthless token whose oracle
 * round-trips to a stale price.
 *
 * Cached for env.PAXSCAN_CACHE_TTL_MS. Throws PaxscanError on transport or
 * schema failure — callers MUST decide whether to skip the tick or fall back;
 * never default to $0 on error (would wrongly breach the account).
 */
export async function getWalletEquityUsd(
  walletAddress: `0x${string}`,
): Promise<number> {
  const key = walletAddress.toLowerCase();
  const now = Date.now();

  const hit = cache.get(key);
  if (hit && now - hit.ts < env.PAXSCAN_CACHE_TTL_MS) return hit.equityUsd;

  const existing = inflight.get(key);
  if (existing) return existing;

  const promise = (async () => {
    try {
      const [info, tokens] = await Promise.all([
        fetchJson(`/addresses/${walletAddress}`, AddressInfoSchema),
        fetchJson(
          `/addresses/${walletAddress}/token-balances`,
          TokenBalancesSchema,
        ),
      ]);
      const native = computeNativeUsd(info.coin_balance, info.exchange_rate);
      const tokensUsd = tokens.reduce(
        (sum, item) => sum + computeTokenUsd(item),
        0,
      );
      const total = native + tokensUsd;
      cache.set(key, { ts: Date.now(), equityUsd: total });
      return total;
    } finally {
      inflight.delete(key);
    }
  })();

  inflight.set(key, promise);
  return promise;
}

/** Test/maintenance seam — clears the in-process cache and inflight map. */
export function _resetPaxscanCache(): void {
  cache.clear();
  inflight.clear();
}

// -----------------------------------------------------------------------------
// Internals
// -----------------------------------------------------------------------------

async function fetchJson<T>(
  path: string,
  schema: z.ZodSchema<T>,
): Promise<T> {
  const url = `${env.PAXSCAN_API_URL.replace(/\/$/, '')}${path}`;
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), env.PAXSCAN_TIMEOUT_MS);
  try {
    const res = await fetch(url, {
      signal: ctl.signal,
      headers: { accept: 'application/json' },
    });
    if (!res.ok) {
      throw new PaxscanError(`paxscan http ${res.status} for ${path}`);
    }
    const json = (await res.json()) as unknown;
    const parsed = schema.safeParse(json);
    if (!parsed.success) {
      throw new PaxscanError(
        `paxscan schema mismatch for ${path}: ${parsed.error.message}`,
      );
    }
    return parsed.data;
  } catch (err) {
    if ((err as { name?: string }).name === 'AbortError') {
      throw new PaxscanError(
        `paxscan timeout (${env.PAXSCAN_TIMEOUT_MS}ms) for ${path}`,
        err,
      );
    }
    if (err instanceof PaxscanError) throw err;
    throw new PaxscanError(
      `paxscan fetch failed for ${path}: ${(err as Error).message}`,
      err,
    );
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Native PAX value: (coin_balance / 1e18) × exchange_rate.
 *
 * coin_balance is wei. Using Number() loses precision above 2^53 wei
 * (~9 quadrillion = 9000 PAX). At 9000 PAX × $20 the absolute error is
 * < $0.000001, far below any breach threshold's resolution.
 */
function computeNativeUsd(
  coinBalance: string | null | undefined,
  rate: string | null | undefined,
): number {
  if (!coinBalance || !rate) return 0;
  const wei = parseBigIntOrZero(coinBalance);
  if (wei === 0n) return 0;
  const r = Number(rate);
  if (!Number.isFinite(r) || r <= 0) return 0;
  return (Number(wei) / 1e18) * r;
}

/**
 * ERC-20 USD value: (value / 10^decimals) × exchange_rate.
 *
 * Skips entries with missing fields, non-numeric decimals, or zero/null rate.
 * NFTs (token_id present) and ERC-1155s slip through cleanly because Paxscan
 * marks their exchange_rate null — counted as $0, which is the right call
 * since we don't want NFT collections inflating funded-account equity.
 */
function computeTokenUsd(item: TokenBalance): number {
  if (!item || !item.token) return 0;
  const value = item.value;
  const decimalsStr = item.token.decimals;
  const rateStr = item.token.exchange_rate;
  if (!value || !decimalsStr || !rateStr) return 0;
  const raw = parseBigIntOrZero(value);
  if (raw === 0n) return 0;
  const decimals = Number(decimalsStr);
  if (!Number.isFinite(decimals) || decimals < 0 || decimals > 36) return 0;
  const rate = Number(rateStr);
  if (!Number.isFinite(rate) || rate <= 0) return 0;
  return (Number(raw) / 10 ** decimals) * rate;
}

function parseBigIntOrZero(s: string): bigint {
  try {
    return BigInt(s);
  } catch {
    return 0n;
  }
}
