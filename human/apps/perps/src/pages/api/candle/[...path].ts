import type { NextApiRequest, NextApiResponse } from "next";

// Unified Sidiora indexer for stocks (TSLA, NVDA, NAS100, XAU, SPX500,
// GOOGL, …) — no per-symbol box exists for those yet.
const DEFAULT_UPSTREAM = `https://${process.env.MARKET_PROXY_HOST || "data.feed.chainflowtrading.com"}/api/sid`;

// Market-data proxy (Nginx on Railway) that hides the per-symbol candle
// hosts behind a single domain. See `/proxy/nginx.conf.template`.
const MARKET_PROXY_HOST =
  process.env.MARKET_PROXY_HOST || "data.feed.chainflowtrading.com";

// F-16: hardening.
//
// 1. **Method allowlist.** TradingView only ever GETs candles. POST/
//    PUT/DELETE here can't accomplish anything useful but they DO let
//    an attacker bounce arbitrary bodies through the proxy. Reject
//    everything but GET/HEAD.
// 2. **Path allowlist.** The catch-all segment is forwarded verbatim
//    to the upstream. Without a check, `..` in the path could let a
//    request break out of the indexer's `/api/<sym>/` namespace. Only
//    accept a fixed list of TradingView endpoint names.
// 3. **Query allowlist.** Only the params TradingView's UDF protocol
//    actually uses; everything else gets dropped.
// 4. **Upstream timeout.** 8 s wall clock so a stalled upstream
//    doesn't pin a serverless function indefinitely.
// 5. **Per-IP rate limit.** Crude in-memory token bucket — not a
//    replacement for CDN-level limits, but stops a single abusive
//    client from running the function quota to zero.

const ALLOWED_METHODS = new Set(["GET", "HEAD"]);
const ALLOWED_ENDPOINTS = new Set([
  "config",
  "symbols",
  "search",
  "history",
  "marks",
  "timescale_marks",
  "time",
  "quotes",
]);
const ALLOWED_QUERY_KEYS = new Set([
  "symbol",
  "symbols",
  "query",
  "resolution",
  "from",
  "to",
  "countback",
  "type",
  "exchange",
  "limit",
  "user",
  "session",
]);

// Symbols that have a dedicated upstream behind the market proxy.
const PROXIED_SYMBOLS: ReadonlySet<string> = new Set([
  "BTC", "ETH", "SOL", "AVAX", "LINK", "HYPE",
  "XRP", "ASTER", "TRUMP", "BNB", "SID", "PAX",
  "TSLA", "NVDA", "NAS100", "SPX500", "GOOGL", "XAU",
]);

const SAFE_SYMBOL = /^[A-Za-z0-9_\-\.]{1,32}$/;
const SAFE_QUERY_VALUE = /^[A-Za-z0-9_\-\.,:\/ ]{0,128}$/;

const UPSTREAM_TIMEOUT_MS = 8_000;

// Rolling 1-minute window token bucket per IP. 120 req/min is well
// above what a single TradingView session needs (~one per resolution
// switch + one per visible candle bar), but low enough that a
// scraping client gets cut off.
const RATE_LIMIT_PER_MIN = 120;
const RATE_LIMIT_WINDOW_MS = 60_000;
const ipBuckets = new Map<string, { count: number; windowStart: number }>();

function clientIp(req: NextApiRequest): string {
  const fwd = req.headers["x-forwarded-for"];
  if (typeof fwd === "string" && fwd.length > 0) return fwd.split(",")[0].trim();
  if (Array.isArray(fwd) && fwd[0]) return fwd[0].split(",")[0].trim();
  return req.socket.remoteAddress ?? "unknown";
}

function checkRateLimit(ip: string): boolean {
  const now = Date.now();
  const bucket = ipBuckets.get(ip);
  if (!bucket || now - bucket.windowStart > RATE_LIMIT_WINDOW_MS) {
    ipBuckets.set(ip, { count: 1, windowStart: now });
    // Periodically prune old entries to bound memory.
    if (ipBuckets.size > 5_000) {
      for (const [k, v] of ipBuckets) {
        if (now - v.windowStart > RATE_LIMIT_WINDOW_MS) ipBuckets.delete(k);
      }
    }
    return true;
  }
  if (bucket.count >= RATE_LIMIT_PER_MIN) return false;
  bucket.count += 1;
  return true;
}

