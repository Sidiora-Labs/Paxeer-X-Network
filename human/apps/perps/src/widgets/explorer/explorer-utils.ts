// ═══════════════════════════════════════════════════════════════════════
//  Explorer utilities — shared helpers for the live explorer feature.
//  Pure functions only; no React, no side effects.
// ═══════════════════════════════════════════════════════════════════════

/**
 * ISO/timestamp string (or Date) → compact relative time.
 *   < 5s   → "just now"
 *   < 60s  → "Ns ago"
 *   < 60m  → "Nm ago"
 *   < 24h  → "Nh ago"
 *   else   → "Nd ago"
 * Invalid / null → "—".
 */
export function toRelativeTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined || input === "") return "\u2014";
  const d =
    typeof input === "string"
      ? new Date(input)
      : typeof input === "number"
        ? new Date(input * (input < 1e12 ? 1000 : 1)) // unix s or ms
        : input;
  if (!(d instanceof Date) || isNaN(d.getTime())) return "\u2014";
  const diff = Math.max(0, (Date.now() - d.getTime()) / 1000);
  if (diff < 5) return "just now";
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

/**
 * Hash truncation: "0x12345678...abcd". Default front=8 back=4.
 * Short inputs (< front+back+2) returned unchanged.
 */
export function truncateHash(s: string | null | undefined, front = 8, back = 4): string {
  if (!s) return "\u2014";
  if (s.length <= front + back + 2) return s;
  return `${s.slice(0, front)}\u2026${s.slice(-back)}`;
}

/**
 * Format gas used / gas limit (strings from the Block type) as compact
 * readable numbers. "12345678" → "12.35M", "987" → "987".
 */
export function fmtGas(v: string | null | undefined): string {
  if (v === null || v === undefined || v === "") return "\u2014";
  const n = Number(v);
  if (!isFinite(n)) return "\u2014";
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}K`;
  return String(Math.round(n));
}

/**
 * Gas used / gas limit → "XX.X%". Null-safe.
 */
export function fmtGasPct(used: string | null | undefined, limit: string | null | undefined): string {
  if (!used || !limit) return "\u2014";
  const u = Number(used);
  const l = Number(limit);
  if (!isFinite(u) || !isFinite(l) || l === 0) return "\u2014";
  return `${((u / l) * 100).toFixed(1)}%`;
}

/**
 * Explorer focus query-param contract.
 *   ?tx=<hash>        → focus on a transaction (takes precedence)
 *   ?block=<id>       → focus on a block (height or hash)
 *   (neither)         → plain live stream
 *   (both)            → tx wins
 */
export type ExplorerFocus =
  | { kind: "tx"; value: string }
  | { kind: "block"; value: string }
  | { kind: "none" };

/**
 * Parse Next.js router `query` (or URLSearchParams) into a discriminated
 * ExplorerFocus. Tolerates string | string[] | undefined per Next.js.
 */
export function parseFocus(
  query: Record<string, string | Array<string> | undefined> | URLSearchParams,
): ExplorerFocus {
  const pick = (k: string): string | undefined => {
    if (query instanceof URLSearchParams) return query.get(k) ?? undefined;
    const raw = query[k];
    if (Array.isArray(raw)) return raw[0];
    return raw;
  };
  const tx = pick("tx");
  if (tx && tx.length > 0) return { kind: "tx", value: tx };
  const block = pick("block");
  if (block && block.length > 0) return { kind: "block", value: block };
  return { kind: "none" };
}

/**
 * Build an explorer URL for a given focus. Used by redirect shells.
 *   buildExplorerUrl({ kind: "tx", value: "0xabc" })  → "/explorer?tx=0xabc"
 *   buildExplorerUrl({ kind: "block", value: "123" }) → "/explorer?block=123"
 *   buildExplorerUrl({ kind: "none" })                → "/explorer"
 */
export function buildExplorerUrl(focus: ExplorerFocus): string {
  if (focus.kind === "tx") return `/explorer?tx=${encodeURIComponent(focus.value)}`;
  if (focus.kind === "block") return `/explorer?block=${encodeURIComponent(focus.value)}`;
  return "/explorer";
}

/**
 * Check whether a string is a 66-char `0x`-prefixed 32-byte hex hash (tx hash
 * or block hash). Pure heuristic — used to disambiguate focus=<value> when
 * callers pass a single string.
 */
export function looksLikeHash(s: string): boolean {
  return /^0x[0-9a-fA-F]{64}$/.test(s);
}

/**
 * Check whether a string is a positive integer block height.
 */
export function looksLikeBlockHeight(s: string): boolean {
  return /^[0-9]+$/.test(s);
}
