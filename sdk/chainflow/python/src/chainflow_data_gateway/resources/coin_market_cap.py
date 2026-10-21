# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

from typing import List
from typing_extensions import Literal

from .._types import Body, Omit, Query, Headers, NotGiven, omit, not_given
from .._utils import maybe_transform, async_maybe_transform
from .._compat import cached_property
from .._resource import SyncAPIResource, AsyncAPIResource
from .._response import (
    to_raw_response_wrapper,
    to_streamed_response_wrapper,
    async_to_raw_response_wrapper,
    async_to_streamed_response_wrapper,
)
from .._base_client import make_request_options
from ..types.coin_market_cap_list_quotes_response import CoinMarketCapListQuotesResponse
from ..types.symbol import Symbol
from ..types.coin_id import CoinID
from ..types import (
    coin_market_cap_list_quotes_params,
    coin_market_cap_list_listings_params,
    coin_market_cap_list_quotes_api_alias_params,
    coin_market_cap_list_listings_api_alias_params,
)
from ..types.coin_market_cap_list_listings_response import CoinMarketCapListListingsResponse
from ..types.coin_market_cap_list_quotes_api_alias_response import CoinMarketCapListQuotesAPIAliasResponse
from ..types.coin_market_cap_list_listings_api_alias_response import CoinMarketCapListListingsAPIAliasResponse

__all__ = ["CoinMarketCapResource", "AsyncCoinMarketCapResource"]


class CoinMarketCapResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> CoinMarketCapResourceWithRawResponse:
        return CoinMarketCapResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> CoinMarketCapResourceWithStreamingResponse:
        return CoinMarketCapResourceWithStreamingResponse(self)

    def list_quotes(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        symbol: List[Symbol] | Omit = omit,
        slug: List[CoinID] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListQuotesResponse:
        """
        Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            symbol: Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
            slug: Comma-separated local IDs. Mutually exclusive with symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListQuotesResponse: Successful response.

        Example:
            ```python
            coin_market_cap = client.coin_market_cap.list_quotes(
                convert="USD",
            )
            ```
        """
        return self._get(
            "/v1/cryptocurrency/quotes/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"convert": convert, "symbol": symbol, "slug": slug},
                    coin_market_cap_list_quotes_params.CoinMarketCapListQuotesParams,
                ),
            ),
            cast_to=CoinMarketCapListQuotesResponse,
        )

    def list_listings(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        start: int | Omit = omit,
        limit: int | Omit = omit,
        sort: Literal["market_cap", "price", "name", "symbol", "volume_24h", "percent_change_24h"] | Omit = omit,
        sort_dir: Literal["asc", "desc"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListListingsResponse:
        """
        Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            start: One-based offset.
            limit: Page size; there are 18 local markets.
            sort: Unavailable values sort last.
            sort_dir: Sort direction.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListListingsResponse: Successful response.

        Example:
            ```python
            coin_market_cap = client.coin_market_cap.list_listings(
                convert="USD",
                start=1,
                limit=100,
                sort="market_cap",
                sort_dir="desc",
            )
            ```
        """
        return self._get(
            "/v1/cryptocurrency/listings/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"convert": convert, "start": start, "limit": limit, "sort": sort, "sort_dir": sort_dir},
                    coin_market_cap_list_listings_params.CoinMarketCapListListingsParams,
                ),
            ),
            cast_to=CoinMarketCapListListingsResponse,
        )

    def list_quotes_api_alias(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        symbol: List[Symbol] | Omit = omit,
        slug: List[CoinID] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListQuotesAPIAliasResponse:
        """
        Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            symbol: Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
            slug: Comma-separated local IDs. Mutually exclusive with symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListQuotesAPIAliasResponse: Successful response.

        Example:
            ```python
            coin_market_cap = client.coin_market_cap.list_quotes_api_alias(
                convert="USD",
            )
            ```
        """
        return self._get(
            "/api/v1/cryptocurrency/quotes/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"convert": convert, "symbol": symbol, "slug": slug},
                    coin_market_cap_list_quotes_api_alias_params.CoinMarketCapListQuotesAPIAliasParams,
                ),
            ),
            cast_to=CoinMarketCapListQuotesAPIAliasResponse,
        )

    def list_listings_api_alias(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        start: int | Omit = omit,
        limit: int | Omit = omit,
        sort: Literal["market_cap", "price", "name", "symbol", "volume_24h", "percent_change_24h"] | Omit = omit,
        sort_dir: Literal["asc", "desc"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListListingsAPIAliasResponse:
        """
        Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            start: One-based offset.
            limit: Page size; there are 18 local markets.
            sort: Unavailable values sort last.
            sort_dir: Sort direction.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListListingsAPIAliasResponse: Successful response.

        Example:
            ```python
            coin_market_cap = client.coin_market_cap.list_listings_api_alias(
                convert="USD",
                start=1,
                limit=100,
                sort="market_cap",
                sort_dir="desc",
            )
            ```
        """
        return self._get(
            "/api/v1/cryptocurrency/listings/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"convert": convert, "start": start, "limit": limit, "sort": sort, "sort_dir": sort_dir},
                    coin_market_cap_list_listings_api_alias_params.CoinMarketCapListListingsAPIAliasParams,
                ),
            ),
            cast_to=CoinMarketCapListListingsAPIAliasResponse,
        )


class AsyncCoinMarketCapResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncCoinMarketCapResourceWithRawResponse:
        return AsyncCoinMarketCapResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncCoinMarketCapResourceWithStreamingResponse:
        return AsyncCoinMarketCapResourceWithStreamingResponse(self)

    async def list_quotes(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        symbol: List[Symbol] | Omit = omit,
        slug: List[CoinID] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListQuotesResponse:
        """
        Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            symbol: Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
            slug: Comma-separated local IDs. Mutually exclusive with symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListQuotesResponse: Successful response.

        Example:
            ```python
            coin_market_cap = await client.coin_market_cap.list_quotes(
                convert="USD",
            )
            ```
        """
        return await self._get(
            "/v1/cryptocurrency/quotes/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"convert": convert, "symbol": symbol, "slug": slug},
                    coin_market_cap_list_quotes_params.CoinMarketCapListQuotesParams,
                ),
            ),
            cast_to=CoinMarketCapListQuotesResponse,
        )

    async def list_listings(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        start: int | Omit = omit,
        limit: int | Omit = omit,
        sort: Literal["market_cap", "price", "name", "symbol", "volume_24h", "percent_change_24h"] | Omit = omit,
        sort_dir: Literal["asc", "desc"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListListingsResponse:
        """
        Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            start: One-based offset.
            limit: Page size; there are 18 local markets.
            sort: Unavailable values sort last.
            sort_dir: Sort direction.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListListingsResponse: Successful response.

        Example:
            ```python
            coin_market_cap = await client.coin_market_cap.list_listings(
                convert="USD",
                start=1,
                limit=100,
                sort="market_cap",
                sort_dir="desc",
            )
            ```
        """
        return await self._get(
            "/v1/cryptocurrency/listings/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"convert": convert, "start": start, "limit": limit, "sort": sort, "sort_dir": sort_dir},
                    coin_market_cap_list_listings_params.CoinMarketCapListListingsParams,
                ),
            ),
            cast_to=CoinMarketCapListListingsResponse,
        )

    async def list_quotes_api_alias(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        symbol: List[Symbol] | Omit = omit,
        slug: List[CoinID] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListQuotesAPIAliasResponse:
        """
        Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            symbol: Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
            slug: Comma-separated local IDs. Mutually exclusive with symbol.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListQuotesAPIAliasResponse: Successful response.

        Example:
            ```python
            coin_market_cap = await client.coin_market_cap.list_quotes_api_alias(
                convert="USD",
            )
            ```
        """
        return await self._get(
            "/api/v1/cryptocurrency/quotes/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"convert": convert, "symbol": symbol, "slug": slug},
                    coin_market_cap_list_quotes_api_alias_params.CoinMarketCapListQuotesAPIAliasParams,
                ),
            ),
            cast_to=CoinMarketCapListQuotesAPIAliasResponse,
        )

    async def list_listings_api_alias(
        self,
        *,
        convert: Literal["USD", "usd"] | Omit = omit,
        start: int | Omit = omit,
        limit: int | Omit = omit,
        sort: Literal["market_cap", "price", "name", "symbol", "volume_24h", "percent_change_24h"] | Omit = omit,
        sort_dir: Literal["asc", "desc"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinMarketCapListListingsAPIAliasResponse:
        """
        Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.

        Args:
            convert: Only USD is supported.
            start: One-based offset.
            limit: Page size; there are 18 local markets.
            sort: Unavailable values sort last.
            sort_dir: Sort direction.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinMarketCapListListingsAPIAliasResponse: Successful response.

        Example:
            ```python
            coin_market_cap = await client.coin_market_cap.list_listings_api_alias(
                convert="USD",
                start=1,
                limit=100,
                sort="market_cap",
                sort_dir="desc",
            )
            ```
        """
        return await self._get(
            "/api/v1/cryptocurrency/listings/latest",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"convert": convert, "start": start, "limit": limit, "sort": sort, "sort_dir": sort_dir},
                    coin_market_cap_list_listings_api_alias_params.CoinMarketCapListListingsAPIAliasParams,
                ),
            ),
            cast_to=CoinMarketCapListListingsAPIAliasResponse,
        )


