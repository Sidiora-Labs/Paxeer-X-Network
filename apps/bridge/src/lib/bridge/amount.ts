/**
 * Amount handling: exact bigint parsing and display. Amounts never pass
 * through a float for anything but a lossy visual approximation (the dxui
 * Numeral in history lists); every comparison, cap check and contract
 * argument uses the bigint value.
 */
import { formatUnits } from "viem";

/** Parse a decimal string to base units. Returns undefined on any malformed input. */
export function parseAmount(input: string, decimals: number): bigint | undefined {
  const text = input.trim().replace(/,/g, "");
  if (!/^\d+(\.\d*)?$/.test(text)) return undefined;
  const [wholeRaw, fraction = ""] = text.split(".");
  const whole = wholeRaw.replace(/^0+(?=\d)/, "");
  if (fraction.length > decimals) return undefined;
  const padded = fraction.padEnd(decimals, "0");
  const combined = `${whole}${padded}`.replace(/^0+(?=\d)/, "");
  return BigInt(combined || "0");
}

/** True when the input names more fractional digits than the asset supports. */
export function hasExcessPrecision(input: string, decimals: number): boolean {
  const text = input.trim();
  const fraction = text.split(".")[1];
  return fraction !== undefined && fraction.length > decimals;
}

const DISPLAY_FRACTION_DIGITS = 6;

/** Full-precision units → trimmed decimal string ("1.5", "0.000001", "0"). */
export function formatAmount(value: bigint, decimals: number): string {
  const full = formatUnits(value, decimals);
  if (!full.includes(".")) return full;
  return full.replace(/0+$/, "").replace(/\.$/, "");
}

/**
 * Units → short display string, truncated (never rounded up) at
 * DISPLAY_FRACTION_DIGITS fractional digits, thousands-grouped.
 * Truncation keeps a displayed balance from overstating what is spendable.
 */
export function formatAmountDisplay(value: bigint, decimals: number): string {
  const full = formatUnits(value, decimals);
  const [whole, fraction = ""] = full.split(".");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const shown = fraction.slice(0, DISPLAY_FRACTION_DIGITS).replace(/0+$/, "");
  if (!shown) return grouped;
  return `${grouped}.${shown}`;
}

/** Percent (0–100, clamped) of `part` in `whole`, for cap-usage bars. */
export function capPercent(part: bigint, whole: bigint): number {
  if (whole <= 0n) return 0;
  if (part <= 0n) return 0;
  if (part >= whole) return 100;
  // Two decimal places of percent via basis points — display only.
  return Number((part * 10000n) / whole) / 100;
}
