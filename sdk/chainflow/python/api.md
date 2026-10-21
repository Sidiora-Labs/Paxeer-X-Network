# Chainflow Data Gateway Python API

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

```python
import os

from chainflow_data_gateway import ChainflowDataGateway

client = ChainflowDataGateway(
    rest_api_key=os.environ.get("REST_API_KEY"),
)
```

## `Gateway`

Gateway health and WebSocket credential issuance.

### Check gateway liveness

Checks Nginx only; does not probe the auth helper, database or market services.

| Direction | Type |
| --- | --- |
| Response | [`GatewayListHealthResponse`](./src/chainflow_data_gateway/types/gateway_list_health_response.py) |

```python
gateway = client.gateway.list_health()
```

### Mint a scoped WebSocket token

Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

| Direction | Type |
| --- | --- |
| Request | [`GatewayCreateWebSocketTokenParams`](./src/chainflow_data_gateway/types/gateway_create_web_socket_token_params.py) |
| Response | [`WsToken`](./src/chainflow_data_gateway/types/ws_token.py) |

```python
gateway = client.gateway.create_web_socket_token(
    symbol="btc",
)
```

## `PriceFeeds`

Per-symbol USD prices, statistics and tapes.

### Get the current feed price

HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

| Direction | Type |
| --- | --- |
| Response | [`PriceFeedListResponse`](./src/chainflow_data_gateway/types/price_feed_list_response.py) |

```python
price_feed = client.price_feeds.list(
    market="btc",
)
```

### Get feed

| Direction | Type |
| --- | --- |
| Response | [`FeedHealth`](./src/chainflow_data_gateway/types/feed_health.py) |

```python
price_feed = client.price_feeds.list_health(
    market="btc",
)
```

### Get spot-style market statistics

| Direction | Type |
| --- | --- |
| Response | [`Stats`](./src/chainflow_data_gateway/types/stats.py) |

```python
price_feed = client.price_feeds.list_spot_style_stats(
    market="btc",
)
```

### Get perpetual-market statistics

| Direction | Type |
| --- | --- |
| Response | [`PerpStats`](./src/chainflow_data_gateway/types/perp_stats.py) |

```python
price_feed = client.price_feeds.list_perpetual_stats(
    market="btc",
)
```

### Get trade-tape storage status

This path reports tape storage status; use /trades/{symbol} for trade records.

| Direction | Type |
| --- | --- |
| Response | [`TradeTapeStatus`](./src/chainflow_data_gateway/types/trade_tape_status.py) |

```python
price_feed = client.price_feeds.list_trade_tape_status(
    market="btc",
)
```

### Get recent spot trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedRetrieveRecentSpotTradesParams`](./src/chainflow_data_gateway/types/price_feed_retrieve_recent_spot_trades_params.py) |
| Response | [`SpotTrades`](./src/chainflow_data_gateway/types/spot_trades.py) |

```python
price_feed = client.price_feeds.retrieve_recent_spot_trades(
    market="btc",
    symbol="symbol",
    limit=100,
)
```

### Get recent perpetual trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedRetrieveRecentPerpetualTradesParams`](./src/chainflow_data_gateway/types/price_feed_retrieve_recent_perpetual_trades_params.py) |
| Response | [`PerpTrades`](./src/chainflow_data_gateway/types/perp_trades.py) |

```python
price_feed = client.price_feeds.retrieve_recent_perpetual_trades(
    market="btc",
    symbol="symbol",
    limit=100,
)
```

## `TradingView`

TradingView UDF configuration, metadata, quotes and candles.

### Get UDF feed capabilities

| Direction | Type |
| --- | --- |
| Response | [`UdfConfig`](./src/chainflow_data_gateway/types/udf_config.py) |

```python
trading_view = client.trading_view.list_datafeed_config(
    market="btc",
)
```

### Get server Unix time

An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

| Direction | Type |
| --- | --- |
| Response | [`TradingViewListFeedTimeResponse`](./src/chainflow_data_gateway/types/trading_view_list_feed_time_response.py) |

```python
trading_view = client.trading_view.list_feed_time(
    market="btc",
)
```

### Resolve UDF symbol metadata

Unknown or mismatched symbol returns an application error with HTTP 200.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewResolveSymbolParams`](./src/chainflow_data_gateway/types/trading_view_resolve_symbol_params.py) |
| Response | [`TradingViewResolveSymbolResponse`](./src/chainflow_data_gateway/types/trading_view_resolve_symbol_response.py) |

```python
trading_view = client.trading_view.resolve_symbol(
    market="btc",
    symbol="BTC",
)
```

### Search within one feed

This is not cross-market search. The endpoint ignores UDF exchange/type filters.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewSearchParams`](./src/chainflow_data_gateway/types/trading_view_search_params.py) |
| Response | [`TradingViewSearchResponse`](./src/chainflow_data_gateway/types/trading_view_search_response.py) |

