/**
 * Shared, pure helpers for the DX Trading kit — number formatting, symbol parsing,
 * profit / loss tones and a display-only countdown. No market data, no network.
 */
import { useEffect, useState } from "react";

/* ------------------------------------------------------------ tones */

export type Tone = "up" | "down" | "flat";

/** Sign of a value as a tone. `NaN`, `null` and zero are flat. */
export function toneOf(value: number | null | undefined): Tone {
  if (value == null || !Number.isFinite(value) || value === 0) return "flat";
  return value > 0 ? "up" : "down";
}

/** Text colours for up / down / flat — the same pair the Order Book chart uses. */
export const TONE_TEXT: Record<Tone, string> = {
  up: "text-emerald-700 dark:text-emerald-400",
  down: "text-rose-600 dark:text-rose-400",
  flat: "text-muted-foreground",
};

/** Soft fills for chips, badges and depth bars. */
export const TONE_BG: Record<Tone, string> = {
  up: "bg-emerald-500/12",
  down: "bg-rose-500/12",
  flat: "bg-muted",
};

/** Solid fills for buy / sell buttons. */
export const SIDE_FILL = {
  buy: "bg-emerald-600 text-white hover:bg-emerald-600/90 dark:bg-emerald-500 dark:text-emerald-950",
  sell: "bg-rose-600 text-white hover:bg-rose-600/90 dark:bg-rose-500 dark:text-rose-950",
} as const;

export type OrderSide = "buy" | "sell";

export const sideTone = (side: OrderSide): Tone => (side === "buy" ? "up" : "down");

/* ------------------------------------------------------------ numbers */

/** Decimal places implied by a tick size: 0.01 → 2, 0.5 → 1, 10 → 0. */
export function decimalsFromTick(tick: number) {
  if (!Number.isFinite(tick) || tick <= 0) return 2;
  const text = tick.toExponential();
  const [mantissa, exponent] = text.split("e");
  const fraction = mantissa.split(".")[1]?.length ?? 0;
  return Math.max(0, fraction - Number(exponent));
}

const formatters = new Map<string, Intl.NumberFormat>();
function nf(options: Intl.NumberFormatOptions) {
  const key = JSON.stringify(options);
  let f = formatters.get(key);
  if (!f) {
    f = new Intl.NumberFormat("en-US", options);
    formatters.set(key, f);
  }
  return f;
}

export type NumberRule = "price" | "number" | "percent" | "compact" | "usd";

export type FormatOptions = {
  /** Decimal places. Wins over `tick`. */
  dp?: number;
  /** Price increment, e.g. 0.01. Sets decimal places when `dp` is omitted. */
  tick?: number;
  /** Prefix positives with `+`. */
  sign?: boolean;
  /** Keep trailing zeros (default) or trim them. */
  pad?: boolean;
};

/**
 * Format a number for trading UIs.
 * - `price` / `number`: grouped, fixed decimals
 * - `percent`: the value is a fraction (0.0123 → 1.23%)
 * - `compact`: 1.2K, 3.4M, 5.6B
 * - `usd`: `$1,234.56`
 */
export function formatNumber(value: number, rule: NumberRule = "number", { dp, tick, sign, pad = true }: FormatOptions = {}) {
  if (!Number.isFinite(value)) return "—";
  const decimals = dp ?? (tick ? decimalsFromTick(tick) : rule === "compact" ? 2 : 2);
  const min = pad ? decimals : 0;
  let body: string;
  switch (rule) {
    case "percent":
      body = `${nf({ minimumFractionDigits: min, maximumFractionDigits: decimals }).format(Math.abs(value) * 100)}%`;
      break;
    case "compact":
      body = nf({ notation: "compact", maximumFractionDigits: decimals }).format(Math.abs(value));
      break;
    case "usd":
      body = `$${nf({ minimumFractionDigits: min, maximumFractionDigits: decimals }).format(Math.abs(value))}`;
      break;
    default:
      body = nf({ minimumFractionDigits: min, maximumFractionDigits: decimals }).format(Math.abs(value));
  }
  const neg = value < 0 && Number(body.replace(/[^0-9.]/g, "")) !== 0;
  if (neg) return `-${body}`;
  if (sign && value > 0) return `+${body}`;
  return body;
}

export const formatPrice = (value: number, tick?: number) => formatNumber(value, "price", { tick });
export const formatPercent = (fraction: number, sign = true, dp = 2) => formatNumber(fraction, "percent", { sign, dp });
export const formatCompact = (value: number, dp = 2) => formatNumber(value, "compact", { dp });
export const formatUsd = (value: number, dp = 2) => formatNumber(value, "usd", { dp });

/* ------------------------------------------------------------ symbols */

export type ParsedSymbol = { base: string; quote: string; type: "PERP" | "SPOT" | string };

/**
 * Parse exchange symbols into parts.
 * Accepts `PERP_BTC_USDC`, `BTC-PERP`, `BTC/USDC` and `BTCUSDT`.
 */
export function parseSymbol(symbol: string): ParsedSymbol {
  const s = symbol.trim().toUpperCase();
  const underscore = s.split("_");
  if (underscore.length === 3) return { type: underscore[0], base: underscore[1], quote: underscore[2] };
  if (s.includes("/")) {
    const [base, quote] = s.split("/");
    return { base, quote, type: "SPOT" };
  }
  if (s.endsWith("-PERP")) return { base: s.slice(0, -5), quote: "USDC", type: "PERP" };
  if (s.includes("-")) {
    const [base, quote] = s.split("-");
    return { base, quote, type: "SPOT" };
  }
  const q = ["USDT", "USDC", "USD"].find((x) => s.endsWith(x) && s.length > x.length);
  return q ? { base: s.slice(0, -q.length), quote: q, type: "SPOT" } : { base: s, quote: "", type: "SPOT" };
}

/** `0x1234…abcd` */
export function shortenAddress(address: string, head = 6, tail = 4) {
  return address.length <= head + tail + 1 ? address : `${address.slice(0, head)}…${address.slice(-tail)}`;
}

/* ------------------------------------------------------------ time */

const pad2 = (n: number) => String(n).padStart(2, "0");

/** `HH:MM:SS` for a duration in ms (clamped at zero). Adds `Nd ` when over a day. */
export function formatCountdown(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const d = Math.floor(total / 86400);
  const h = Math.floor((total % 86400) / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return `${d ? `${d}d ` : ""}${pad2(h)}:${pad2(m)}:${pad2(s)}`;
}

/** Display-only countdown to a timestamp, re-rendering once a second. */
export function useCountdown(target: number | Date | null | undefined) {
  const to = target == null ? null : typeof target === "number" ? target : target.getTime();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (to == null) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [to]);
  return to == null ? null : Math.max(0, to - now);
}
