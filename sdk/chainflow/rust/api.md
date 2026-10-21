# Chainflow Data Gateway Rust API

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

```rust
use chainflow_data_gateway::*;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = ChainflowDataGatewayClient::from_env()?;

    // ... the samples below go here

    Ok(())
}
```

## `Gateway`

Gateway health and WebSocket credential issuance.

### Check gateway liveness

Checks Nginx only; does not probe the auth helper, database or market services.

| Direction | Type |
| --- | --- |
| Response | `bytes::Bytes` |

```rust
let response = client.gateway().list_health().send().await?;
```

### Mint a scoped WebSocket token

Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

| Direction | Type |
| --- | --- |
| Request | [`WsTokenRequest`](./src/models/gateway.rs) |
| Response | [`WsToken`](./src/models/gateway.rs) |

```rust
let response = client
    .gateway()
    .create_web_socket_token(WsTokenRequest { symbol: WsScope::Btc })
    .send()
    .await?;
```

## `PriceFeeds`

Per-symbol USD prices, statistics and tapes.

### Get the current feed price

HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

| Direction | Type |
| --- | --- |
| Response | [`PriceFeedsListResponse`](./src/models/price_feeds.rs) |

```rust
let response = client.price_feeds().list(Market::Btc).send().await?;
```

### Get feed

| Direction | Type |
| --- | --- |
| Response | [`FeedHealth`](./src/models/price_feeds.rs) |

```rust
let response = client.price_feeds().list_health(Market::Btc).send().await?;
```

### Get spot-style market statistics

| Direction | Type |
| --- | --- |
| Response | [`Stats`](./src/models/price_feeds.rs) |

```rust
let response = client.price_feeds().list_spot_style_stats(Market::Btc).send().await?;
```

### Get perpetual-market statistics

| Direction | Type |
| --- | --- |
| Response | [`PerpStats`](./src/models/price_feeds.rs) |

```rust
let response = client.price_feeds().list_perpetual_stats(Market::Btc).send().await?;
```

### Get trade-tape storage status

This path reports tape storage status; use /trades/{symbol} for trade records.

| Direction | Type |
| --- | --- |
| Response | [`TradeTapeStatus`](./src/models/price_feeds.rs) |

```rust
let response = client.price_feeds().list_trade_tape_status(Market::Btc).send().await?;
```

### Get recent spot trades

| Direction | Type |
| --- | --- |
| Response | [`SpotTrades`](./src/models/price_feeds.rs) |

```rust
let response = client
    .price_feeds()
    .retrieve_recent_spot_trades(Market::Btc, "example")
    .send()
    .await?;
```

### Get recent perpetual trades

| Direction | Type |
| --- | --- |
| Response | [`PerpTrades`](./src/models/price_feeds.rs) |

```rust
let response = client
    .price_feeds()
    .retrieve_recent_perpetual_trades(Market::Btc, "example")
    .send()
    .await?;
```

## `TradingView`

TradingView UDF configuration, metadata, quotes and candles.

### Get UDF feed capabilities

| Direction | Type |
| --- | --- |
| Response | [`UdfConfig`](./src/models/trading_view.rs) |

```rust
let response = client.trading_view().list_datafeed_config(Market::Btc).send().await?;
```

### Get server Unix time

An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

| Direction | Type |
| --- | --- |
| Response | `bytes::Bytes` |

```rust
let response = client.trading_view().list_feed_time(Market::Btc).send().await?;
```

### Resolve UDF symbol metadata

Unknown or mismatched symbol returns an application error with HTTP 200.

| Direction | Type |
| --- | --- |
| Response | [`TradingViewResolveSymbolResponse`](./src/models/trading_view.rs) |

```rust
let response = client
    .trading_view()
    .resolve_symbol(Market::Btc, Symbol::Btc)
    .send()
    .await?;
```

### Search within one feed

This is not cross-market search. The endpoint ignores UDF exchange/type filters.

| Direction | Type |
| --- | --- |
| Response | [`Vec<SymbolSearchResult>`](./src/models/trading_view.rs) |

```rust
let response = client.trading_view().search(Market::Btc).send().await?;
```

### Get OHLCV history

Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

| Direction | Type |
| --- | --- |
| Response | [`TradingViewListCandleHistoryResponse`](./src/models/trading_view.rs) |

```rust
let response = client
    .trading_view()
    .list_candle_history(Market::Btc, Symbol::Btc, Resolution::_1, 1, 1)
    .send()
    .await?;
```

### Get UDF quotes

Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

| Direction | Type |
| --- | --- |
| Response | [`TradingViewListQuotesResponse`](./src/models/trading_view.rs) |

```rust
let response = client.trading_view().list_quotes(Market::Btc).send().await?;
```

## `Markets`

Cross-market summaries and ticker adapters.

### Get aggregator health

| Direction | Type |
| --- | --- |
| Response | [`MarketsHealth`](./src/models/markets.rs) |

```rust
let response = client.markets().list_health().send().await?;
```

### List display-scaled market summaries

