import test from "node:test";
import assert from "node:assert/strict";
import priceFeed from "../apps/web/src/pages/api/price-feed/[...path]";
import type { NextApiRequest, NextApiResponse } from "next";
import {
  getFeedOrigin,
  usesDirectMarketFeeds,
} from "../packages/paxeer-backend/src/core/feeds";

test("only approved production origins use the public REST gateway directly", () => {
  assert.equal(usesDirectMarketFeeds("https://app.hyperpax.xyz"), true);
  assert.equal(
    getFeedOrigin("https://app.hyperpax.xyz"),
    "https://data.feed.chainflowtrading.com/api",
  );
  for (const origin of [
    "http://localhost:3000",
    "http://127.0.0.1:3000",
    "https://preview.vercel.app",
    "https://app.hyperpax.xyz.example.com",
  ]) {
    assert.equal(usesDirectMarketFeeds(origin), false);
    assert.equal(getFeedOrigin(origin), "/api/price-feed");
  }
});

test("approved production sockets connect without fetching or attaching a token", async () => {
  const { authenticatedMarketSocketUrl } =
    await import("../packages/paxeer-backend/src/core/authenticatedMarketSocket");
  const beforeWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const beforeFetch = globalThis.fetch;
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: { location: { origin: "https://app.hyperpax.xyz" } },
  });
  globalThis.fetch = async () =>
    assert.fail("Direct production sockets must not mint tokens");
  try {
    const target = new URL(
      await authenticatedMarketSocketUrl(
        "wss://data.feed.chainflowtrading.com/ws/eth?token=obsolete",
      ),
    );
    assert.equal(target.origin, "wss://data.feed.chainflowtrading.com");
    assert.equal(target.pathname, "/ws/eth");
    assert.equal(target.searchParams.has("token"), false);
  } finally {
    globalThis.fetch = beforeFetch;
    if (beforeWindow) Object.defineProperty(globalThis, "window", beforeWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
});
function response() {
  const result = {
    statusCode: 200,
    headers: new Map<string, string>(),
    body: undefined as unknown,
  };
  const res = {
    setHeader(name: string, value: string) {
      result.headers.set(name.toLowerCase(), value);
      return this;
    },
    status(status: number) {
      result.statusCode = status;
      return this;
    },
    json(body: unknown) {
      result.body = body;
      return this;
    },
    send(body: unknown) {
      result.body = body;
      return this;
    },
    end() {
      return this;
    },
  } as unknown as NextApiResponse;
  return { result, res };
}
const request = (path: string[], extra = {}) =>
  ({
    method: "GET",
    query: { path },
    url: "/api/price-feed/" + path.join("/"),
    ...extra,
  }) as NextApiRequest;

test("price feed keeps authentication in the upstream header and strips arbitrary query parameters", async () => {
  const fetchBefore = globalThis.fetch,
    keyBefore = process.env.PRICE_FEED_REST_API_KEY;
  process.env.PRICE_FEED_REST_API_KEY = "server-only-test-key";
  const { result, res } = response();
  globalThis.fetch = async (url, init) => {
    const target = new URL(String(url));
    assert.equal(target.hostname, "data.feed.chainflowtrading.com");
    assert.equal(target.pathname, "/api/eth/history");
    assert.equal(target.searchParams.get("resolution"), "15");
    assert.equal(target.searchParams.has("api_key"), false);
    assert.equal(target.searchParams.has("url"), false);
    assert.equal(
      new Headers(init?.headers).get("X-API-Key"),
      "server-only-test-key",
    );
    return Response.json(
      { s: "no_data" },
      { headers: { "Set-Cookie": "upstream-only" } },
    );
  };
  try {
    await priceFeed(
      request(["eth", "history"], {
        url: "/api/price-feed/eth/history?resolution=15&from=100&to=200&api_key=client-input&url=https://example.com",
      }),
      res,
    );
    assert.equal(result.statusCode, 200);
    assert.equal(result.headers.has("set-cookie"), false);
    assert.equal(result.headers.get("cache-control"), "no-store");
    assert.equal(String(result.body).includes("server-only-test-key"), false);
  } finally {
    globalThis.fetch = fetchBefore;
    if (keyBefore === undefined) delete process.env.PRICE_FEED_REST_API_KEY;
    else process.env.PRICE_FEED_REST_API_KEY = keyBefore;
  }
});

test("price feed rejects mutations, unrelated paths and unsupported symbols", async () => {
  for (const [req, expected] of [
    [request(["eth", "history"], { method: "POST" }), 405],
    [request(["markets", "admin"]), 404],
    [request(["markets", "ticker", "UNSUPPORTED"]), 404],
    [request(["eth", ".."]), 400],
  ] as const) {
    const { result, res } = response();
    await priceFeed(req, res);
    assert.equal(result.statusCode, expected);
  }
});