class CoinMarketCapResourceWithRawResponse:
    def __init__(self, coin_market_cap: CoinMarketCapResource) -> None:
        self._coin_market_cap = coin_market_cap

        self.list_quotes = to_raw_response_wrapper(
            coin_market_cap.list_quotes,
        )
        self.list_listings = to_raw_response_wrapper(
            coin_market_cap.list_listings,
        )
        self.list_quotes_api_alias = to_raw_response_wrapper(
            coin_market_cap.list_quotes_api_alias,
        )
        self.list_listings_api_alias = to_raw_response_wrapper(
            coin_market_cap.list_listings_api_alias,
        )


class AsyncCoinMarketCapResourceWithRawResponse:
    def __init__(self, coin_market_cap: AsyncCoinMarketCapResource) -> None:
        self._coin_market_cap = coin_market_cap

        self.list_quotes = async_to_raw_response_wrapper(
            coin_market_cap.list_quotes,
        )
        self.list_listings = async_to_raw_response_wrapper(
            coin_market_cap.list_listings,
        )
        self.list_quotes_api_alias = async_to_raw_response_wrapper(
            coin_market_cap.list_quotes_api_alias,
        )
        self.list_listings_api_alias = async_to_raw_response_wrapper(
            coin_market_cap.list_listings_api_alias,
        )


class CoinMarketCapResourceWithStreamingResponse:
    def __init__(self, coin_market_cap: CoinMarketCapResource) -> None:
        self._coin_market_cap = coin_market_cap

        self.list_quotes = to_streamed_response_wrapper(
            coin_market_cap.list_quotes,
        )
        self.list_listings = to_streamed_response_wrapper(
            coin_market_cap.list_listings,
        )
        self.list_quotes_api_alias = to_streamed_response_wrapper(
            coin_market_cap.list_quotes_api_alias,
        )
        self.list_listings_api_alias = to_streamed_response_wrapper(
            coin_market_cap.list_listings_api_alias,
        )


class AsyncCoinMarketCapResourceWithStreamingResponse:
    def __init__(self, coin_market_cap: AsyncCoinMarketCapResource) -> None:
        self._coin_market_cap = coin_market_cap

        self.list_quotes = async_to_streamed_response_wrapper(
            coin_market_cap.list_quotes,
        )
        self.list_listings = async_to_streamed_response_wrapper(
            coin_market_cap.list_listings,
        )
        self.list_quotes_api_alias = async_to_streamed_response_wrapper(
            coin_market_cap.list_quotes_api_alias,
        )
        self.list_listings_api_alias = async_to_streamed_response_wrapper(
            coin_market_cap.list_listings_api_alias,
        )
