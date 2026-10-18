# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

from typing_extensions import Literal

from .._types import Body, Query, Headers, NotGiven, not_given
from .._utils import path_template
from .._compat import cached_property
from .._resource import SyncAPIResource, AsyncAPIResource
from .._response import (
    to_raw_response_wrapper,
    to_streamed_response_wrapper,
    async_to_raw_response_wrapper,
    async_to_streamed_response_wrapper,
)
from .._base_client import make_request_options
from ..types.markets_health import MarketsHealth
from ..types.market_list_response import MarketListResponse
from ..types.market_retrieve_response import MarketRetrieveResponse
from ..types.market_list_totals_response import MarketListTotalsResponse
from ..types.market_list_tickers_response import MarketListTickersResponse
from ..types.market_retrieve_ticker_response import MarketRetrieveTickerResponse
from ..types.market_list_open_interest_response import MarketListOpenInterestResponse

__all__ = ["MarketsResource", "AsyncMarketsResource"]


class MarketsResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> MarketsResourceWithRawResponse:
        return MarketsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> MarketsResourceWithStreamingResponse:
        return MarketsResourceWithStreamingResponse(self)

    def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketsHealth:
        """
        Get aggregator health

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketsHealth: Successful response.

        Example:
            ```python
            market = client.markets.list_health()
            ```
        """
        return self._get(
            "/api/markets/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketsHealth,
        )

    def list(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListResponse:
        """
        List display-scaled market summaries

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListResponse: Successful response.

        Example:
            ```python
            market = client.markets.list()
            ```
        """
        return self._get(
            "/api/markets/markets",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListResponse,
        )

    def retrieve(
        self,
        symbol: Literal[
            "BTC",
            "ETH",
            "SOL",
            "AVAX",
            "LINK",
            "HYPE",
            "XRP",
            "ASTER",
            "TRUMP",
            "BNB",
            "SID",
            "PAX",
            "TSLA",
            "NVDA",
            "GOOGL",
            "XAU",
            "NAS100",
            "SPX500",
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketRetrieveResponse:
        """
        Get one display-scaled market summary

        Args:
            symbol: Uppercase market symbol; gateway path matching is case-sensitive.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketRetrieveResponse: Successful response.

        Example:
            ```python
            market = client.markets.retrieve(
                symbol="BTC",
            )
            ```
        """
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return self._get(
            path_template("/api/markets/markets/{symbol}", **{"symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketRetrieveResponse,
        )

    def list_totals(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListTotalsResponse:
        """
        Get display-scaled aggregate totals

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListTotalsResponse: Successful response.

        Example:
            ```python
            market = client.markets.list_totals()
            ```
        """
        return self._get(
            "/api/markets/totals",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListTotalsResponse,
        )

    def list_tickers(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListTickersResponse:
        """
        Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListTickersResponse: Successful response.

        Example:
            ```python
            market = client.markets.list_tickers()
            ```
        """
        return self._get(
            "/api/markets/tickers",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListTickersResponse,
        )

    def retrieve_ticker(
        self,
        symbol: str,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketRetrieveTickerResponse:
        """
        Unknown, missing or stale prices return HTTP 503, not 404.

        Args:
            symbol: Bare uppercase symbol (BTC) or wrapped symbol (PERP_BTC_USDC).
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketRetrieveTickerResponse: Successful response.

        Example:
            ```python
            market = client.markets.retrieve_ticker(
                symbol="symbol",
            )
            ```
        """
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return self._get(
            path_template("/api/markets/ticker/{symbol}", **{"symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketRetrieveTickerResponse,
        )

    def list_open_interest(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListOpenInterestResponse:
        """
        Get estimated long/short perpetual open interest

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListOpenInterestResponse: Successful response.

        Example:
            ```python
            market = client.markets.list_open_interest()
            ```
        """
        return self._get(
            "/api/markets/open-interest",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListOpenInterestResponse,
        )


class AsyncMarketsResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncMarketsResourceWithRawResponse:
        return AsyncMarketsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncMarketsResourceWithStreamingResponse:
        return AsyncMarketsResourceWithStreamingResponse(self)

    async def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketsHealth:
        """
        Get aggregator health

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketsHealth: Successful response.

        Example:
            ```python
            market = await client.markets.list_health()
            ```
        """
        return await self._get(
            "/api/markets/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketsHealth,
        )

    async def list(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListResponse:
        """
        List display-scaled market summaries

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListResponse: Successful response.

        Example:
            ```python
            market = await client.markets.list()
            ```
        """
        return await self._get(
            "/api/markets/markets",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListResponse,
        )

    async def retrieve(
        self,
        symbol: Literal[
            "BTC",
            "ETH",
            "SOL",
            "AVAX",
            "LINK",
            "HYPE",
            "XRP",
            "ASTER",
            "TRUMP",
            "BNB",
            "SID",
            "PAX",
            "TSLA",
            "NVDA",
            "GOOGL",
            "XAU",
            "NAS100",
            "SPX500",
        ],
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketRetrieveResponse:
        """
        Get one display-scaled market summary

        Args:
            symbol: Uppercase market symbol; gateway path matching is case-sensitive.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketRetrieveResponse: Successful response.

        Example:
            ```python
            market = await client.markets.retrieve(
                symbol="BTC",
            )
            ```
        """
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return await self._get(
            path_template("/api/markets/markets/{symbol}", **{"symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketRetrieveResponse,
        )

    async def list_totals(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListTotalsResponse:
        """
        Get display-scaled aggregate totals

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListTotalsResponse: Successful response.

        Example:
            ```python
            market = await client.markets.list_totals()
            ```
        """
        return await self._get(
            "/api/markets/totals",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListTotalsResponse,
        )

    async def list_tickers(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListTickersResponse:
        """
        Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListTickersResponse: Successful response.

        Example:
            ```python
            market = await client.markets.list_tickers()
            ```
        """
        return await self._get(
            "/api/markets/tickers",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListTickersResponse,
        )

    async def retrieve_ticker(
        self,
        symbol: str,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketRetrieveTickerResponse:
        """
        Unknown, missing or stale prices return HTTP 503, not 404.

        Args:
            symbol: Bare uppercase symbol (BTC) or wrapped symbol (PERP_BTC_USDC).
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketRetrieveTickerResponse: Successful response.

        Example:
            ```python
            market = await client.markets.retrieve_ticker(
                symbol="symbol",
            )
            ```
        """
        if symbol is None or (isinstance(symbol, str) and not symbol):
            raise ValueError(f"Expected a non-empty value for `symbol` but received {symbol!r}")
        return await self._get(
            path_template("/api/markets/ticker/{symbol}", **{"symbol": symbol}),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketRetrieveTickerResponse,
        )

    async def list_open_interest(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> MarketListOpenInterestResponse:
        """
        Get estimated long/short perpetual open interest

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            MarketListOpenInterestResponse: Successful response.

        Example:
            ```python
            market = await client.markets.list_open_interest()
            ```
        """
        return await self._get(
            "/api/markets/open-interest",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=MarketListOpenInterestResponse,
        )


class MarketsResourceWithRawResponse:
    def __init__(self, markets: MarketsResource) -> None:
        self._markets = markets

        self.list_health = to_raw_response_wrapper(
            markets.list_health,
        )
        self.list = to_raw_response_wrapper(
            markets.list,
        )
        self.retrieve = to_raw_response_wrapper(
            markets.retrieve,
        )
        self.list_totals = to_raw_response_wrapper(
            markets.list_totals,
        )
        self.list_tickers = to_raw_response_wrapper(
            markets.list_tickers,
        )
        self.retrieve_ticker = to_raw_response_wrapper(
            markets.retrieve_ticker,
        )
        self.list_open_interest = to_raw_response_wrapper(
            markets.list_open_interest,
        )


class AsyncMarketsResourceWithRawResponse:
    def __init__(self, markets: AsyncMarketsResource) -> None:
        self._markets = markets

        self.list_health = async_to_raw_response_wrapper(
            markets.list_health,
        )
        self.list = async_to_raw_response_wrapper(
            markets.list,
        )
        self.retrieve = async_to_raw_response_wrapper(
            markets.retrieve,
        )
        self.list_totals = async_to_raw_response_wrapper(
            markets.list_totals,
        )
        self.list_tickers = async_to_raw_response_wrapper(
            markets.list_tickers,
        )
        self.retrieve_ticker = async_to_raw_response_wrapper(
            markets.retrieve_ticker,
        )
        self.list_open_interest = async_to_raw_response_wrapper(
            markets.list_open_interest,
        )


class MarketsResourceWithStreamingResponse:
    def __init__(self, markets: MarketsResource) -> None:
        self._markets = markets

        self.list_health = to_streamed_response_wrapper(
            markets.list_health,
        )
        self.list = to_streamed_response_wrapper(
            markets.list,
        )
        self.retrieve = to_streamed_response_wrapper(
            markets.retrieve,
        )
        self.list_totals = to_streamed_response_wrapper(
            markets.list_totals,
        )
        self.list_tickers = to_streamed_response_wrapper(
            markets.list_tickers,
        )
        self.retrieve_ticker = to_streamed_response_wrapper(
            markets.retrieve_ticker,
        )
        self.list_open_interest = to_streamed_response_wrapper(
            markets.list_open_interest,
        )


class AsyncMarketsResourceWithStreamingResponse:
    def __init__(self, markets: AsyncMarketsResource) -> None:
        self._markets = markets

        self.list_health = async_to_streamed_response_wrapper(
            markets.list_health,
        )
        self.list = async_to_streamed_response_wrapper(
            markets.list,
        )
        self.retrieve = async_to_streamed_response_wrapper(
            markets.retrieve,
        )
        self.list_totals = async_to_streamed_response_wrapper(
            markets.list_totals,
        )
        self.list_tickers = async_to_streamed_response_wrapper(
            markets.list_tickers,
        )
        self.retrieve_ticker = async_to_streamed_response_wrapper(
            markets.retrieve_ticker,
        )
        self.list_open_interest = async_to_streamed_response_wrapper(
            markets.list_open_interest,
        )
