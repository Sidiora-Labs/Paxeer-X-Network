# Chainflow Data Gateway TypeScript API

Complete reference of every operation, grouped by resource. See [the README](./README.md) for usage and configuration.

## Contents

- [`Gateway`](#gateway)
  - [Check gateway liveness](#check-gateway-liveness)
  - [Mint a scoped WebSocket token](#mint-a-scoped-websocket-token)
- [`PriceFeeds`](#pricefeeds)
  - [Get the current feed price](#get-the-current-feed-price)
  - [Get feed](#get-feed)
  - [Get spot-style market statistics](#get-spot-style-market-statistics)
  - [Get perpetual-market statistics](#get-perpetual-market-statistics)
  - [Get trade-tape storage status](#get-trade-tape-storage-status)
  - [Get recent spot trades](#get-recent-spot-trades)
  - [Get recent perpetual trades](#get-recent-perpetual-trades)
- [`TradingView`](#tradingview)
  - [Get UDF feed capabilities](#get-udf-feed-capabilities)
  - [Get server Unix time](#get-server-unix-time)
  - [Resolve UDF symbol metadata](#resolve-udf-symbol-metadata)
  - [Search within one feed](#search-within-one-feed)
  - [Get OHLCV history](#get-ohlcv-history)
  - [Get UDF quotes](#get-udf-quotes)
- [`Markets`](#markets)
  - [Get aggregator health](#get-aggregator-health)
  - [List display-scaled market summaries](#list-display-scaled-market-summaries)
  - [Get one display-scaled market summary](#get-one-display-scaled-market-summary)
  - [Get display-scaled aggregate totals](#get-display-scaled-aggregate-totals)
  - [List fresh unscaled tickers](#list-fresh-unscaled-tickers)
  - [Get one fresh unscaled ticker](#get-one-fresh-unscaled-ticker)
  - [Get estimated long/short perpetual open interest](#get-estimated-longshort-perpetual-open-interest)
- [`Explorer`](#explorer)
  - [Get EVM explorer health](#get-evm-explorer-health)
  - [Get recent EVM blocks](#get-recent-evm-blocks)
  - [Get recent explorer activity](#get-recent-explorer-activity)
- [`CoinGecko`](#coingecko)
  - [List local CoinGecko-compatible IDs](#list-local-coingecko-compatible-ids)
  - [Get CoinGecko-compatible market data](#get-coingecko-compatible-market-data)
  - [Get CoinGecko-compatible coin detail](#get-coingecko-compatible-coin-detail)
  - [Get compact USD quotes](#get-compact-usd-quotes)
- [`CoinMarketCap`](#coinmarketcap)
  - [Get CMC-compatible latest quotes](#get-cmc-compatible-latest-quotes)
  - [Get CMC-compatible latest listings](#get-cmc-compatible-latest-listings)
  - [Get CMC-compatible latest quotes](#get-cmc-compatible-latest-quotes-1)
  - [Get CMC-compatible latest listings](#get-cmc-compatible-latest-listings-1)
- [`WebSockets`](#websockets)
  - [Open {market} WebSocket](#open-market-websocket)
  - [Open markets WebSocket](#open-markets-websocket)
  - [Open explorer WebSocket](#open-explorer-websocket)
  - [Open explorer WebSocket](#open-explorer-websocket-1)

## Setup

```ts
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

const client = new ChainflowDataGateway({
  restAPIKey: process.env['REST_API_KEY'], // defaults to the REST_API_KEY env var
});
```

## `Gateway`

Gateway health and WebSocket credential issuance.

### Check gateway liveness

Checks Nginx only; does not probe the auth helper, database or market services.

| Direction | Type |
| --- | --- |
| Response | [`GatewayListHealthResponse`](./src/resources/gateway.ts) |

```ts
const response = await client.gateway.listHealth();
```

### Mint a scoped WebSocket token

Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

| Direction | Type |
| --- | --- |
| Request | [`GatewayCreateWebSocketTokenParams`](./src/resources/gateway.ts) |
| Response | [`WsToken`](./src/resources/gateway.ts) |

```ts
const wsToken = await client.gateway.createWebSocketToken({ symbol: 'btc' });
```

## `PriceFeeds`

Per-symbol USD prices, statistics and tapes.

### Get the current feed price

HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

| Direction | Type |
| --- | --- |
| Response | [`PriceFeedListResponse`](./src/resources/price-feeds.ts) |

```ts
const priceFeed = await client.priceFeeds.list('btc');
```

### Get feed

| Direction | Type |
| --- | --- |
| Response | [`FeedHealth`](./src/resources/price-feeds.ts) |

```ts
const feedHealth = await client.priceFeeds.listHealth('btc');
```

### Get spot-style market statistics

| Direction | Type |
| --- | --- |
| Response | [`Stats`](./src/resources/price-feeds.ts) |

```ts
const stats = await client.priceFeeds.listSpotStyleStats('btc');
```

### Get perpetual-market statistics

| Direction | Type |
| --- | --- |
| Response | [`PerpStats`](./src/resources/price-feeds.ts) |

```ts
const perpStats = await client.priceFeeds.listPerpetualStats('btc');
```

### Get trade-tape storage status

This path reports tape storage status; use /trades/{symbol} for trade records.

| Direction | Type |
| --- | --- |
| Response | [`TradeTapeStatus`](./src/resources/price-feeds.ts) |

```ts
const tradeTapeStatus = await client.priceFeeds.listTradeTapeStatus('btc');
```

### Get recent spot trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedRetrieveRecentSpotTradesParams`](./src/resources/price-feeds.ts) |
| Response | [`SpotTrades`](./src/resources/price-feeds.ts) |

```ts
const spotTrades = await client.priceFeeds.retrieveRecentSpotTrades('symbol', {
  market: 'btc',
  limit: 100,
});
```

### Get recent perpetual trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedRetrieveRecentPerpetualTradesParams`](./src/resources/price-feeds.ts) |
| Response | [`PerpTrades`](./src/resources/price-feeds.ts) |

```ts
const perpTrades = await client.priceFeeds.retrieveRecentPerpetualTrades('symbol', {
  market: 'btc',
  limit: 100,
});
```

## `TradingView`

TradingView UDF configuration, metadata, quotes and candles.

### Get UDF feed capabilities

| Direction | Type |
| --- | --- |
| Response | [`UdfConfig`](./src/resources/trading-view.ts) |

```ts
const udfConfig = await client.tradingView.listDatafeedConfig('btc');
```

### Get server Unix time

An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

| Direction | Type |
| --- | --- |
| Response | [`TradingViewListFeedTimeResponse`](./src/resources/trading-view.ts) |

```ts
const response = await client.tradingView.listFeedTime('btc');
```

### Resolve UDF symbol metadata

Unknown or mismatched symbol returns an application error with HTTP 200.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewResolveSymbolParams`](./src/resources/trading-view.ts) |
| Response | [`TradingViewResolveSymbolResponse`](./src/resources/trading-view.ts) |

```ts
const tradingView = await client.tradingView.resolveSymbol('btc', {
  symbol: 'BTC',
});
```

### Search within one feed

This is not cross-market search. The endpoint ignores UDF exchange/type filters.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewSearchParams`](./src/resources/trading-view.ts) |
| Response | [`TradingViewSearchResponse`](./src/resources/trading-view.ts) |

```ts
const tradingView = await client.tradingView.search('btc', {
  query: '',
  limit: 30,
});
```

### Get OHLCV history

Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListCandleHistoryParams`](./src/resources/trading-view.ts) |
| Response | [`TradingViewListCandleHistoryResponse`](./src/resources/trading-view.ts) |

```ts
const tradingView = await client.tradingView.listCandleHistory('btc', {
  symbol: 'BTC',
  resolution: '1',
  from: 1,
  to: 1,
  countback: 1000,
});
```

### Get UDF quotes

Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListQuotesParams`](./src/resources/trading-view.ts) |
| Response | [`TradingViewListQuotesResponse`](./src/resources/trading-view.ts) |

```ts
const tradingView = await client.tradingView.listQuotes('btc');
```

## `Markets`

Cross-market summaries and ticker adapters.

### Get aggregator health

| Direction | Type |
| --- | --- |
| Response | [`MarketsHealth`](./src/resources/markets.ts) |

```ts
const marketsHealth = await client.markets.listHealth();
```

### List display-scaled market summaries

| Direction | Type |
| --- | --- |
| Response | [`MarketListResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.list();
```

### Get one display-scaled market summary

| Direction | Type |
| --- | --- |
| Response | [`MarketRetrieveResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.retrieve('BTC');
```

### Get display-scaled aggregate totals

| Direction | Type |
| --- | --- |
| Response | [`MarketListTotalsResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.listTotals();
```

### List fresh unscaled tickers

Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

| Direction | Type |
| --- | --- |
| Response | [`MarketListTickersResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.listTickers();
```

### Get one fresh unscaled ticker

Unknown, missing or stale prices return HTTP 503, not 404.

| Direction | Type |
| --- | --- |
| Response | [`MarketRetrieveTickerResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.retrieveTicker('symbol');
```

### Get estimated long/short perpetual open interest

| Direction | Type |
| --- | --- |
| Response | [`MarketListOpenInterestResponse`](./src/resources/markets.ts) |

```ts
const market = await client.markets.listOpenInterest();
```

## `Explorer`

EVM blocks and activity.

### Get EVM explorer health

| Direction | Type |
| --- | --- |
| Response | [`ExplorerHealth`](./src/resources/explorer.ts) |

```ts
const explorerHealth = await client.explorer.listHealth();
```

### Get recent EVM blocks

In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListBlocksParams`](./src/resources/explorer.ts) |
| Response | [`ExplorerListBlocksResponse`](./src/resources/explorer.ts) |

```ts
const explorer = await client.explorer.listBlocks({
  limit: 20,
});
```

### Get recent explorer activity

Explorer activity.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListTransactionsParams`](./src/resources/explorer.ts) |
| Response | [`ExplorerListTransactionsResponse`](./src/resources/explorer.ts) |

```ts
const explorer = await client.explorer.listTransactions({
  limit: 50,
});
```

## `CoinGecko`

USD-only CoinGecko-compatible local-feed formats.

### List local CoinGecko-compatible IDs

List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListParams`](./src/resources/coin-gecko.ts) |
| Response | [`CoinGeckoListResponse`](./src/resources/coin-gecko.ts) |

```ts
const coinGecko = await client.coinGecko.list({
  include_platform: false,
});
```

### Get CoinGecko-compatible market data

Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListMarketsParams`](./src/resources/coin-gecko.ts) |
| Response | [`CoinGeckoListMarketsResponse`](./src/resources/coin-gecko.ts) |

```ts
const coinGecko = await client.coinGecko.listMarkets({
  vs_currency: 'usd',
  page: 1,
  per_page: 100,
  order: 'market_cap_desc',
  sparkline: false,
  precision: 'full',
  locale: 'en',
});
```

### Get CoinGecko-compatible coin detail

Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoRetrieveParams`](./src/resources/coin-gecko.ts) |
| Response | [`CoinDetail`](./src/resources/coin-gecko.ts) |

```ts
const coinDetail = await client.coinGecko.retrieve('bitcoin', {
  localization: false,
  tickers: false,
  community_data: false,
  developer_data: false,
  sparkline: false,
  market_data: true,
});
```

### Get compact USD quotes

Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListSimplePricesParams`](./src/resources/coin-gecko.ts) |
| Response | [`CoinGeckoListSimplePricesResponse`](./src/resources/coin-gecko.ts) |

```ts
const coinGecko = await client.coinGecko.listSimplePrices({
  vs_currencies: 'usd',
  include_market_cap: false,
  include_24hr_vol: false,
  include_24hr_change: false,
  include_last_updated_at: false,
});
```

## `CoinMarketCap`

USD-only CMC-compatible local-feed formats.

### Get CMC-compatible latest quotes

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesParams`](./src/resources/coin-market-cap.ts) |
| Response | [`CoinMarketCapListQuotesResponse`](./src/resources/coin-market-cap.ts) |

```ts
const coinMarketCap = await client.coinMarketCap.listQuotes({
  convert: 'USD',
});
```

### Get CMC-compatible latest listings

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsParams`](./src/resources/coin-market-cap.ts) |
| Response | [`CoinMarketCapListListingsResponse`](./src/resources/coin-market-cap.ts) |

```ts
const coinMarketCap = await client.coinMarketCap.listListings({
  convert: 'USD',
  start: 1,
  limit: 100,
  sort: 'market_cap',
  sort_dir: 'desc',
});
```

### Get CMC-compatible latest quotes

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesAPIAliasParams`](./src/resources/coin-market-cap.ts) |
| Response | [`CoinMarketCapListQuotesAPIAliasResponse`](./src/resources/coin-market-cap.ts) |

```ts
const coinMarketCap = await client.coinMarketCap.listQuotesAPIAlias({
  convert: 'USD',
});
```

### Get CMC-compatible latest listings

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsAPIAliasParams`](./src/resources/coin-market-cap.ts) |
| Response | [`CoinMarketCapListListingsAPIAliasResponse`](./src/resources/coin-market-cap.ts) |

```ts
const coinMarketCap = await client.coinMarketCap.listListingsAPIAlias({
  convert: 'USD',
  start: 1,
  limit: 100,
  sort: 'market_cap',
  sort_dir: 'desc',
});
```

## `WebSockets`

Upgrade handshakes; application messages are described by x-websocket.

### Open {market} WebSocket

One connection addresses one feed. SYMBOL is the uppercase counterpart of lowercase market; no cross-symbol subscriptions. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```ts
const connection = client.webSockets.connectPriceFeed('btc');

try {
  for await (const message of connection) {
    console.log(message);
  }
} finally {
  connection.close();
}
```

### Open markets WebSocket

Aggregate socket. Initial snapshots and full replacements; default broadcast cadence is 5 seconds. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```ts
const connection = client.webSockets.connectMarkets();

try {
  for await (const message of connection) {
    console.log(message);
  }
} finally {
  connection.close();
}
```

### Open explorer WebSocket

EVM blocks and a separate activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```ts
const connection = client.webSockets.connectExplorer();

try {
  for await (const message of connection) {
    console.log(message);
  }
} finally {
  connection.close();
}
```

### Open explorer WebSocket

Alias of /ws/explorer. EVM blocks and a separate  activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```ts
const connection = client.webSockets.connectExplorerAlias();

try {
  for await (const message of connection) {
    console.log(message);
  }
} finally {
  connection.close();
}
```
