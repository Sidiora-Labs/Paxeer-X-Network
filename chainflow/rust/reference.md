# ChainflowDataGateway Rust SDK Reference

## Operations

### client.gateway().list_health()

Check gateway liveness

- HTTP: `GET /health`
- Response body: `text/plain`
- Errors: `401`

### client.gateway().create_web_socket_token(…)

Mint a scoped WebSocket token

- HTTP: `POST /auth/ws-token`
- Request body: `application/json`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 413, 415, 429, 500, 502, 504`

### client.price_feeds().list(…)

Get the current feed price

- HTTP: `GET /api/{market}/price`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().list_health(…)

Get feed

- HTTP: `GET /api/{market}/health`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().list_spot_style_stats(…)

Get spot-style market statistics

- HTTP: `GET /api/{market}/stats`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().list_perpetual_stats(…)

Get perpetual-market statistics

- HTTP: `GET /api/{market}/perp_stats`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().list_trade_tape_status(…)

Get trade-tape storage status

- HTTP: `GET /api/{market}/trades`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().retrieve_recent_spot_trades(…)

Get recent spot trades

- HTTP: `GET /api/{market}/trades/{symbol}`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.price_feeds().retrieve_recent_perpetual_trades(…)

Get recent perpetual trades

- HTTP: `GET /api/{market}/perp_trades/{symbol}`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().list_datafeed_config(…)

Get UDF feed capabilities

- HTTP: `GET /api/{market}/config`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().list_feed_time(…)

Get server Unix time

- HTTP: `GET /api/{market}/time`
- Response body: `text/html`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().resolve_symbol(…)

Resolve UDF symbol metadata

- HTTP: `GET /api/{market}/symbols`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().search(…)

Search within one feed

- HTTP: `GET /api/{market}/search`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().list_candle_history(…)

Get OHLCV history

- HTTP: `GET /api/{market}/history`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 504`

### client.trading_view().list_quotes(…)

Get UDF quotes

- HTTP: `GET /api/{market}/quotes`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().list_health()

Get aggregator health

- HTTP: `GET /api/markets/health`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().list()

List display-scaled market summaries

- HTTP: `GET /api/markets/markets`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().retrieve(…)

Get one display-scaled market summary

- HTTP: `GET /api/markets/markets/{symbol}`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().list_totals()

Get display-scaled aggregate totals

- HTTP: `GET /api/markets/totals`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().list_tickers()

List fresh unscaled tickers

- HTTP: `GET /api/markets/tickers`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.markets().retrieve_ticker(…)

Get one fresh unscaled ticker

- HTTP: `GET /api/markets/ticker/{symbol}`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.markets().list_open_interest()

Get estimated long/short perpetual open interest

- HTTP: `GET /api/markets/open-interest`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.explorer().list_health()

Get EVM explorer health

- HTTP: `GET /api/explorer/health`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.explorer().list_blocks()

Get recent EVM blocks

- HTTP: `GET /api/explorer/blocks`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.explorer().list_transactions()

Get recent explorer activity

- HTTP: `GET /api/explorer/transactions`
- Response body: `application/json`
- Errors: `401, 403, 404, 405, 429, 500, 502, 504`

### client.coin_gecko().list()

List local CoinGecko-compatible IDs

- HTTP: `GET /api/v3/coins/list`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 504`

### client.coin_gecko().list_markets()

Get CoinGecko-compatible market data

- HTTP: `GET /api/v3/coins/markets`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_gecko().retrieve(…)

Get CoinGecko-compatible coin detail

- HTTP: `GET /api/v3/coins/{id}`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_gecko().list_simple_prices()

Get compact USD quotes

- HTTP: `GET /api/v3/simple/price`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_market_cap().list_quotes()

Get CMC-compatible latest quotes

- HTTP: `GET /v1/cryptocurrency/quotes/latest`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_market_cap().list_listings()

Get CMC-compatible latest listings

- HTTP: `GET /v1/cryptocurrency/listings/latest`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_market_cap().list_quotes_api_alias()

Get CMC-compatible latest quotes

- HTTP: `GET /api/v1/cryptocurrency/quotes/latest`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.coin_market_cap().list_listings_api_alias()

Get CMC-compatible latest listings

- HTTP: `GET /api/v1/cryptocurrency/listings/latest`
- Response body: `application/json`
- Errors: `400, 401, 403, 404, 405, 429, 500, 502, 503, 504`

### client.web_sockets().connect_price_feed(…)

Open {market} WebSocket

- HTTP: `GET /ws/{market}`
- Errors: `401, 403, 404, 405, 426, 429, 502, 504`

### client.web_sockets().connect_markets()

Open markets WebSocket

- HTTP: `GET /ws/markets`
- Errors: `401, 403, 404, 405, 426, 429, 502, 504`

### client.web_sockets().connect_explorer()

Open explorer WebSocket

- HTTP: `GET /ws/explorer`
- Errors: `401, 403, 404, 405, 426, 429, 502, 504`

### client.web_sockets().connect_explorer_alias()

Open explorer WebSocket

- HTTP: `GET /explorer`
- Errors: `401, 403, 404, 405, 426, 429, 502, 504`

## Models

- `Market`
- `Symbol`
- `CoinId`
- `Resolution`
- `UnixSeconds`
- `UnixMilliseconds`
- `IsoTimestamp`
- `ErrorModel`
- `UdfError`
- `Price`
- `FeedHealth`
- `FeedHealthError`
- `UdfConfig`
- `SymbolInfo`
- `SymbolSearchResult`
- `CandleHistory`
- `HistoryNoData`
- `QuoteValues`
- `Quote`
- `MissingQuote`
- `Quotes`
- `Stats`
- `PerpStats`
- `SpotTrade`
- `PerpTrade`
- `TradeTapeStatus`
- `SpotTrades`
- `PerpTrades`
- `WsTokenRequest`
- `WsToken`
- `MarketRecord`
- `Totals`
- `Ticker`
- `OpenInterest`
- `WsRuntimeStats`
- `MarketsHealth`
- `ExplorerHealth`
- `Block`
- `Transaction`
- `CoinListItem`
- `CoinSimpleQuote`
- `CoinMarket`
- `CmcStatus`
- `CmcUsdQuote`
- `CmcAsset`
- `CmcError`
- `CoinDetail`
- `WsScope`
