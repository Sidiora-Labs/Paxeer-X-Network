// ── Canonical formatting library ─────────────────────────────────────
// Single source of truth. Created by Agent 3 (refactor/formatting).
// Merged in B5b (2026-05-06): added `fmt` and `fmtCompact` from the now-
// retired src/lib/ui-design/format.ts. The duplicate `shortAddr` from that
// file was dropped — this file's version (3-dot ASCII, no length guard) wins.

const usdFmt = new Intl.NumberFormat("en-US", {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/**
 * Full two-decimal USD. No compact abbreviation.
 * fmtUsd(1234567.89)  → "$1,234,567.89"
 * fmtUsd(null)        → "$0.00"
 */
export function fmtUsd(v: number | string | null | undefined): string {
  const n = typeof v === "string" ? parseFloat(v || "0") : (v ?? 0);
  if (isNaN(n)) return "$0.00";
  return n < 0 ? `-$${usdFmt.format(-n)}` : `$${usdFmt.format(n)}`;
}

/**
 * Compact USD. Zero/null/undefined → "—".
 * Thresholds (absolute value): ≥1e9 → B, ≥1e6 → M, ≥1e3 → K, else 2 decimals.
 * fmtUsdCompact(1234567) → "$1.23M"
 * fmtUsdCompact(0)       → "—"
 */
export function fmtUsdCompact(v: number | string | null | undefined): string {
  const n = typeof v === "string" ? parseFloat(v || "0") : (v ?? 0);
  if (isNaN(n) || Math.abs(n) < 0.005) return "\u2014";
  const sign = n < 0 ? "-" : "";
  const abs = Math.abs(n);
  if (abs >= 1_000_000_000) return `${sign}$${(abs / 1_000_000_000).toFixed(2)}B`;
  if (abs >= 1_000_000) return `${sign}$${(abs / 1_000_000).toFixed(2)}M`;
  if (abs >= 1_000) return `${sign}$${(abs / 1_000).toFixed(2)}K`;
  return `${sign}$${usdFmt.format(abs)}`;
}

/**
 * Tiered decimal precision based on price magnitude.
 * ≥1000 → 2 decimals, ≥1 → 4 decimals, else 6 decimals.
 */
export function fmtPrice(price: number): string {
  if (price >= 1000)
    return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1)
    return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

/**
 * Raw ratio (0.012) → "+1.2345%" with sign. `digits` defaults to 4.
 */
export function fmtPercent(v: number, digits: number = 4): string {
  const pct = v * 100;
  return `${pct >= 0 ? "+" : ""}${pct.toFixed(digits)}%`;
}

/**
 * Already-a-percentage value (1.23) → "+1.23%". Always 2 decimals.
 */
export function fmtChangePct(v: number): string {
  return `${v >= 0 ? "+" : ""}${v.toFixed(2)}%`;
}

/**
 * ISO/timestamp string → locale date string. Null/undefined → "—".
 */
export function fmtDate(v: string | null | undefined): string {
  if (!v) return "\u2014";
  const d = new Date(v);
  if (isNaN(d.getTime())) return "\u2014";
  return d.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * "0x1234...5678" — first 6 + last 4 chars.
 */
export function shortAddr(a: string): string {
  return `${a.slice(0, 6)}...${a.slice(-4)}`;
}

/**
 * Parse an 18-decimal wei string to a JS number.
 * Returns 0 on null/undefined/unparseable.
 */
export function parse18(v: string | null | undefined): number {
  if (!v) return 0;
  // PerfDB sometimes returns PSQL NUMERIC values serialised in scientific
  // notation (e.g. "1000e+20", "-5e-3"). BigInt rejects those, so take the
  // float path whenever an exponent is present.
  if (/[eE]/.test(v)) {
    const n = parseFloat(v);
    return isNaN(n) ? 0 : n / 1e18;
  }
  if (!v.includes(".")) {
    try {
      return Number(BigInt(v)) / 1e18;
    } catch {
      return 0;
    }
  }
  const n = parseFloat(v);
  return isNaN(n) ? 0 : n / 1e18;
}

// ── Token-style formatters (merged from src/lib/ui-design/format.ts in B5b) ─

/**
 * Locale-aware fixed-digit number format. Defaults to 2 decimals.
 * Uses en-US grouping + tabular-nums-friendly output.
 *
 * @example fmt(12487.42)     // "12,487.42"
 * @example fmt(0.4821, 4)    // "0.4821"
 * @example fmt(1_000_000, 0) // "1,000,000"
 */
export function fmt(n: number, digits = 2): string {
  return Number(n).toLocaleString("en-US", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}

/**
 * Compact large numbers with K/M/B suffixes.
 * Ported from the inline pattern in design/LiquidStat.jsx:21-25.
 *
 * @example fmtCompact(1_234)       // "1,234"
 * @example fmtCompact(1_234_567)   // "1.23M"
 * @example fmtCompact(204_592_180) // "204.59M"
 */
export function fmtCompact(n: number): string {
  if (Math.abs(n) >= 1_000_000_000) return (n / 1_000_000_000).toFixed(2) + "B";
  if (Math.abs(n) >= 1_000_000) return (n / 1_000_000).toFixed(2) + "M";
  if (Math.abs(n) >= 1_000)
    return n.toLocaleString("en-US", { maximumFractionDigits: 2 });
  return n.toFixed(2);
}
