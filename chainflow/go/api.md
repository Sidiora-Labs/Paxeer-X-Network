# Chainflow Data Gateway Go API

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

## Setup

```go
import (
	"context"
	"fmt"

	sdk "chainflow-data-gateway"
)

client := sdk.NewClient()
```

## `Gateway`

Gateway health and WebSocket credential issuance.

### Check gateway liveness

Checks Nginx only; does not probe the auth helper, database or market services.

| Direction | Type |
| --- | --- |
| Response | `string` |

```go
gateway, err := client.Gateway.ListHealth(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(gateway)
```

### Mint a scoped WebSocket token

Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

| Direction | Type |
| --- | --- |
| Request | [`GatewayNewWebSocketTokenParams`](./gateway.go) |
| Response | [`WsToken`](./gateway.go) |

```go
gateway, err := client.Gateway.NewWebSocketToken(context.Background(), sdk.GatewayNewWebSocketTokenParams{
	Symbol: sdk.F[sdk.WsScope](sdk.WsScope("btc")),
})
if err != nil {
	panic(err)
}

fmt.Println(gateway)
```

## `PriceFeeds`

Per-symbol USD prices, statistics and tapes.

### Get the current feed price

HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

| Direction | Type |
| --- | --- |
| Response | [`PriceFeedListResponse`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.List(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get feed

| Direction | Type |
| --- | --- |
| Response | [`FeedHealth`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.ListHealth(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get spot-style market statistics

| Direction | Type |
| --- | --- |
| Response | [`Stats`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.ListSpotStyleStats(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get perpetual-market statistics

| Direction | Type |
| --- | --- |
| Response | [`PerpStats`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.ListPerpetualStats(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get trade-tape storage status

This path reports tape storage status; use /trades/{symbol} for trade records.

| Direction | Type |
| --- | --- |
| Response | [`TradeTapeStatus`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.ListTradeTapeStatus(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get recent spot trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedGetRecentSpotTradesParams`](./pricefeed.go) |
| Response | [`SpotTrades`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.GetRecentSpotTrades(context.Background(), "btc", "symbol", sdk.PriceFeedGetRecentSpotTradesParams{
	Limit: sdk.F[int64](100),
})
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

### Get recent perpetual trades

| Direction | Type |
| --- | --- |
| Request | [`PriceFeedGetRecentPerpetualTradesParams`](./pricefeed.go) |
| Response | [`PerpTrades`](./pricefeed.go) |

```go
priceFeed, err := client.PriceFeeds.GetRecentPerpetualTrades(context.Background(), "btc", "symbol", sdk.PriceFeedGetRecentPerpetualTradesParams{
	Limit: sdk.F[int64](100),
})
if err != nil {
	panic(err)
}

fmt.Println(priceFeed)
```

## `TradingView`

TradingView UDF configuration, metadata, quotes and candles.

### Get UDF feed capabilities

| Direction | Type |
| --- | --- |
| Response | [`UdfConfig`](./tradingview.go) |

```go
tradingView, err := client.TradingView.ListDatafeedConfig(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

### Get server Unix time

An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

| Direction | Type |
| --- | --- |
| Response | `string` |

```go
tradingView, err := client.TradingView.ListFeedTime(context.Background(), "btc")
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

### Resolve UDF symbol metadata

Unknown or mismatched symbol returns an application error with HTTP 200.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewResolveSymbolParams`](./tradingview.go) |
| Response | [`TradingViewResolveSymbolResponse`](./tradingview.go) |

```go
tradingView, err := client.TradingView.ResolveSymbol(context.Background(), "btc", sdk.TradingViewResolveSymbolParams{
	Symbol: sdk.F[sdk.Symbol](sdk.Symbol("BTC")),
})
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

### Search within one feed

This is not cross-market search. The endpoint ignores UDF exchange/type filters.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewSearchParams`](./tradingview.go) |
| Response | [`[]SymbolSearchResult`](./tradingview.go) |

```go
tradingView, err := client.TradingView.Search(context.Background(), "btc", sdk.TradingViewSearchParams{
	Limit: sdk.F[int64](30),
	Query: sdk.F[string](""),
})
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

### Get OHLCV history

Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListCandleHistoryParams`](./tradingview.go) |
| Response | [`TradingViewListCandleHistoryResponse`](./tradingview.go) |

```go
tradingView, err := client.TradingView.ListCandleHistory(context.Background(), "btc", sdk.TradingViewListCandleHistoryParams{
	From:       sdk.F[int64](1),
	Resolution: sdk.F[sdk.TradingViewListCandleHistoryParamsResolution](sdk.TradingViewListCandleHistoryParamsResolution("1")),
	Symbol:     sdk.F[sdk.Symbol](sdk.Symbol("BTC")),
	To:         sdk.F[int64](1),
	Countback:  sdk.F[int64](1000),
})
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

### Get UDF quotes

Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

| Direction | Type |
| --- | --- |
| Request | [`TradingViewListQuotesParams`](./tradingview.go) |
| Response | [`TradingViewListQuotesResponse`](./tradingview.go) |

```go
tradingView, err := client.TradingView.ListQuotes(context.Background(), "btc", sdk.TradingViewListQuotesParams{})
if err != nil {
	panic(err)
}

fmt.Println(tradingView)
```

## `Markets`

Cross-market summaries and ticker adapters.

### Get aggregator health

| Direction | Type |
| --- | --- |
| Response | [`MarketsHealth`](./market.go) |

```go
market, err := client.Markets.ListHealth(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### List display-scaled market summaries

| Direction | Type |
| --- | --- |
| Response | [`MarketListResponse`](./market.go) |

```go
market, err := client.Markets.List(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### Get one display-scaled market summary

| Direction | Type |
| --- | --- |
| Response | [`MarketGetResponse`](./market.go) |

```go
market, err := client.Markets.Get(context.Background(), "BTC")
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### Get display-scaled aggregate totals

| Direction | Type |
| --- | --- |
| Response | [`MarketListTotalsResponse`](./market.go) |

```go
market, err := client.Markets.ListTotals(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### List fresh unscaled tickers

Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

| Direction | Type |
| --- | --- |
| Response | [`MarketListTickersResponse`](./market.go) |

```go
market, err := client.Markets.ListTickers(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### Get one fresh unscaled ticker

Unknown, missing or stale prices return HTTP 503, not 404.

| Direction | Type |
| --- | --- |
| Response | [`MarketGetTickerResponse`](./market.go) |

```go
market, err := client.Markets.GetTicker(context.Background(), "symbol")
if err != nil {
	panic(err)
}

fmt.Println(market)
```

### Get estimated long/short perpetual open interest

| Direction | Type |
| --- | --- |
| Response | [`MarketListOpenInterestResponse`](./market.go) |

```go
market, err := client.Markets.ListOpenInterest(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(market)
```

## `Explorer`

EVM blocks and activity.

### Get EVM explorer health

| Direction | Type |
| --- | --- |
| Response | [`ExplorerHealth`](./explorer.go) |

```go
explorer, err := client.Explorer.ListHealth(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(explorer)
```

### Get recent EVM blocks

In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListBlocksParams`](./explorer.go) |
| Response | [`ExplorerListBlocksResponse`](./explorer.go) |

```go
explorer, err := client.Explorer.ListBlocks(context.Background(), sdk.ExplorerListBlocksParams{
	Limit: sdk.F[int64](20),
})
if err != nil {
	panic(err)
}

fmt.Println(explorer)
```

### Get recent explorer activity

Explorer activity.

| Direction | Type |
| --- | --- |
| Request | [`ExplorerListTransactionsParams`](./explorer.go) |
| Response | [`ExplorerListTransactionsResponse`](./explorer.go) |

```go
explorer, err := client.Explorer.ListTransactions(context.Background(), sdk.ExplorerListTransactionsParams{
	Limit: sdk.F[int64](50),
})
if err != nil {
	panic(err)
}

fmt.Println(explorer)
```

## `CoinGecko`

USD-only CoinGecko-compatible local-feed formats.

### List local CoinGecko-compatible IDs

List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListParams`](./coingecko.go) |
| Response | [`[]CoinListItem`](./coingecko.go) |

```go
coinGecko, err := client.CoinGecko.List(context.Background(), sdk.CoinGeckoListParams{
	IncludePlatform: sdk.F[bool](false),
})
if err != nil {
	panic(err)
}

fmt.Println(coinGecko)
```

### Get CoinGecko-compatible market data

Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListMarketsParams`](./coingecko.go) |
| Response | [`[]CoinMarket`](./coingecko.go) |

```go
coinGecko, err := client.CoinGecko.ListMarkets(context.Background(), sdk.CoinGeckoListMarketsParams{
	Locale:     sdk.F[sdk.CoinGeckoListMarketsParamsLocale](sdk.CoinGeckoListMarketsParamsLocale("en")),
	Order:      sdk.F[sdk.CoinGeckoListMarketsParamsOrder](sdk.CoinGeckoListMarketsParamsOrder("market_cap_desc")),
	Page:       sdk.F[int64](1),
	PerPage:    sdk.F[int64](100),
	Precision:  sdk.F[string]("full"),
	Sparkline:  sdk.F[bool](false),
	VsCurrency: sdk.F[sdk.CoinGeckoListMarketsParamsVsCurrency](sdk.CoinGeckoListMarketsParamsVsCurrency("usd")),
})
if err != nil {
	panic(err)
}

fmt.Println(coinGecko)
```

### Get CoinGecko-compatible coin detail

Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoGetParams`](./coingecko.go) |
| Response | [`CoinDetail`](./coingecko.go) |

```go
coinGecko, err := client.CoinGecko.Get(context.Background(), "bitcoin", sdk.CoinGeckoGetParams{
	CommunityData: sdk.F[bool](false),
	DeveloperData: sdk.F[bool](false),
	Localization:  sdk.F[bool](false),
	MarketData:    sdk.F[bool](bool(true)),
	Sparkline:     sdk.F[bool](false),
	Tickers:       sdk.F[bool](false),
})
if err != nil {
	panic(err)
}

fmt.Println(coinGecko)
```

### Get compact USD quotes

Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

| Direction | Type |
| --- | --- |
| Request | [`CoinGeckoListSimplePricesParams`](./coingecko.go) |
| Response | [`CoinGeckoListSimplePricesResponse`](./coingecko.go) |

```go
coinGecko, err := client.CoinGecko.ListSimplePrices(context.Background(), sdk.CoinGeckoListSimplePricesParams{
	Include24hrChange:    sdk.F[bool](false),
	Include24hrVol:       sdk.F[bool](false),
	IncludeLastUpdatedAt: sdk.F[bool](false),
	IncludeMarketCap:     sdk.F[bool](false),
	VsCurrencies:         sdk.F[sdk.CoinGeckoListSimplePricesParamsVsCurrencies](sdk.CoinGeckoListSimplePricesParamsVsCurrencies("usd")),
})
if err != nil {
	panic(err)
}

fmt.Println(coinGecko)
```

## `CoinMarketCap`

USD-only CMC-compatible local-feed formats.

### Get CMC-compatible latest quotes

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesParams`](./coinmarketcap.go) |
| Response | [`CoinMarketCapListQuotesResponse`](./coinmarketcap.go) |

```go
coinMarketCap, err := client.CoinMarketCap.ListQuotes(context.Background(), sdk.CoinMarketCapListQuotesParams{
	Convert: sdk.F[sdk.CoinMarketCapListQuotesParamsConvert](sdk.CoinMarketCapListQuotesParamsConvert("USD")),
})
if err != nil {
	panic(err)
}

fmt.Println(coinMarketCap)
```

### Get CMC-compatible latest listings

Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsParams`](./coinmarketcap.go) |
| Response | [`CoinMarketCapListListingsResponse`](./coinmarketcap.go) |

```go
coinMarketCap, err := client.CoinMarketCap.ListListings(context.Background(), sdk.CoinMarketCapListListingsParams{
	Convert: sdk.F[sdk.CoinMarketCapListListingsParamsConvert](sdk.CoinMarketCapListListingsParamsConvert("USD")),
	Limit:   sdk.F[int64](100),
	Sort:    sdk.F[sdk.CoinMarketCapListListingsParamsSort](sdk.CoinMarketCapListListingsParamsSort("market_cap")),
	SortDir: sdk.F[sdk.CoinMarketCapListListingsParamsSortDir](sdk.CoinMarketCapListListingsParamsSortDir("desc")),
	Start:   sdk.F[int64](1),
})
if err != nil {
	panic(err)
}

fmt.Println(coinMarketCap)
```

### Get CMC-compatible latest quotes

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListQuotesAPIAliasParams`](./coinmarketcap.go) |
| Response | [`CoinMarketCapListQuotesAPIAliasResponse`](./coinmarketcap.go) |

```go
coinMarketCap, err := client.CoinMarketCap.ListQuotesAPIAlias(context.Background(), sdk.CoinMarketCapListQuotesAPIAliasParams{
	Convert: sdk.F[sdk.CoinMarketCapListQuotesAPIAliasParamsConvert](sdk.CoinMarketCapListQuotesAPIAliasParamsConvert("USD")),
})
if err != nil {
	panic(err)
}

fmt.Println(coinMarketCap)
```

### Get CMC-compatible latest listings

Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

| Direction | Type |
| --- | --- |
| Request | [`CoinMarketCapListListingsAPIAliasParams`](./coinmarketcap.go) |
| Response | [`CoinMarketCapListListingsAPIAliasResponse`](./coinmarketcap.go) |

```go
coinMarketCap, err := client.CoinMarketCap.ListListingsAPIAlias(context.Background(), sdk.CoinMarketCapListListingsAPIAliasParams{
	Convert: sdk.F[sdk.CoinMarketCapListListingsAPIAliasParamsConvert](sdk.CoinMarketCapListListingsAPIAliasParamsConvert("USD")),
	Limit:   sdk.F[int64](100),
	Sort:    sdk.F[sdk.CoinMarketCapListListingsAPIAliasParamsSort](sdk.CoinMarketCapListListingsAPIAliasParamsSort("market_cap")),
	SortDir: sdk.F[sdk.CoinMarketCapListListingsAPIAliasParamsSortDir](sdk.CoinMarketCapListListingsAPIAliasParamsSortDir("desc")),
	Start:   sdk.F[int64](1),
})
if err != nil {
	panic(err)
}

fmt.Println(coinMarketCap)
```
