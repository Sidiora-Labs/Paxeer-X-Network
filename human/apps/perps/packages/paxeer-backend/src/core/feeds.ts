/** Approved production origins connect directly; local previews use server-held keys. */
export const PUBLIC_FEED_ORIGIN = "https://data.feed.chainflowtrading.com";
const DIRECT_FEED_ORIGINS = (
  process.env.NEXT_PUBLIC_DIRECT_PRICE_FEED_ORIGINS ||
  "https://app.hyperpax.xyz"
)
  .split(",")
  .map((origin) => origin.trim());

export function usesDirectMarketFeeds(origin?: string): boolean {
  const currentOrigin =
    origin ?? (typeof window === "undefined" ? "" : window.location.origin);
  return DIRECT_FEED_ORIGINS.includes(currentOrigin);
}

export function getFeedOrigin(origin?: string): string {
  return usesDirectMarketFeeds(origin)
    ? `${PUBLIC_FEED_ORIGIN}/api`
    : "/api/price-feed";
}

export const FEED_ORIGIN = getFeedOrigin();
export const MARKET_DATA_URL = `${FEED_ORIGIN}/markets`;
export const FEED_SYMBOLS = [
  "BTC",
  "ETH",
  "SOL",
  "AVAX",
  "LINK",
  "HYPE",
  "XRP",
  "ASTER",
  "TRUMP",
  "BNB",
  "SID",
  "PAX",
  "TSLA",
  "NVDA",
  "GOOGL",
  "XAU",
  "NAS100",
  "SPX500",
] as const;
export function normalizeFeedSymbol(symbol: string): string {
  return symbol
    .toUpperCase()
    .replace(/^PERP_/, "")
    .replace(/_USDC$/, "")
    .replace(/\/USD$/, "");
}
export function candleFeedUrl(
  endpoint: "config" | "symbols" | "history",
  symbol = "SID",
  params?: URLSearchParams,
): string {
  const normalized = normalizeFeedSymbol(symbol);
  if (!(FEED_SYMBOLS as readonly string[]).includes(normalized))
    throw new Error(`Unsupported feed symbol: ${symbol}`);
  const query = new URLSearchParams(params);
  if (endpoint !== "config") query.set("symbol", normalized);
  return `${FEED_ORIGIN}/${normalized.toLowerCase()}/${endpoint}${query.size ? `?${query}` : ""}`;
}
