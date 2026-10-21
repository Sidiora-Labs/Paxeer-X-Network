export interface SpotBookLevel {
  price: number;
  /** The level's price exactly as the wire sent it, for exact bigint conversion. */
  priceText: string;
  size: number;
}

/** A positive decimal's exact value as digits * 10^-scale, or null for zero, negative or malformed text. */
function parsePositiveDecimal(raw: unknown): { digits: bigint; scale: number } | null {
  if (typeof raw !== "string" && typeof raw !== "number") return null;
  const m = /^(\d*)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/.exec(String(raw).trim());
  if (!m || !(m[1] || m[2])) return null;
  const frac = m[2] ?? "";
  const digits = BigInt(`${m[1]}${frac}` || "0");
  if (digits <= 0n) return null;
  return { digits, scale: frac.length - Number(m[3] ?? 0) };
}

function compareDecimal(a: { digits: bigint; scale: number }, b: { digits: bigint; scale: number }): number {
  const x = a.scale < b.scale ? a.digits * 10n ** BigInt(b.scale - a.scale) : a.digits;
  const y = b.scale < a.scale ? b.digits * 10n ** BigInt(a.scale - b.scale) : b.digits;
  return x < y ? -1 : x > y ? 1 : 0;
}

/**
 * Book levels with a positive, finite price and size only, asks ascending and
 * bids descending by exact price on the wire text, so index 0 is real liquidity.
 */
export function normalizeLevels(raw: unknown, side: "asks" | "bids"): SpotBookLevel[] {
  if (!Array.isArray(raw)) return [];
  const out: { level: SpotBookLevel; exact: { digits: bigint; scale: number } }[] = [];
  for (const r of raw) {
    const obj = r as Record<string, unknown>;
    const rawPrice = Array.isArray(r) ? r[0] : obj?.price;
    const rawSize = Array.isArray(r) ? r[1] : obj?.size ?? obj?.quantity;
    const exact = parsePositiveDecimal(rawPrice);
    if (!exact || !parsePositiveDecimal(rawSize)) continue;
    const price = Number(rawPrice);
    const size = Number(rawSize);
    if (!(price > 0 && size > 0 && Number.isFinite(price) && Number.isFinite(size))) continue;
    out.push({ level: { price, priceText: String(rawPrice).trim(), size }, exact });
  }
  const dir = side === "asks" ? 1 : -1;
  return out.sort((a, b) => dir * compareDecimal(a.exact, b.exact)).map((e) => e.level);
}