```python
trading_view = client.trading_view.search(
    market="btc",
    query="",
    limit=30,
)
```

### Get OHLCV history

Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListCandleHistoryParams`](./src/chainflow_data_gateway/types/trading_view_list_candle_history_params.py) |
| Response | [`TradingViewListCandleHistoryResponse`](./src/chainflow_data_gateway/types/trading_view_list_candle_history_response.py) |

```python
trading_view = client.trading_view.list_candle_history(
    market="btc",
    symbol="BTC",
    resolution="1",
    from_=1,
    to=1,
    countback=1000,
)
```

### Get UDF quotes

Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListQuotesParams`](./src/chainflow_data_gateway/types/trading_view_list_quotes_params.py) |
| Response | [`TradingViewListQuotesResponse`](./src/chainflow_data_gateway/types/trading_view_list_quotes_response.py) |

```python
trading_view = client.trading_view.list_quotes(
    market="btc",
)
```

## `Markets`

Cross-market summaries and ticker adapters.

### Get aggregator health

| Direction | Type |
| --- | --- |
| Response | [`MarketsHealth`](./src/chainflow_data_gateway/types/markets_health.py) |

```python
market = client.markets.list_health()
```

### List display-scaled market summaries

| Direction | Type |
| --- | --- |
| Response | [`MarketListResponse`](./src/chainflow_data_gateway/types/market_list_response.py) |

```python
market = client.markets.list()
```

### Get one display-scaled market summary

| Direction | Type |
| --- | --- |
| Response | [`MarketRetrieveResponse`](./src/chainflow_data_gateway/types/market_retrieve_response.py) |

```python
market = client.markets.retrieve(
    symbol="BTC",
)
```

### Get display-scaled aggregate totals

| Direction | Type |
| --- | --- |
| Response | [`MarketListTotalsResponse`](./src/chainflow_data_gateway/types/market_list_totals_response.py) |

```python
market = client.markets.list_totals()
```

### List fresh unscaled tickers

Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

| Direction | Type |
| --- | --- |
| Response | [`MarketListTickersResponse`](./src/chainflow_data_gateway/types/market_list_tickers_response.py) |

```python
market = client.markets.list_tickers()
```

### Get one fresh unscaled ticker

Unknown, missing or stale prices return HTTP 503, not 404.

| Direction | Type |
| --- | --- |
| Response | [`MarketRetrieveTickerResponse`](./src/chainflow_data_gateway/types/market_retrieve_ticker_response.py) |

```python
market = client.markets.retrieve_ticker(
    symbol="symbol",
)
```

### Get estimated long/short perpetual open interest

| Direction | Type |
| --- | --- |
| Response | [`MarketListOpenInterestResponse`](./src/chainflow_data_gateway/types/market_list_open_interest_response.py) |

```python
market = client.markets.list_open_interest()
```

## `Explorer`

EVM blocks and activity.

### Get EVM explorer health

| Direction | Type |
| --- | --- |
| Response | [`ExplorerHealth`](./src/chainflow_data_gateway/types/explorer_health.py) |

```python
explorer = client.explorer.list_health()
```

### Get recent EVM blocks

In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListBlocksParams`](./src/chainflow_data_gateway/types/explorer_list_blocks_params.py) |
| Response | [`ExplorerListBlocksResponse`](./src/chainflow_data_gateway/types/explorer_list_blocks_response.py) |

```python
explorer = client.explorer.list_blocks(
    limit=20,
)
```

### Get recent explorer activity

Explorer activity.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListTransactionsParams`](./src/chainflow_data_gateway/types/explorer_list_transactions_params.py) |
| Response | [`ExplorerListTransactionsResponse`](./src/chainflow_data_gateway/types/explorer_list_transactions_response.py) |

```python
explorer = client.explorer.list_transactions(
    limit=50,
)
```

## `CoinGecko`

USD-only CoinGecko-compatible local-feed formats.

### List local CoinGecko-compatible IDs

