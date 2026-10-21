// Layer 2 — RPC endpoint list + viem fallback transport factory.
//
// Paxeer operates 200+ owned RPC nodes; we currently expose two
// public hostnames and let viem's `fallback()` rotate between them
// with per-request ranking. The single-endpoint config that shipped
// before this change took the whole UI down whenever the default
// node had a degraded ~10 % error rate during reorg-heavy windows.
//
// Order of precedence:
//
//   1. `NEXT_PUBLIC_PAXEER_RPC_URLS` (comma-separated) — full override.
//   2. `NEXT_PUBLIC_PAXEER_RPC_URL` (single) — back-compat single host.
//   3. Hard-coded defaults below.
//
// Adding endpoints is a no-code change once an operator wires the env
// var into Vercel — keep both forms here so a hot-fix can ship without
// a redeploy.

import { fallback, http, type Transport } from "viem";

const DEFAULT_PAXEER_RPCS: readonly string[] = Array.from(
  { length: 16 }, (_, i) => `https://api${i + 1}.mainnet-beta.paxeer.network`,
);

// JSON-RPC endpoints that must never serve reads, no matter what a
// deploy env pins:
//
//   - *.rpcpaxeer.online — the pre-migration Evmos-era network. Still
//     answers JSON-RPC but with the DEAD chain's state, so a receipt or
//     balance query "succeeds" with empty/old data and viem's fallback
//     never fails over.
//   - api.hyperpax.xyz — live chain, but its tx-by-hash/receipt index
//     does not resolve fresh transactions (deposits confirmed on-chain
//     while eth_getTransactionReceipt kept returning null). It remains
//     the SPOT REST/WS data API host; only JSON-RPC moved off it, to
//     stats.paxscan.io.
const LEGACY_RPC_HOSTS = [".rpcpaxeer.online", "rpcpaxeer.online", "api.hyperpax.xyz", "stats.paxscan.io"];

function isLegacyRpcUrl(url: string): boolean {
  try {
    const host = new URL(url).hostname;
    return LEGACY_RPC_HOSTS.some((h) => host === h || host.endsWith(h));
  } catch {
    return false;
  }
}

function dedupe(list: readonly string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of list) {
    const url = raw.trim();
    if (!url) continue;
    if (seen.has(url)) continue;
    if (isLegacyRpcUrl(url)) continue;
    seen.add(url);
    out.push(url);
  }
  return out;
}

export function getPaxeerRpcUrls(): string[] {
  const multi = (process.env.NEXT_PUBLIC_PAXEER_RPC_URLS ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  if (multi.length > 0) {
    const urls = dedupe(multi);
    // If the env pinned only legacy endpoints, fall back to the
    // defaults rather than shipping a client with zero transports.
    if (urls.length > 0) return urls;
  }
  const single = (process.env.NEXT_PUBLIC_PAXEER_RPC_URL ?? "").trim();
  if (single) {
    const urls = dedupe([single, ...DEFAULT_PAXEER_RPCS]);
    if (urls.length > 0) return urls;
  }
  return [...DEFAULT_PAXEER_RPCS];
}

/**
 * Build a viem fallback transport across all configured RPCs.
 *
 * `rank: true` periodically re-orders endpoints by latency so the
 * fastest healthy node serves the bulk of traffic. `retryCount: 0`
 * on the inner http() transports keeps fallback's own retry budget
 * the only retry surface — otherwise both layers retry and we burn
 * 9 × the budget on a slow node.
 */
export function buildPaxeerTransport(): Transport {
  const urls = getPaxeerRpcUrls();
  // viem accepts a single transport when the array has one element,
  // but using fallback() unconditionally simplifies upgrade paths
  // (adding a second URL is just an env change).
  const transports = urls.map((url) =>
    http(url, {
      timeout: 10_000,
      retryCount: 0,
    }),
  );
  return fallback(transports, {
    rank: { interval: 30_000, sampleCount: 3, timeout: 1_000, weights: { latency: 0.5, stability: 0.5 } },
    retryCount: 2,
    retryDelay: 200,
  });
}
