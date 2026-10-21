/** Public, rate-limited read feeds. No API keys belong in the browser. */
export const FEED_ORIGIN = `https://${process.env.NEXT_PUBLIC_MARKET_PROXY_HOST || "data.feed.chainflowtrading.com"}`;
export const MARKET_DATA_URL = `${FEED_ORIGIN}/api/markets`;
export const FEED_SYMBOLS = ["BTC", "ETH", "SOL", "AVAX", "LINK", "HYPE", "XRP", "ASTER", "TRUMP", "BNB", "SID", "PAX", "TSLA", "NVDA", "GOOGL", "XAU", "NAS100", "SPX500"] as const;
export function normalizeFeedSymbol(symbol: string): string {
  return symbol.toUpperCase().replace(/^PERP_/, "").replace(/_USDC$/, "").replace(/\/USD$/, "");
}
export function candleFeedUrl(endpoint: "config" | "symbols" | "history", symbol = "SID", params?: URLSearchParams): string {
  const normalized = normalizeFeedSymbol(symbol);
  if (!(FEED_SYMBOLS as readonly string[]).includes(normalized)) throw new Error(`Unsupported feed symbol: ${symbol}`);
  const query = new URLSearchParams(params);
  if (endpoint !== "config") query.set("symbol", normalized);
  return `${FEED_ORIGIN}/api/${normalized.toLowerCase()}/${endpoint}${query.size ? `?${query}` : ""}`;
}