function getUpstream(symbol: string | undefined): string {
  if (!symbol) return DEFAULT_UPSTREAM;
  const sym = symbol.toUpperCase().replace("PERP_", "").replace("_USDC", "");
  if (PROXIED_SYMBOLS.has(sym)) {
    return `https://${MARKET_PROXY_HOST}/api/${sym.toLowerCase()}`;
  }
  return DEFAULT_UPSTREAM;
}

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  const method = (req.method ?? "GET").toUpperCase();
  if (!ALLOWED_METHODS.has(method)) {
    res.setHeader("Allow", "GET, HEAD");
    return res.status(405).json({ error: "Method not allowed" });
  }

  const ip = clientIp(req);
  if (!checkRateLimit(ip)) {
    res.setHeader("Retry-After", "60");
    return res.status(429).json({ error: "Too many requests" });
  }

  const { path, ...query } = req.query;
  const segments = Array.isArray(path) ? path : (path ? [path] : []);
  if (segments.length === 0) return res.status(400).json({ error: "Missing endpoint" });

  // Reject path traversal and any non-allowlisted endpoint name.
  const endpoint = String(segments[0]);
  if (!ALLOWED_ENDPOINTS.has(endpoint) || segments.length > 1) {
    return res.status(400).json({ error: "Unknown candle endpoint" });
  }

  // Build a filtered query string. Drop anything not on the allowlist
  // and reject obviously hostile values (whitespace, control chars,
  // etc.) before they hit the upstream.
  const qs = new URLSearchParams();
  for (const [key, val] of Object.entries(query)) {
    if (!ALLOWED_QUERY_KEYS.has(key)) continue;
    const raw = Array.isArray(val) ? val[0] : val;
    if (typeof raw !== "string") continue;
    if (!SAFE_QUERY_VALUE.test(raw)) continue;
    qs.set(key, raw);
  }

  const symbolParam = qs.get("symbol") ?? undefined;
  if (symbolParam && !SAFE_SYMBOL.test(symbolParam)) {
    return res.status(400).json({ error: "Invalid symbol" });
  }

  if (symbolParam) {
    const normalized = symbolParam.toUpperCase().replace(/^PERP_/, "").replace(/_USDC$/, "");
    if (!PROXIED_SYMBOLS.has(normalized)) return res.status(404).json({ error: "Unknown symbol" });
    qs.set("symbol", normalized);
  }
  if (endpoint === "marks" || endpoint === "timescale_marks") return res.status(200).json([]);
  if (endpoint === "search") {
    const query = (qs.get("query") || "").toUpperCase();
    return res.status(200).json([...PROXIED_SYMBOLS].filter(s => s.includes(query)).map(symbol => ({ symbol, ticker: symbol, full_name: symbol, description: symbol, exchange: "Hyperpax", type: "crypto" })));
  }

  const base = getUpstream(symbolParam);
  const upstream = `${base}/${endpoint}${qs.toString() ? `?${qs}` : ""}`;

  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(new Error("timeout")), UPSTREAM_TIMEOUT_MS);

  try {
    if (endpoint === "quotes") {
      const symbols = [...new Set((qs.get("symbols") || qs.get("symbol") || "SID").split(",").map(s => s.toUpperCase().replace(/^PERP_/, "").replace(/_USDC$/, "")))];
      if (symbols.length > 18 || symbols.some(s => !PROXIED_SYMBOLS.has(s))) return res.status(400).json({ error: "Invalid quote symbols" });
      const batches = await Promise.all(symbols.map(async symbol => {
        const response = await fetch(`${getUpstream(symbol)}/quotes?symbols=${encodeURIComponent(symbol)}`, {
          headers: { Accept: "application/json", Origin: "https://app.hyperpax.xyz" }, signal: abort.signal,
        });
        if (!response.ok) throw new Error(`Quote upstream HTTP ${response.status}`);
        const body = await response.json();
        return body.d || [];
      }));
      res.setHeader("Cache-Control", "private, no-store");
      return res.status(200).json({ s: "ok", d: batches.flat() });
    }
    const upstreamRes = await fetch(upstream, {
      method,
      headers: {
        Accept: "application/json",
        Origin: "https://app.hyperpax.xyz",
      },
      signal: abort.signal,
    });

    const contentType = upstreamRes.headers.get("content-type") || "";
    res.setHeader("Cache-Control", "public, s-maxage=2, stale-while-revalidate=5");

    if (contentType.includes("application/json")) {
      const data = await upstreamRes.json();
      return res.status(upstreamRes.status).json(data);
    }

    const text = await upstreamRes.text();
    return res.status(upstreamRes.status).send(text);
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    const isTimeout = abort.signal.aborted;
    console.warn("[candle proxy]", isTimeout ? "timeout" : "upstream error", message);
    return res
      .status(isTimeout ? 504 : 502)
      .json({ error: isTimeout ? "Upstream candle server timed out" : "Upstream candle server unreachable" });
  } finally {
    clearTimeout(timer);
  }
}
