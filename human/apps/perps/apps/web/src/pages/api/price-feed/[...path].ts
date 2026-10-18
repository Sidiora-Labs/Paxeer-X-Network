import type { NextApiRequest, NextApiResponse } from "next";
import { APIError } from "@sidioralabs/chainflow-data-gateway";
import type { Market } from "@sidioralabs/chainflow-data-gateway/resources/coin-gecko";
import { chainflowClient } from "../../../server/chainflow";

const SYMBOLS = new Set([
  "btc",
  "eth",
  "sol",
  "avax",
  "link",
  "hype",
  "xrp",
  "aster",
  "trump",
  "bnb",
  "sid",
  "pax",
  "tsla",
  "nvda",
  "googl",
  "xau",
  "nas100",
  "spx500",
]);

export default async function priceFeed(
  req: NextApiRequest,
  res: NextApiResponse,
) {
  res.setHeader("Cache-Control", "no-store");
  if (req.method !== "GET" && req.method !== "HEAD") {
    res.setHeader("Allow", "GET, HEAD");
    res.status(405).json({ error: "method_not_allowed" });
    return;
  }
  const segments = req.query.path;
  if (
    !Array.isArray(segments) ||
    segments.some((segment) => !/^[a-zA-Z0-9_-]+$/.test(segment))
  ) {
    res.status(400).json({ error: "invalid_feed_path" });
    return;
  }
  const mint = segments.length === 1 && segments[0] === "ws-token";
  const scope =
    typeof req.query.symbol === "string" ? req.query.symbol.toLowerCase() : "";
  if (mint && !SYMBOLS.has(scope) && !["markets", "explorer"].includes(scope)) {
    res.status(400).json({ error: "invalid_feed_symbol" });
    return;
  }
  const marketResource = segments[0] === "markets";
  const allowed =
    mint ||
    (marketResource &&
      ((segments.length === 2 &&
        ["tickers", "totals", "markets"].includes(segments[1])) ||
        (segments.length === 3 &&
          segments[1] === "ticker" &&
          SYMBOLS.has(segments[2].toLowerCase())))) ||
    (SYMBOLS.has(segments[0]) &&
      segments.length === 2 &&
      ["config", "symbols", "history", "time", "search"].includes(segments[1]));
  if (!allowed) {
    res.status(404).json({ error: "feed_not_found" });
    return;
  }
  if (
    !process.env.PRICE_FEED_REST_API_KEY ||
    (mint && !process.env.PRICE_FEED_WS_API_KEY)
  ) {
    res.status(503).json({ error: "feed_not_configured" });
    return;
  }
  const incoming = new URL(req.url || "/", "http://localhost");
  const params = incoming.searchParams;
  try {
    const client = chainflowClient(mint ? "mint" : "rest");
    let data: unknown;
    if (mint) {
      const token = await client.gateway.createWebSocketToken({
        symbol: scope as Market | "markets" | "explorer",
      });
      const url = new URL(`/ws/${scope}`, client.baseURL);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      data = { ...token, url: url.toString() };
    } else if (marketResource) {
      if (segments[1] === "tickers") data = await client.markets.listTickers();
      else if (segments[1] === "markets") data = await client.markets.list();
      else if (segments[1] === "totals")
        data = await client.markets.listTotals();
      else data = await client.markets.retrieveTicker(segments[2]);
    } else {
      const market = segments[0] as Market;
      const symbol = params.get("symbol") || market.toUpperCase();
      switch (segments[1]) {
        case "config":
          data = await client.tradingView.listDatafeedConfig(market);
          break;
        case "symbols":
          data = await client.tradingView.resolveSymbol(market, {
            symbol: symbol as Uppercase<Market>,
          });
          break;
        case "time":
          data = await client.tradingView.listFeedTime(market);
          break;
        case "search":
          data = await client.tradingView.search(market, {
            query: params.get("query") || "",
            limit: Number(params.get("limit") || 30),
          });
          break;
        case "history": {
          const from = Number(params.get("from")),
            to = Number(params.get("to"));
          const countback = params.has("countback")
            ? Number(params.get("countback"))
            : undefined;
          if (
            !params.has("from") ||
            !params.has("to") ||
            !Number.isFinite(from) ||
            !Number.isFinite(to) ||
            from < 0 ||
            to < from ||
            (countback !== undefined &&
              (!Number.isInteger(countback) ||
                countback <= 0 ||
                countback > 10000))
          ) {
            res.status(400).json({ error: "invalid_history_range" });
            return;
          }
          const resolution = params.get("resolution") || "15";
          if (
            ![
              "1",
              "2",
              "3",
              "5",
              "15",
              "30",
              "60",
              "240",
              "480",
              "D",
              "W",
              "1D",
              "1W",
            ].includes(resolution)
          ) {
            res.status(400).json({ error: "unsupported_resolution" });
            return;
          }
          data = await client.tradingView.listCandleHistory(market, {
            symbol: symbol as Uppercase<Market>,
            resolution: resolution as Parameters<
              typeof client.tradingView.listCandleHistory
            >[1]["resolution"],
            from,
            to,
            ...(countback ? { countback } : {}),
          });
          break;
        }
      }
    }
    if (req.method === "HEAD") {
      res.status(200).end();
      return;
    }
    if (segments[1] === "time") {
      res.setHeader("Content-Type", "text/plain");
      res.status(200).send(String(data));
    } else res.status(200).json(data);
  } catch (error) {
    res
      .status(error instanceof APIError ? error.status || 502 : 502)
      .json({ error: "feed_request_failed" });
  }
}