| Direction | Type |
| --- | --- |
| Response | [`MarketsListResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().list().send().await?;
```

### Get one display-scaled market summary

| Direction | Type |
| --- | --- |
| Response | [`MarketsRetrieveResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().retrieve(Symbol::Btc).send().await?;
```

### Get display-scaled aggregate totals

| Direction | Type |
| --- | --- |
| Response | [`MarketsListTotalsResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().list_totals().send().await?;
```

### List fresh unscaled tickers

Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

| Direction | Type |
| --- | --- |
| Response | [`MarketsListTickersResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().list_tickers().send().await?;
```

### Get one fresh unscaled ticker

Unknown, missing or stale prices return HTTP 503, not 404.

| Direction | Type |
| --- | --- |
| Response | [`MarketsRetrieveTickerResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().retrieve_ticker("example").send().await?;
```

### Get estimated long/short perpetual open interest

| Direction | Type |
| --- | --- |
| Response | [`MarketsListOpenInterestResponse`](./src/models/markets.rs) |

```rust
let response = client.markets().list_open_interest().send().await?;
```

## `Explorer`

EVM blocks and activity.

### Get EVM explorer health

| Direction | Type |
| --- | --- |
| Response | [`ExplorerHealth`](./src/models/explorer.rs) |

```rust
let response = client.explorer().list_health().send().await?;
```

### Get recent EVM blocks

In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

| Direction | Type |
| --- | --- |
| Response | [`ExplorerListBlocksResponse`](./src/models/explorer.rs) |

```rust
let response = client.explorer().list_blocks().send().await?;
```

### Get recent explorer activity

Explorer activity.

| Direction | Type |
| --- | --- |
| Response | [`ExplorerListTransactionsResponse`](./src/models/explorer.rs) |

```rust
let response = client.explorer().list_transactions().send().await?;
```

## `CoinGecko`

USD-only CoinGecko-compatible local-feed formats.

### List local CoinGecko-compatible IDs

List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Response | [`Vec<CoinListItem>`](./src/models/coin_gecko.rs) |

```rust
let response = client.coin_gecko().list().send().await?;
```

### Get CoinGecko-compatible market data

Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Response | [`Vec<CoinMarket>`](./src/models/coin_gecko.rs) |

```rust
let response = client.coin_gecko().list_markets().send().await?;
```

### Get CoinGecko-compatible coin detail

Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Response | [`CoinDetail`](./src/models/coin_gecko.rs) |

```rust
let response = client.coin_gecko().retrieve(CoinId::Bitcoin).send().await?;
```

### Get compact USD quotes

Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Response | `std::collections::HashMap<String, CoinSimpleQuote>` |

```rust
let response = client.coin_gecko().list_simple_prices().send().await?;
```

## `CoinMarketCap`

USD-only CMC-compatible local-feed formats.

### Get CMC-compatible latest quotes

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Response | [`CoinMarketCapListQuotesResponse`](./src/models/coin_market_cap.rs) |

```rust
let response = client.coin_market_cap().list_quotes().send().await?;
```

### Get CMC-compatible latest listings

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Response | [`CoinMarketCapListListingsResponse`](./src/models/coin_market_cap.rs) |

```rust
let response = client.coin_market_cap().list_listings().send().await?;
```

### Get CMC-compatible latest quotes

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Response | [`CoinMarketCapListQuotesApiAliasResponse`](./src/models/coin_market_cap.rs) |

```rust
let response = client.coin_market_cap().list_quotes_api_alias().send().await?;
```

### Get CMC-compatible latest listings

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Response | [`CoinMarketCapListListingsApiAliasResponse`](./src/models/coin_market_cap.rs) |

```rust
let response = client.coin_market_cap().list_listings_api_alias().send().await?;
```

## `WebSockets`

Upgrade handshakes; application messages are described by x-websocket.

### Open {market} WebSocket

One connection addresses one feed. SYMBOL is the uppercase counterpart of lowercase market; no cross-symbol subscriptions. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

| Direction | Type |
| --- | --- |
| Response | [`WebSocketConnection<serde_json::Value, serde_json::Value>`](./src/websocket.rs) |

```rust
let _connection = client.web_sockets().connect_price_feed(Market::Btc).open().await?;
```

### Open markets WebSocket

Aggregate socket. Initial snapshots and full replacements; default broadcast cadence is 5 seconds. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

| Direction | Type |
| --- | --- |
| Response | [`WebSocketConnection<serde_json::Value, serde_json::Value>`](./src/websocket.rs) |

```rust
let _connection = client.web_sockets().connect_markets().open().await?;
```

### Open explorer WebSocket

EVM blocks and a separate activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

| Direction | Type |
| --- | --- |
| Response | [`WebSocketConnection<serde_json::Value, serde_json::Value>`](./src/websocket.rs) |

```rust
let _connection = client.web_sockets().connect_explorer().open().await?;
```

### Open explorer WebSocket

Alias of /ws/explorer. EVM blocks and a separate  activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

| Direction | Type |
| --- | --- |
| Response | [`WebSocketConnection<serde_json::Value, serde_json::Value>`](./src/websocket.rs) |

```rust
let _connection = client.web_sockets().connect_explorer_alias().open().await?;
```
