# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

from typing import List
from typing_extensions import Literal

from .._types import Body, Omit, Query, Headers, NotGiven, omit, not_given
from .._utils import path_template, maybe_transform, async_maybe_transform
from .._compat import cached_property
from .._resource import SyncAPIResource, AsyncAPIResource
from .._response import (
    to_raw_response_wrapper,
    to_streamed_response_wrapper,
    async_to_raw_response_wrapper,
    async_to_streamed_response_wrapper,
)
from .._base_client import make_request_options
from ..types.udf_config import UdfConfig
from ..types.trading_view_resolve_symbol_response import TradingViewResolveSymbolResponse
from ..types.symbol import Symbol
from ..types import (
    trading_view_resolve_symbol_params,
    trading_view_search_params,
    trading_view_list_candle_history_params,
    trading_view_list_quotes_params,
)
from ..types.trading_view_search_response import TradingViewSearchResponse
from ..types.trading_view_list_candle_history_response import TradingViewListCandleHistoryResponse
from ..types.trading_view_list_quotes_response import TradingViewListQuotesResponse

__all__ = ["TradingViewResource", "AsyncTradingViewResource"]


class TradingViewResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> TradingViewResourceWithRawResponse:
        return TradingViewResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> TradingViewResourceWithStreamingResponse:
        return TradingViewResourceWithStreamingResponse(self)

    def list_datafeed_config(
        self,
        market: Literal[
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
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> UdfConfig:
        """
        Get UDF feed capabilities

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            UdfConfig: Successful response.

        Example:
            ```python
            trading_view = client.trading_view.list_datafeed_config(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/config", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=UdfConfig,
        )

    def list_feed_time(
        self,
        market: Literal[
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
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> str:
        """
        An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            str: Unix epoch seconds encoded as decimal text.

        Example:
            ```python
            trading_view = client.trading_view.list_feed_time(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        extra_headers = {"Accept": "text/html", **(extra_headers or {})}
        return self._get(
            path_template("/api/{market}/time", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=str,
        )

    def resolve_symbol(
        self,
        market: Literal[
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
        ],
        *,
        symbol: Symbol,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewResolveSymbolResponse:
        """
        Unknown or mismatched symbol returns an application error with HTTP 200.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbol: Uppercase symbol matching the selected market.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewResolveSymbolResponse: Successful response.

        Example:
            ```python
            trading_view = client.trading_view.resolve_symbol(
                market="btc",
                symbol="BTC",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/symbols", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"symbol": symbol}, trading_view_resolve_symbol_params.TradingViewResolveSymbolParams
                ),
            ),
            cast_to=TradingViewResolveSymbolResponse,
        )

    def search(
        self,
        market: Literal[
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
        ],
        *,
        query: str | Omit = omit,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewSearchResponse:
        """
        This is not cross-market search. The endpoint ignores UDF exchange/type filters.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            query: Case-insensitive substring of symbol or full description.
            limit: Maximum results; one feed exposes at most one symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewSearchResponse: Successful response.

        Example:
            ```python
            trading_view = client.trading_view.search(
                market="btc",
                query="",
                limit=30,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/search", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"query": query, "limit": limit}, trading_view_search_params.TradingViewSearchParams
                ),
            ),
            cast_to=TradingViewSearchResponse,
        )

    def list_candle_history(
        self,
        market: Literal[
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
        ],
        *,
        symbol: Symbol,
        resolution: Literal["1", "2", "3", "5", "15", "30", "60", "240", "480", "D", "W", "1D", "1W"],
        from_: int,
        to: int,
        countback: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewListCandleHistoryResponse:
        """
        Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbol: Uppercase symbol matching the selected market.
            resolution: UDF resolution: numeric strings are minutes; D/1D and W/1W are aliases.
            from_: Inclusive lower bound in Unix seconds.
            to: Inclusive upper bound in Unix seconds; must be >= from.
            countback: Row limit within from/to. Values above 5000 are clamped to 5000, not rejected. Live-candle merging can add one extra row.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewListCandleHistoryResponse: Successful response.

        Example:
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
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/history", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"symbol": symbol, "resolution": resolution, "from_": from_, "to": to, "countback": countback},
                    trading_view_list_candle_history_params.TradingViewListCandleHistoryParams,
                ),
            ),
            cast_to=TradingViewListCandleHistoryResponse,
        )

    def list_quotes(
        self,
        market: Literal[
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
        ],
        *,
        symbols: List[Symbol] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewListQuotesResponse:
        """
        Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbols: Comma-separated symbols. Only the selected feed's symbol is returned; other symbols are silently omitted.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewListQuotesResponse: Successful response.

        Example:
            ```python
            trading_view = client.trading_view.list_quotes(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/quotes", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"symbols": symbols}, trading_view_list_quotes_params.TradingViewListQuotesParams
                ),
            ),
            cast_to=TradingViewListQuotesResponse,
        )


class AsyncTradingViewResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncTradingViewResourceWithRawResponse:
        return AsyncTradingViewResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncTradingViewResourceWithStreamingResponse:
        return AsyncTradingViewResourceWithStreamingResponse(self)

    async def list_datafeed_config(
        self,
        market: Literal[
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
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> UdfConfig:
        """
        Get UDF feed capabilities

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            UdfConfig: Successful response.

        Example:
            ```python
            trading_view = await client.trading_view.list_datafeed_config(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/config", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=UdfConfig,
        )

    async def list_feed_time(
        self,
        market: Literal[
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
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> str:
        """
        An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            str: Unix epoch seconds encoded as decimal text.

        Example:
            ```python
            trading_view = await client.trading_view.list_feed_time(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        extra_headers = {"Accept": "text/html", **(extra_headers or {})}
        return await self._get(
            path_template("/api/{market}/time", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=str,
        )

    async def resolve_symbol(
        self,
        market: Literal[
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
        ],
        *,
        symbol: Symbol,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewResolveSymbolResponse:
        """
        Unknown or mismatched symbol returns an application error with HTTP 200.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbol: Uppercase symbol matching the selected market.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewResolveSymbolResponse: Successful response.

        Example:
            ```python
            trading_view = await client.trading_view.resolve_symbol(
                market="btc",
                symbol="BTC",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/symbols", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"symbol": symbol}, trading_view_resolve_symbol_params.TradingViewResolveSymbolParams
                ),
            ),
            cast_to=TradingViewResolveSymbolResponse,
        )

    async def search(
        self,
        market: Literal[
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
        ],
        *,
        query: str | Omit = omit,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewSearchResponse:
        """
        This is not cross-market search. The endpoint ignores UDF exchange/type filters.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            query: Case-insensitive substring of symbol or full description.
            limit: Maximum results; one feed exposes at most one symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewSearchResponse: Successful response.

        Example:
            ```python
            trading_view = await client.trading_view.search(
                market="btc",
                query="",
                limit=30,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/search", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"query": query, "limit": limit}, trading_view_search_params.TradingViewSearchParams
                ),
            ),
            cast_to=TradingViewSearchResponse,
        )

    async def list_candle_history(
        self,
        market: Literal[
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
        ],
        *,
        symbol: Symbol,
        resolution: Literal["1", "2", "3", "5", "15", "30", "60", "240", "480", "D", "W", "1D", "1W"],
        from_: int,
        to: int,
        countback: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewListCandleHistoryResponse:
        """
        Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbol: Uppercase symbol matching the selected market.
            resolution: UDF resolution: numeric strings are minutes; D/1D and W/1W are aliases.
            from_: Inclusive lower bound in Unix seconds.
            to: Inclusive upper bound in Unix seconds; must be >= from.
            countback: Row limit within from/to. Values above 5000 are clamped to 5000, not rejected. Live-candle merging can add one extra row.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewListCandleHistoryResponse: Successful response.

        Example:
            ```python
            trading_view = await client.trading_view.list_candle_history(
                market="btc",
                symbol="BTC",
                resolution="1",
                from_=1,
                to=1,
                countback=1000,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/history", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"symbol": symbol, "resolution": resolution, "from_": from_, "to": to, "countback": countback},
                    trading_view_list_candle_history_params.TradingViewListCandleHistoryParams,
                ),
            ),
            cast_to=TradingViewListCandleHistoryResponse,
        )

    async def list_quotes(
        self,
        market: Literal[
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
        ],
        *,
        symbols: List[Symbol] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> TradingViewListQuotesResponse:
        """
        Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            symbols: Comma-separated symbols. Only the selected feed's symbol is returned; other symbols are silently omitted.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradingViewListQuotesResponse: Successful response.

        Example:
            ```python
            trading_view = await client.trading_view.list_quotes(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/quotes", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"symbols": symbols}, trading_view_list_quotes_params.TradingViewListQuotesParams
                ),
            ),
            cast_to=TradingViewListQuotesResponse,
        )


class TradingViewResourceWithRawResponse:
    def __init__(self, trading_view: TradingViewResource) -> None:
        self._trading_view = trading_view

        self.list_datafeed_config = to_raw_response_wrapper(
            trading_view.list_datafeed_config,
        )
        self.list_feed_time = to_raw_response_wrapper(
            trading_view.list_feed_time,
        )
        self.resolve_symbol = to_raw_response_wrapper(
            trading_view.resolve_symbol,
        )
        self.search = to_raw_response_wrapper(
            trading_view.search,
        )
        self.list_candle_history = to_raw_response_wrapper(
            trading_view.list_candle_history,
        )
        self.list_quotes = to_raw_response_wrapper(
            trading_view.list_quotes,
        )


class AsyncTradingViewResourceWithRawResponse:
    def __init__(self, trading_view: AsyncTradingViewResource) -> None:
        self._trading_view = trading_view

        self.list_datafeed_config = async_to_raw_response_wrapper(
            trading_view.list_datafeed_config,
        )
        self.list_feed_time = async_to_raw_response_wrapper(
            trading_view.list_feed_time,
        )
        self.resolve_symbol = async_to_raw_response_wrapper(
            trading_view.resolve_symbol,
        )
        self.search = async_to_raw_response_wrapper(
            trading_view.search,
        )
        self.list_candle_history = async_to_raw_response_wrapper(
            trading_view.list_candle_history,
        )
        self.list_quotes = async_to_raw_response_wrapper(
            trading_view.list_quotes,
        )


class TradingViewResourceWithStreamingResponse:
    def __init__(self, trading_view: TradingViewResource) -> None:
        self._trading_view = trading_view

        self.list_datafeed_config = to_streamed_response_wrapper(
            trading_view.list_datafeed_config,
        )
        self.list_feed_time = to_streamed_response_wrapper(
            trading_view.list_feed_time,
        )
        self.resolve_symbol = to_streamed_response_wrapper(
            trading_view.resolve_symbol,
        )
        self.search = to_streamed_response_wrapper(
            trading_view.search,
        )
        self.list_candle_history = to_streamed_response_wrapper(
            trading_view.list_candle_history,
        )
        self.list_quotes = to_streamed_response_wrapper(
            trading_view.list_quotes,
        )


class AsyncTradingViewResourceWithStreamingResponse:
    def __init__(self, trading_view: AsyncTradingViewResource) -> None:
        self._trading_view = trading_view

        self.list_datafeed_config = async_to_streamed_response_wrapper(
            trading_view.list_datafeed_config,
        )
        self.list_feed_time = async_to_streamed_response_wrapper(
            trading_view.list_feed_time,
        )
        self.resolve_symbol = async_to_streamed_response_wrapper(
            trading_view.resolve_symbol,
        )
        self.search = async_to_streamed_response_wrapper(
            trading_view.search,
        )
        self.list_candle_history = async_to_streamed_response_wrapper(
            trading_view.list_candle_history,
        )
        self.list_quotes = async_to_streamed_response_wrapper(
            trading_view.list_quotes,
        )
