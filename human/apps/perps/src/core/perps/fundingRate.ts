/**
 * Venue funding rate for Paxeer Perps markets.
 *
 * The venue ticker sends `last_funding_rate_hourly`: the last settled hourly
 * funding rate as an exact decimal string (a fraction, "0.0001" = 0.01%).
 * It is held as an 18-decimal bigint (1e18 = 1.0), never as a float. The
 * 24h figure is that settled hourly rate × 24, a projection of the last
 * settlement and not an estimate of the next one.
 */

import { formatWadFixed } from "./pnlHelpers";

/** Decimals of the fixed-point funding rate: 1e18 = a rate of 1.0. */
export const FUNDING_RATE_DECIMALS = 18;
const SCALE = 10n ** BigInt(FUNDING_RATE_DECIMALS);
const HOURS_PER_DAY = 24n;
const EXACT_DECIMAL = /^(-?)(\d+)(?:\.(\d{1,18}))?$/;

export const FUNDING_LABEL_1H = "last settled funding (1h)";
export const FUNDING_LABEL_24H = "last settled funding (1h) ×24 projection";

/** The venue ticker fields the quoter reads. */
export interface VenueTicker {
  index_price?: number | null;
  mark_price?: number | null;
  last_funding_rate_hourly?: string | null;
}

/**
 * Parses the venue's hourly rate string to the 18-decimal scale. Anything
 * that is not a plain decimal with at most 18 fractional digits throws, so a
 * missing, renamed or inexact field never reads as zero.
 */
export function parseFundingRateHourly(v: string | null | undefined): bigint {
  const m = typeof v === "string" ? EXACT_DECIMAL.exec(v) : null;
  if (!m) throw new Error(`last_funding_rate_hourly is not an exact decimal string: ${String(v)}`);
  const abs = BigInt(m[2]) * SCALE + BigInt((m[3] ?? "").padEnd(FUNDING_RATE_DECIMALS, "0"));
  return m[1] ? -abs : abs;
}

/** The settled hourly rate × 24, exact. */
export function projectFunding24h(hourly: bigint): bigint {
  return hourly * HOURS_PER_DAY;
}

/** A rate as a percent with `dp` decimals ("0.0100%"), rounded half away from zero. */
export function formatFundingRatePct(rate: bigint, dp = 4): string {
  return `${formatWadFixed(rate * 100n, dp)}%`;
}

/** The two labelled funding figures a display shows for one hourly rate. */
export function fundingFigures(hourly: bigint): { label: string; rate: bigint; text: string }[] {
  const daily = projectFunding24h(hourly);
  return [
    { label: FUNDING_LABEL_1H, rate: hourly, text: formatFundingRatePct(hourly) },
    { label: FUNDING_LABEL_24H, rate: daily, text: formatFundingRatePct(daily) },
  ];
}

const PLAIN_DECIMAL = /^(-?)(\d+)(?:\.(\d+))?$/;

/**
 * A funding rate the server sends as a JSON number (the symbol socket's
 * perp_stats funding_rate and last_funding_rate, and the GraphQL Float
 * last_funding_rate) as an exact plain decimal string. JSON.parse does not
 * expose the number's text, so this writes the parsed double's shortest
 * round-trip digits, which are the digits the server's encoder emitted for
 * that value, without exponent and without any float arithmetic. A string
 * already in exact decimal form passes unchanged; null, missing, non-finite or
 * any other value is null, never zero.
 */
export function fundingRateFromJsonNumber(v: unknown): string | null {
  if (typeof v === "string") return PLAIN_DECIMAL.test(v) ? v : null;
  if (typeof v !== "number" || !Number.isFinite(v)) return null;
  const [mantissa, exp] = String(v).split("e");
  if (exp === undefined) return mantissa;
  const sign = mantissa.startsWith("-") ? "-" : "";
  const [int, frac = ""] = mantissa.replace("-", "").split(".");
  const digits = int + frac;
  const point = int.length + +exp;
  if (point <= 0) return `${sign}0.${"0".repeat(-point)}${digits}`;
  if (point >= digits.length) return `${sign}${digits}${"0".repeat(point - digits.length)}`;
  return `${sign}${digits.slice(0, point)}.${digits.slice(point)}`;
}

/** An exact decimal rate string as a percent with `dp` decimals, rounded half away from zero; null or malformed is a dash. */
export function formatFundingDecimalPct(v: string | null, dp = 4): string {
  const m = v === null ? null : PLAIN_DECIMAL.exec(v);
  if (!m) return "—";
  const frac = m[3] ?? "";
  const units = BigInt(m[2] + frac) * 100n;
  const drop = frac.length - dp;
  const unit = 10n ** BigInt(Math.max(drop, 0));
  const scaled = drop <= 0 ? units * 10n ** BigInt(-drop) : (units + unit / 2n) / unit;
  const step = 10n ** BigInt(dp);
  const body = dp > 0 ? `${scaled / step}.${(scaled % step).toString().padStart(dp, "0")}` : `${scaled}`;
  return `${m[1] && scaled > 0n ? "-" : ""}${body}%`;
}
