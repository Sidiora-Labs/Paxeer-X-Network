// Runs once at server start: parse every server-side variable so a malformed value fails the boot.
export async function register(): Promise<void> {
  if (process.env.NEXT_RUNTIME === "edge") return;
  const { parseMarketProxyOrigin } = await import("./pages/api/candle/[...path]");
  const { parseStatusPageUrl } = await import("./app/shell/Footer");
  await import("./core/network/gateway");
  if (process.env.MARKET_PROXY_ORIGIN) parseMarketProxyOrigin(process.env.MARKET_PROXY_ORIGIN);
  parseStatusPageUrl(process.env.NEXT_PUBLIC_STATUS_PAGE_URL);
}
