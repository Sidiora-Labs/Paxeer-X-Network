/**
 * Same-origin proxy for the wallet's Blockscout API.
 *
 * Why this exists:
 *   - Hides `BLOCKSCOUT_UPSTREAM_BASE` from clients (no NEXT_PUBLIC_*).
 *   - Same-origin requests → zero CORS friction for PWA + Capacitor.
 *   - Single chokepoint for rate limiting, auth, and server-side caching.
 *
 * Routing:
 *   Client calls `/api/wallet/api/v2/addresses/0x.../tokens?type=ERC-20`
 *   This handler forwards to
 *     `${BLOCKSCOUT_UPSTREAM_BASE}/api/v2/addresses/0x.../tokens?type=ERC-20`
 *   The `[...path]` segments are joined with `/` and the original query
 *   string is preserved.
 *
 * Methods:
 *   The Blockscout v2 API consumed by `@paxeer/wallet-data` is read-only,
 *   so we only allow GET / HEAD. Anything else returns 405.
 *
 * Caching strategy (server-side, in-process):
 *   Root cause of upstream 429s: every user's request hits api.paxscan.io
 *   from the same server IP, burning a shared rate-limit bucket.
 *
 *   Two mechanisms protect the upstream:
 *   1. Response cache  — successful upstream responses are stored in memory
 *      for a TTL that scales with how frequently the data changes.
 *   2. In-flight deduplication — if 50 users simultaneously request the same
 *      address/token list, only ONE fetch goes upstream; all 50 share the
 *      result. This collapses thundering-herd bursts to a single call.
 *
 *   Cache TTLs (conservative — prefer freshness over upstream savings):
 *     transactions / token-transfers  20 s  (new txs land every few seconds)
 *     token holdings / NFTs           45 s  (balance changes less often)
 *     address info / stats / counters 90 s  (very stable)
 *     everything else                 30 s
 */

import { NextResponse, type NextRequest } from 'next/server';
import { walletLimiter } from '@/lib/rateLimit';
import {
    boundaryErrorResponse,
    encodePathSegments,
    HttpBoundaryError,
    publicError,
    readBoundedUpstream,
    timeoutSignal,
    trustedClientIdentity,
    validateQuery,
} from '@/server/http';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

const UPSTREAM_BASE = (process.env.BLOCKSCOUT_UPSTREAM_BASE || 'https://api.paxscan.io').replace(/\/+$/, '');

// ── Server-side cache ─────────────────────────────────────────────────────────

interface CacheEntry {
    body: string;
    status: number;
    headers: Record<string, string>;
    expiresAt: number;
}

// Upstream responses cached by full URL (path + query string).
const responseCache = new Map<string, CacheEntry>();

// In-flight fetch promises — key = upstream URL, value = promise of the raw
// upstream result. Any second request for the same URL while the first is
// in-flight waits on the same promise (no duplicate upstream calls).
const inFlight = new Map<string, Promise<CacheEntry>>();

// Evict expired entries every 2 minutes to prevent unbounded memory growth.
const pruneTimer = setInterval(() => {
    const now = Date.now();
    for (const [k, v] of responseCache) {
        if (now > v.expiresAt) responseCache.delete(k);
    }
}, 2 * 60_000);
if (pruneTimer.unref) pruneTimer.unref();

// Don't cache responses larger than 512 KB to bound memory usage.
const MAX_CACHE_BYTES = 512 * 1024;

function cacheTtlMs(segments: string[]): number {
    const path = segments.join('/').toLowerCase();
    if (path.includes('transactions') || path.includes('token-transfers')) return 20_000;
    if (path.includes('tokens') || path.includes('nft')) return 45_000;
    if (path.includes('stats') || path.includes('counters') || path.includes('addresses')) return 90_000;
    return 30_000;
}

// ── Header helpers ────────────────────────────────────────────────────────────

function buildUpstreamUrl(req: NextRequest, pathSegments: string[]): string {
    const path = encodePathSegments(pathSegments);
    validateQuery(req, { maxPairs: 32, maxValueLength: 512 });
    const search = req.nextUrl.search;
    return `${UPSTREAM_BASE}/${path}${search}`;
}