test("price feed fails explicitly when credentials are absent", async () => {
  const before = process.env.PRICE_FEED_REST_API_KEY;
  delete process.env.PRICE_FEED_REST_API_KEY;
  try {
    const { result, res } = response();
    await priceFeed(request(["markets", "tickers"]), res);
    assert.equal(result.statusCode, 503);
    assert.deepEqual(result.body, { error: "feed_not_configured" });
  } finally {
    if (before !== undefined) process.env.PRICE_FEED_REST_API_KEY = before;
  }
});

test("WebSocket credential minting uses the separate key and an exact symbol-only body", async () => {
  const previousFetch = globalThis.fetch;
  const previousRest = process.env.PRICE_FEED_REST_API_KEY,
    previousWs = process.env.PRICE_FEED_WS_API_KEY;
  process.env.PRICE_FEED_REST_API_KEY = "test-rest-key";
  process.env.PRICE_FEED_WS_API_KEY = "test-ws-key";
  globalThis.fetch = async (input, init) => {
    assert.equal(new URL(String(input)).pathname, "/auth/ws-token");
    assert.equal(new Headers(init?.headers).get("X-API-Key"), "test-ws-key");
    assert.deepEqual(JSON.parse(String(init?.body)), { symbol: "eth" });
    return Response.json({
      token: "opaque-test-credential",
      symbol: "eth",
      expiresAt: new Date(Date.now() + 300000).toISOString(),
      expiresIn: 300,
    });
  };
  try {
    const { result, res } = response();
    await priceFeed(
      request(["ws-token"], { query: { path: ["ws-token"], symbol: "eth" } }),
      res,
    );
    assert.equal(result.statusCode, 200);
    assert.equal(result.headers.get("cache-control"), "no-store");
    assert.equal(JSON.stringify(result.body).includes("test-ws-key"), false);
    assert.equal(JSON.stringify(result.body).includes("test-rest-key"), false);
    assert.equal((result.body as { symbol: string }).symbol, "eth");
  } finally {
    globalThis.fetch = previousFetch;
    if (previousRest === undefined) delete process.env.PRICE_FEED_REST_API_KEY;
    else process.env.PRICE_FEED_REST_API_KEY = previousRest;
    if (previousWs === undefined) delete process.env.PRICE_FEED_WS_API_KEY;
    else process.env.PRICE_FEED_WS_API_KEY = previousWs;
  }
});

test("socket reconnects request a new scoped credential without caching tokens", async () => {
  const { authenticatedMarketSocketUrl } =
    await import("../packages/paxeer-backend/src/core/authenticatedMarketSocket");
  const before = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async (input, init) => {
    assert.equal(String(input), "/api/price-feed/ws-token?symbol=eth");
    assert.equal(init?.cache, "no-store");
    return Response.json({
      symbol: "eth",
      token: `ephemeral-${++calls}`,
      expiresAt: new Date(Date.now() + 300000).toISOString(),
      url: "wss://data.feed.chainflowtrading.com/ws/eth",
    });
  };
  try {
    const first = await authenticatedMarketSocketUrl(
      "wss://data.feed.chainflowtrading.com/ws/eth",
    );
    const second = await authenticatedMarketSocketUrl(
      "wss://data.feed.chainflowtrading.com/ws/eth",
    );
    assert.equal(calls, 2);
    assert.notEqual(first, second);
  } finally {
    globalThis.fetch = before;
  }
});

test("upstream authentication errors cannot reflect credentials into the browser", async () => {
  const before = globalThis.fetch,
    beforeKey = process.env.PRICE_FEED_REST_API_KEY;
  process.env.PRICE_FEED_REST_API_KEY = "test-private-key";
  globalThis.fetch = async () =>
    Response.json({ error: "test-private-key" }, { status: 401 });
  try {
    const { result, res } = response();
    await priceFeed(request(["markets", "tickers"]), res);
    assert.equal(result.statusCode, 401);
    assert.deepEqual(result.body, { error: "feed_request_failed" });
  } finally {
    globalThis.fetch = before;
    if (beforeKey === undefined) delete process.env.PRICE_FEED_REST_API_KEY;
    else process.env.PRICE_FEED_REST_API_KEY = beforeKey;
  }
});

test("feed funding countdowns use durations anchored to the source timestamp", async () => {
  const { feedFundingDeadline } =
    await import("../packages/paxeer-backend/src/core/feedFunding");
  const sourceTime = Date.UTC(2026, 0, 1);
  assert.equal(
    feedFundingDeadline(sourceTime, 8 * 60 * 60 * 1000),
    sourceTime + 28_800_000,
  );
  assert.equal(feedFundingDeadline(sourceTime, 0), undefined);
  assert.equal(feedFundingDeadline(Number.NaN, 1000), undefined);
});