List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListParams`](./src/chainflow_data_gateway/types/coin_gecko_list_params.py) |
| Response | [`CoinGeckoListResponse`](./src/chainflow_data_gateway/types/coin_gecko_list_response.py) |

```python
coin_gecko = client.coin_gecko.list(
    include_platform=False,
)
```

### Get CoinGecko-compatible market data

Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListMarketsParams`](./src/chainflow_data_gateway/types/coin_gecko_list_markets_params.py) |
| Response | [`CoinGeckoListMarketsResponse`](./src/chainflow_data_gateway/types/coin_gecko_list_markets_response.py) |

```python
coin_gecko = client.coin_gecko.list_markets(
    vs_currency="usd",
    page=1,
    per_page=100,
    order="market_cap_desc",
    sparkline=False,
    precision="full",
    locale="en",
)
```

### Get CoinGecko-compatible coin detail

Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoRetrieveParams`](./src/chainflow_data_gateway/types/coin_gecko_retrieve_params.py) |
| Response | [`CoinDetail`](./src/chainflow_data_gateway/types/coin_detail.py) |

```python
coin_gecko = client.coin_gecko.retrieve(
    id="bitcoin",
    localization=False,
    tickers=False,
    community_data=False,
    developer_data=False,
    sparkline=False,
    market_data=True,
)
```

### Get compact USD quotes

Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListSimplePricesParams`](./src/chainflow_data_gateway/types/coin_gecko_list_simple_prices_params.py) |
| Response | [`CoinGeckoListSimplePricesResponse`](./src/chainflow_data_gateway/types/coin_gecko_list_simple_prices_response.py) |

```python
coin_gecko = client.coin_gecko.list_simple_prices(
    vs_currencies="usd",
    include_market_cap=False,
    include_24hr_vol=False,
    include_24hr_change=False,
    include_last_updated_at=False,
)
```

## `CoinMarketCap`

USD-only CMC-compatible local-feed formats.

### Get CMC-compatible latest quotes

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesParams`](./src/chainflow_data_gateway/types/coin_market_cap_list_quotes_params.py) |
| Response | [`CoinMarketCapListQuotesResponse`](./src/chainflow_data_gateway/types/coin_market_cap_list_quotes_response.py) |

```python
coin_market_cap = client.coin_market_cap.list_quotes(
    convert="USD",
)
```

### Get CMC-compatible latest listings

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsParams`](./src/chainflow_data_gateway/types/coin_market_cap_list_listings_params.py) |
| Response | [`CoinMarketCapListListingsResponse`](./src/chainflow_data_gateway/types/coin_market_cap_list_listings_response.py) |

```python
coin_market_cap = client.coin_market_cap.list_listings(
    convert="USD",
    start=1,
    limit=100,
    sort="market_cap",
    sort_dir="desc",
)
```

### Get CMC-compatible latest quotes

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesAPIAliasParams`](./src/chainflow_data_gateway/types/coin_market_cap_list_quotes_api_alias_params.py) |
| Response | [`CoinMarketCapListQuotesAPIAliasResponse`](./src/chainflow_data_gateway/types/coin_market_cap_list_quotes_api_alias_response.py) |

```python
coin_market_cap = client.coin_market_cap.list_quotes_api_alias(
    convert="USD",
)
```

### Get CMC-compatible latest listings

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsAPIAliasParams`](./src/chainflow_data_gateway/types/coin_market_cap_list_listings_api_alias_params.py) |
| Response | [`CoinMarketCapListListingsAPIAliasResponse`](./src/chainflow_data_gateway/types/coin_market_cap_list_listings_api_alias_response.py) |

```python
coin_market_cap = client.coin_market_cap.list_listings_api_alias(
    convert="USD",
    start=1,
    limit=100,
    sort="market_cap",
    sort_dir="desc",
)
```

## `WebSockets`

Upgrade handshakes; application messages are described by x-websocket.

### Open {market} WebSocket

One connection addresses one feed. SYMBOL is the uppercase counterpart of lowercase market; no cross-symbol subscriptions. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```python
with client.web_sockets.connect_price_feed(market="market") as connection:
    message = connection.recv()
    print(message)
```

### Open markets WebSocket

Aggregate socket. Initial snapshots and full replacements; default broadcast cadence is 5 seconds. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```python
with client.web_sockets.connect_markets() as connection:
    message = connection.recv()
    print(message)
```

### Open explorer WebSocket

EVM blocks and a separate activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```python
with client.web_sockets.connect_explorer() as connection:
    message = connection.recv()
    print(message)
```

### Open explorer WebSocket

Alias of /ws/explorer. EVM blocks and a separate  activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.

```python
with client.web_sockets.connect_explorer_alias() as connection:
    message = connection.recv()
    print(message)
```
