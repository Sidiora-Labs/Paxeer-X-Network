import ChainflowDataGateway from "@sidioralabs/chainflow-data-gateway";

/** Each credential has one purpose; the generated client's mint header overrides its REST header. */
export function chainflowClient(purpose: "rest" | "mint" = "rest") {
  const restAPIKey = process.env.PRICE_FEED_REST_API_KEY;
  const wsMintKey =
    purpose === "mint" ? process.env.PRICE_FEED_WS_API_KEY : null;
  if (!restAPIKey || (purpose === "mint" && !wsMintKey))
    throw new Error("feed_not_configured");
  return new ChainflowDataGateway({
    restAPIKey,
    wsMintKey,
    wsToken: null,
    baseURL:
      process.env.PRICE_FEED_ORIGIN || "https://data.feed.chainflowtrading.com",
    timeout: 12_000,
    maxRetries: 0,
    logLevel: "off",
    fetch: (input, init) =>
      globalThis.fetch(input, { ...init, redirect: "error" }),
  });
}
