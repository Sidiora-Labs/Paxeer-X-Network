// ── Sidiora Launchpad token discovery ────────────────────────────────────────
// Paginates the wallet API (Blockscout) /api/v2/tokens endpoint.
// Any ERC-20 not in the known PECOR set is treated as a Sidiora launchpad token.
// isSidioraToken() in constants.ts is the authoritative runtime check.
// After discovery, tokens are enriched with rich metadata (logo, name, decimals)
// from the Sidiora batch metadata API.

import { TOKENS } from '@/lib/swap/sdk';
import { PAXEER_CONFIG } from '@/lib/constants';
import { cacheGet, cacheSet, cacheAge } from '@/lib/metaCache';
import { rewriteLogoUrl } from '@/lib/mediaProxy';
import type { SwapToken } from './constants';

const SIDIORA_METADATA_API = '/wallet/api/sidiora/metadata';

const METADATA_BATCH_SIZE = 40;
const META_TTL = 24 * 60 * 60_000; // 24 h — logos/names are static
const LIST_TTL = 60 * 60_000;       // 1 h  — list of known tokens
const META_CACHE_KEY = 'sidiora-meta-map';
const LIST_CACHE_KEY = 'sidiora-token-list';

/**
 * Enrich tokens with Sidiora metadata (name / symbol / decimals / logo).
 * - Loads the persisted metadata map from localStorage (24 h TTL).
 * - Only fetches addresses not already in cache.
 * - Merges new entries into cache and persists.
 */
async function enrichWithSidioraMetadata(tokens: SwapToken[]): Promise<SwapToken[]> {
    if (tokens.length === 0) return tokens;

    // ── Load cached metadata map ──────────────────────────────────────────
    const cached: Record<string, any> = cacheGet<Record<string, any>>(META_CACHE_KEY, META_TTL) ?? {};

    // Only request addresses that are missing from cache
    const missing = tokens.filter((t) => !cached[t.address.toLowerCase()]);

    if (missing.length > 0) {
        for (let i = 0; i < missing.length; i += METADATA_BATCH_SIZE) {
            const chunk = missing.slice(i, i + METADATA_BATCH_SIZE);
            const addrList = chunk.map((t) => t.address).join(',');
            try {
                const res = await fetch(`${SIDIORA_METADATA_API}?addresses=${addrList}`);
                if (!res.ok) continue;
                const data: Record<string, any> = await res.json();
                for (const [k, v] of Object.entries(data)) {
                    cached[k.toLowerCase()] = v;
                }
            } catch {
                // enrichment is non-critical
            }
        }
        // Persist the merged map
        cacheSet(META_CACHE_KEY, cached);
    }

    return tokens.map((t) => {
        const meta = cached[t.address.toLowerCase()];
        if (!meta) return t;
        return {
            ...t,
            name: meta.name || t.name,
            symbol: meta.symbol || t.symbol,
            decimals: typeof meta.decimals === 'number' ? meta.decimals : t.decimals,
            iconUrl: rewriteLogoUrl(meta.images?.logo) || t.iconUrl,
        };
    });
}

// Same-origin proxy. The Next route handler at `/wallet/api/wallet/[...path]`
// forwards to BLOCKSCOUT_UPSTREAM_BASE on the server, so the upstream host
// never ships in the client bundle.
const WALLET_API_BASE = PAXEER_CONFIG.blockscoutApiBase;
const MAX_PAGES = 10; // 50 items/page × 10 pages = up to 500 tokens

// Addresses of all known PECOR V3 core tokens (lowercase)
const PECOR_CORE = new Set(
    Object.values(TOKENS).map(t => t.address.toLowerCase()),
);

interface WalletApiToken {
    address_hash: string;
    name: string;
    symbol: string;
    decimals: string | null;
    icon_url: string | null;
    type: string | null;
}

interface WalletApiTokensResponse {
    items: WalletApiToken[];
    next_page_params: Record<string, unknown> | null;
}

export async function fetchLaunchpadTokens(): Promise<SwapToken[]> {
    const result: SwapToken[] = [];
    const seen = new Set<string>();

    let nextUrl: string | null = `${WALLET_API_BASE}/api/v2/tokens?type=ERC-20`;
    let pages = 0;

    while (nextUrl && pages < MAX_PAGES) {
        try {
            const res = await fetch(nextUrl);
            if (!res.ok) break;

            const data: WalletApiTokensResponse = await res.json();

            for (const t of data.items) {
                const addr = t.address_hash.toLowerCase();
                if (seen.has(addr) || PECOR_CORE.has(addr)) continue;
                seen.add(addr);

                result.push({
                    symbol: t.symbol || addr.slice(0, 6),
                    name: t.name || addr.slice(0, 8) + '\u2026',
                    address: addr,
                    decimals: t.decimals ? Number(t.decimals) : 18,
                    isLaunchpad: true,
                });
            }

            if (!data.next_page_params) break;

            const qs = new URLSearchParams({ type: 'ERC-20' });
            for (const [k, v] of Object.entries(data.next_page_params)) {
                qs.set(k, String(v));
            }
            nextUrl = `${WALLET_API_BASE}/api/v2/tokens?${qs.toString()}`;
            pages++;
        } catch (err) {
            console.warn('[Sidiora] fetchLaunchpadTokens page failed:', err);
            break;
        }
    }

    const enriched = await enrichWithSidioraMetadata(result);
    cacheSet(LIST_CACHE_KEY, enriched);
    return enriched;
}

/**
 * Synchronously return the cached token list from localStorage if it is
 * still within TTL. Returns null when the cache is cold or expired.
 * Used by `useOutputTokens` as TanStack Query `initialData` to render
 * immediately without a network round-trip.
 */
export function getCachedLaunchpadTokens(): { tokens: SwapToken[]; updatedAt: number } | null {
    const tokens = cacheGet<SwapToken[]>(LIST_CACHE_KEY, LIST_TTL);
    if (!tokens) return null;
    const updatedAt = cacheAge(LIST_CACHE_KEY, LIST_TTL);
    return { tokens, updatedAt };
}
