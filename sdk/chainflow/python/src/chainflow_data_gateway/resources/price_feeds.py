# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

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
from ..types.price_feed_list_response import PriceFeedListResponse
from ..types.feed_health import FeedHealth
from ..types.stats import Stats
from ..types.perp_stats import PerpStats
from ..types.trade_tape_status import TradeTapeStatus
from ..types.spot_trades import SpotTrades
from ..types import price_feed_retrieve_recent_spot_trades_params, price_feed_retrieve_recent_perpetual_trades_params
from ..types.perp_trades import PerpTrades

__all__ = ["PriceFeedsResource", "AsyncPriceFeedsResource"]


class PriceFeedsResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> PriceFeedsResourceWithRawResponse:
        return PriceFeedsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> PriceFeedsResourceWithStreamingResponse:
        return PriceFeedsResourceWithStreamingResponse(self)

    def list(
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
    ) -> PriceFeedListResponse:
        """
        HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PriceFeedListResponse: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.list(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/price", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=PriceFeedListResponse,
        )

    def list_health(
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
    ) -> FeedHealth:
        """
        Get feed

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            FeedHealth: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.list_health(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/health", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=FeedHealth,
        )

    def list_spot_style_stats(
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
    ) -> Stats:
        """
        Get spot-style market statistics

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            Stats: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.list_spot_style_stats(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/stats", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=Stats,
        )

    def list_perpetual_stats(
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
    ) -> PerpStats:
        """
        Get perpetual-market statistics

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PerpStats: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.list_perpetual_stats(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/perp_stats", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=PerpStats,
        )

    def list_trade_tape_status(
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
    ) -> TradeTapeStatus:
        """
        This path reports tape storage status; use /trades/{symbol} for trade records.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradeTapeStatus: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.list_trade_tape_status(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return self._get(
            path_template("/api/{market}/trades", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=TradeTapeStatus,
        )

    def retrieve_recent_spot_trades(
        self,
        symbol: str,
        *,
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
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> SpotTrades:
        """
        Get recent spot trades

        Args:
            symbol: Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            limit: Most recent trades, newest first.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            SpotTrades: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.retrieve_recent_spot_trades(
                market="btc",
                symbol="symbol",
                limit=100,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return self._get(
            path_template("/api/{market}/trades/{symbol}", **{"market": market, "symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"limit": limit},
                    price_feed_retrieve_recent_spot_trades_params.PriceFeedRetrieveRecentSpotTradesParams,
                ),
            ),
            cast_to=SpotTrades,
        )

    def retrieve_recent_perpetual_trades(
        self,
        symbol: str,
        *,
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
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> PerpTrades:
        """
        Get recent perpetual trades

        Args:
            symbol: Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            limit: Most recent trades, newest first.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PerpTrades: Successful response.

        Example:
            ```python
            price_feed = client.price_feeds.retrieve_recent_perpetual_trades(
                market="btc",
                symbol="symbol",
                limit=100,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return self._get(
            path_template("/api/{market}/perp_trades/{symbol}", **{"market": market, "symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"limit": limit},
                    price_feed_retrieve_recent_perpetual_trades_params.PriceFeedRetrieveRecentPerpetualTradesParams,
                ),
            ),
            cast_to=PerpTrades,
        )


class AsyncPriceFeedsResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncPriceFeedsResourceWithRawResponse:
        return AsyncPriceFeedsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncPriceFeedsResourceWithStreamingResponse:
        return AsyncPriceFeedsResourceWithStreamingResponse(self)

    async def list(
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
    ) -> PriceFeedListResponse:
        """
        HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PriceFeedListResponse: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.list(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/price", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=PriceFeedListResponse,
        )

    async def list_health(
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
    ) -> FeedHealth:
        """
        Get feed

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            FeedHealth: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.list_health(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/health", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=FeedHealth,
        )

    async def list_spot_style_stats(
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
    ) -> Stats:
        """
        Get spot-style market statistics

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            Stats: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.list_spot_style_stats(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/stats", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=Stats,
        )

    async def list_perpetual_stats(
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
    ) -> PerpStats:
        """
        Get perpetual-market statistics

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PerpStats: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.list_perpetual_stats(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/perp_stats", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=PerpStats,
        )

    async def list_trade_tape_status(
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
    ) -> TradeTapeStatus:
        """
        This path reports tape storage status; use /trades/{symbol} for trade records.

        Args:
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            TradeTapeStatus: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.list_trade_tape_status(
                market="btc",
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        return await self._get(
            path_template("/api/{market}/trades", **{"market": market}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=TradeTapeStatus,
        )

    async def retrieve_recent_spot_trades(
        self,
        symbol: str,
        *,
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
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> SpotTrades:
        """
        Get recent spot trades

        Args:
            symbol: Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            limit: Most recent trades, newest first.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            SpotTrades: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.retrieve_recent_spot_trades(
                market="btc",
                symbol="symbol",
                limit=100,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return await self._get(
            path_template("/api/{market}/trades/{symbol}", **{"market": market, "symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"limit": limit},
                    price_feed_retrieve_recent_spot_trades_params.PriceFeedRetrieveRecentSpotTradesParams,
                ),
            ),
            cast_to=SpotTrades,
        )

    async def retrieve_recent_perpetual_trades(
        self,
        symbol: str,
        *,
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
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> PerpTrades:
        """
        Get recent perpetual trades

        Args:
            symbol: Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
            market: Lowercase feed identifier. This route addresses ONE feed, not all markets.
            limit: Most recent trades, newest first.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            PerpTrades: Successful response.

        Example:
            ```python
            price_feed = await client.price_feeds.retrieve_recent_perpetual_trades(
                market="btc",
                symbol="symbol",
                limit=100,
            )
            ```
        """
        if market is None or (isinstance(market, str) and not market):
            raise ValueError(f"Expected a non-empty value for `market` but received {market!r}")
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return await self._get(
            path_template("/api/{market}/perp_trades/{symbol}", **{"market": market, "symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"limit": limit},
                    price_feed_retrieve_recent_perpetual_trades_params.PriceFeedRetrieveRecentPerpetualTradesParams,
                ),
            ),
            cast_to=PerpTrades,
        )


class PriceFeedsResourceWithRawResponse:
    def __init__(self, price_feeds: PriceFeedsResource) -> None:
        self._price_feeds = price_feeds

        self.list = to_raw_response_wrapper(
            price_feeds.list,
        )
        self.list_health = to_raw_response_wrapper(
            price_feeds.list_health,
        )
        self.list_spot_style_stats = to_raw_response_wrapper(
            price_feeds.list_spot_style_stats,
        )
        self.list_perpetual_stats = to_raw_response_wrapper(
            price_feeds.list_perpetual_stats,
        )
        self.list_trade_tape_status = to_raw_response_wrapper(
            price_feeds.list_trade_tape_status,
        )
        self.retrieve_recent_spot_trades = to_raw_response_wrapper(
            price_feeds.retrieve_recent_spot_trades,
        )
        self.retrieve_recent_perpetual_trades = to_raw_response_wrapper(
            price_feeds.retrieve_recent_perpetual_trades,
        )


class AsyncPriceFeedsResourceWithRawResponse:
    def __init__(self, price_feeds: AsyncPriceFeedsResource) -> None:
        self._price_feeds = price_feeds

        self.list = async_to_raw_response_wrapper(
            price_feeds.list,
        )
        self.list_health = async_to_raw_response_wrapper(
            price_feeds.list_health,
        )
        self.list_spot_style_stats = async_to_raw_response_wrapper(
            price_feeds.list_spot_style_stats,
        )
        self.list_perpetual_stats = async_to_raw_response_wrapper(
            price_feeds.list_perpetual_stats,
        )
        self.list_trade_tape_status = async_to_raw_response_wrapper(
            price_feeds.list_trade_tape_status,
        )
        self.retrieve_recent_spot_trades = async_to_raw_response_wrapper(
            price_feeds.retrieve_recent_spot_trades,
        )
        self.retrieve_recent_perpetual_trades = async_to_raw_response_wrapper(
            price_feeds.retrieve_recent_perpetual_trades,
        )


class PriceFeedsResourceWithStreamingResponse:
    def __init__(self, price_feeds: PriceFeedsResource) -> None:
        self._price_feeds = price_feeds

        self.list = to_streamed_response_wrapper(
            price_feeds.list,
        )
        self.list_health = to_streamed_response_wrapper(
            price_feeds.list_health,
        )
        self.list_spot_style_stats = to_streamed_response_wrapper(
            price_feeds.list_spot_style_stats,
        )
        self.list_perpetual_stats = to_streamed_response_wrapper(
            price_feeds.list_perpetual_stats,
        )
        self.list_trade_tape_status = to_streamed_response_wrapper(
            price_feeds.list_trade_tape_status,
        )
        self.retrieve_recent_spot_trades = to_streamed_response_wrapper(
            price_feeds.retrieve_recent_spot_trades,
        )
        self.retrieve_recent_perpetual_trades = to_streamed_response_wrapper(
            price_feeds.retrieve_recent_perpetual_trades,
        )


class AsyncPriceFeedsResourceWithStreamingResponse:
    def __init__(self, price_feeds: AsyncPriceFeedsResource) -> None:
        self._price_feeds = price_feeds

        self.list = async_to_streamed_response_wrapper(
            price_feeds.list,
        )
        self.list_health = async_to_streamed_response_wrapper(
            price_feeds.list_health,
        )
        self.list_spot_style_stats = async_to_streamed_response_wrapper(
            price_feeds.list_spot_style_stats,
        )
        self.list_perpetual_stats = async_to_streamed_response_wrapper(
            price_feeds.list_perpetual_stats,
        )
        self.list_trade_tape_status = async_to_streamed_response_wrapper(
            price_feeds.list_trade_tape_status,
        )
        self.retrieve_recent_spot_trades = async_to_streamed_response_wrapper(
            price_feeds.retrieve_recent_spot_trades,
        )
        self.retrieve_recent_perpetual_trades = async_to_streamed_response_wrapper(
            price_feeds.retrieve_recent_perpetual_trades,
        )