// ── Core fetch with cache + deduplication ─────────────────────────────────────

async function fetchWithCache(
    upstreamUrl: string,
    segments: string[],
    req: NextRequest,
): Promise<CacheEntry> {
    const now = Date.now();

    // 1. Cache hit
    const cached = responseCache.get(upstreamUrl);
    if (cached && now < cached.expiresAt) return cached;

    // 2. Deduplicate in-flight: reuse an existing promise for the same URL
    const existing = inFlight.get(upstreamUrl);
    if (existing) return existing;

    // 3. New upstream fetch
    const fetchPromise: Promise<CacheEntry> = (async () => {
        const timed = timeoutSignal(req.signal, 15_000);
        try {
            const upstreamRes = await fetch(upstreamUrl, {
                method: req.method,
                headers: {
                    Accept: 'application/json',
                    'X-Forwarded-By': 'paxport-wallet-proxy',
                },
                signal: timed.signal,
                cache: 'no-store',
                redirect: 'error',
            });

            const upstream = await readBoundedUpstream(upstreamRes, {
                maxBytes: 2_097_152,
                allowedContentTypes: new Set(['application/json']),
            });
            const body = Buffer.from(upstream.body).toString('utf-8');
            const entry: CacheEntry = {
                body,
                status: upstreamRes.status,
                headers: {
                    'content-type': 'application/json; charset=utf-8',
                    'cache-control':
                        upstreamRes.headers.get('cache-control') ??
                        'private, max-age=10, stale-while-revalidate=30',
                },
                expiresAt: 0,
            };

            // Only cache successful responses within the size limit
            if (upstreamRes.status === 200 && body.length <= MAX_CACHE_BYTES) {
                entry.expiresAt = Date.now() + cacheTtlMs(segments);
                responseCache.set(upstreamUrl, entry);
            }

            return entry;
        } finally {
            timed.dispose();
            inFlight.delete(upstreamUrl);
        }
    })();

    inFlight.set(upstreamUrl, fetchPromise);
    return fetchPromise;
}

// ── Route handler ─────────────────────────────────────────────────────────────

type RouteContext = { params: Promise<{ path: string[] }> };

async function forward(req: NextRequest, ctx: RouteContext): Promise<Response> {
    try {
        const rl = await walletLimiter.check(trustedClientIdentity(req));
        if (!rl.ok) {
            return NextResponse.json(
                { error: { code: 'RATE_LIMITED', message: 'Request limit exceeded' } },
                {
                    status: 429,
                    headers: {
                        'Retry-After': String(rl.retryAfter ?? 60),
                        'Cache-Control': 'no-store',
                    },
                },
            );
        }
        if (!UPSTREAM_BASE) {
            return publicError(
                503,
                'WALLET_UPSTREAM_UNAVAILABLE',
                'Wallet data is temporarily unavailable',
            );
        }
        const params = await ctx.params;
        const segments = Array.isArray(params.path) ? params.path : [];
        const upstreamUrl = buildUpstreamUrl(req, segments);
        const entry = await fetchWithCache(upstreamUrl, segments, req);
        const headers = new Headers(entry.headers);
        const fromCache = entry.expiresAt > 0 && Date.now() < entry.expiresAt;
        headers.set('x-cache', fromCache ? 'HIT' : 'MISS');
        return new Response(req.method === 'HEAD' ? null : entry.body, {
            status: entry.status,
            headers,
        });
    } catch (err) {
        if (err instanceof HttpBoundaryError) return boundaryErrorResponse(err);
        const aborted = err instanceof Error && err.name === 'AbortError';
        return publicError(
            aborted ? 504 : 502,
            aborted ? 'UPSTREAM_TIMEOUT' : 'UPSTREAM_UNAVAILABLE',
            aborted ? 'Upstream request timed out' : 'Upstream request failed',
        );
    }
}

export async function GET(req: NextRequest, ctx: RouteContext) {
    return forward(req, ctx);
}

export async function HEAD(req: NextRequest, ctx: RouteContext) {
    return forward(req, ctx);
}

export function POST() {
    return NextResponse.json({ error: 'method not allowed' }, { status: 405 });
}
export const PUT = POST;
export const PATCH = POST;
export const DELETE = POST;
