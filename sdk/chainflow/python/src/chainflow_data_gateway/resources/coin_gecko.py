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
from ..types.coin_gecko_list_response import CoinGeckoListResponse
from ..types import (
    coin_gecko_list_params,
    coin_gecko_list_markets_params,
    coin_gecko_retrieve_params,
    coin_gecko_list_simple_prices_params,
)
from ..types.coin_gecko_list_markets_response import CoinGeckoListMarketsResponse
from ..types.coin_id import CoinID
from ..types.market import Market
from ..types.coin_detail import CoinDetail
from ..types.coin_gecko_list_simple_prices_response import CoinGeckoListSimplePricesResponse

__all__ = ["CoinGeckoResource", "AsyncCoinGeckoResource"]


class CoinGeckoResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> CoinGeckoResourceWithRawResponse:
        return CoinGeckoResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> CoinGeckoResourceWithStreamingResponse:
        return CoinGeckoResourceWithStreamingResponse(self)

    def list(
        self,
        *,
        include_platform: bool | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListResponse:
        """
        List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            include_platform: Only false is supported; true returns 400.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListResponse: Successful response.

        Example:
            ```python
            coin_gecko = client.coin_gecko.list(
                include_platform=False,
            )
            ```
        """
        return self._get(
            "/api/v3/coins/list",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"include_platform": include_platform}, coin_gecko_list_params.CoinGeckoListParams
                ),
            ),
            cast_to=CoinGeckoListResponse,
        )

    def list_markets(
        self,
        *,
        vs_currency: Literal["usd", "USD"] | Omit = omit,
        ids: List[CoinID] | Omit = omit,
        symbols: List[Market] | Omit = omit,
        page: int | Omit = omit,
        per_page: int | Omit = omit,
        order: Literal["market_cap_asc", "market_cap_desc", "volume_asc", "volume_desc", "id_asc", "id_desc"]
        | Omit = omit,
        sparkline: bool | Omit = omit,
        price_change_percentage: List[Literal["1h", "24h"]] | Omit = omit,
        precision: str | Omit = omit,
        locale: Literal["en"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListMarketsResponse:
        """
        Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            vs_currency: Only USD is supported.
            ids: Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
            symbols: Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
            page: One-based page.
            per_page: Page size.
            order: Unavailable values sort last; ID breaks ties.
            sparkline: Only false is supported; true returns 400.
            price_change_percentage: Optional extra percentage-change fields.
            precision: full or decimal count 0..18.
            locale: Only English metadata is supported.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListMarketsResponse: Successful response.

        Example:
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
        """
        return self._get(
            "/api/v3/coins/markets",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {
                        "vs_currency": vs_currency,
                        "ids": ids,
                        "symbols": symbols,
                        "page": page,
                        "per_page": per_page,
                        "order": order,
                        "sparkline": sparkline,
                        "price_change_percentage": price_change_percentage,
                        "precision": precision,
                        "locale": locale,
                    },
                    coin_gecko_list_markets_params.CoinGeckoListMarketsParams,
                ),
            ),
            cast_to=CoinGeckoListMarketsResponse,
        )

    def retrieve(
        self,
        id: Literal[
            "bitcoin",
            "ethereum",
            "solana",
            "avalanche-2",
            "chainlink",
            "hyperliquid",
            "ripple",
            "aster",
            "official-trump",
            "binancecoin",
            "sidiora",
            "paxeer",
            "tesla",
            "nvidia",
            "alphabet",
            "gold",
            "nasdaq-100",
            "sp-500",
        ],
        *,
        localization: bool | Omit = omit,
        tickers: bool | Omit = omit,
        community_data: bool | Omit = omit,
        developer_data: bool | Omit = omit,
        sparkline: bool | Omit = omit,
        market_data: Literal[True] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinDetail:
        """
        Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            id: Local adapter identifier; not an official provider registry ID.
            localization: Only false is supported; true returns 400.
            tickers: Only false is supported; true returns 400.
            community_data: Only false is supported; true returns 400.
            developer_data: Only false is supported; true returns 400.
            sparkline: Only false is supported; true returns 400.
            market_data: Must be true.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinDetail: Successful response.

        Example:
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
        """
        if id is None or (isinstance(id, str) and not id):
            raise ValueError(f"Expected a non-empty value for `id` but received {id!r}")
        return self._get(
            path_template("/api/v3/coins/{id}", **{"id": id}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {
                        "localization": localization,
                        "tickers": tickers,
                        "community_data": community_data,
                        "developer_data": developer_data,
                        "sparkline": sparkline,
                        "market_data": market_data,
                    },
                    coin_gecko_retrieve_params.CoinGeckoRetrieveParams,
                ),
            ),
            cast_to=CoinDetail,
        )

    def list_simple_prices(
        self,
        *,
        ids: List[CoinID] | Omit = omit,
        symbols: List[Market] | Omit = omit,
        vs_currencies: Literal["usd", "USD"] | Omit = omit,
        include_market_cap: bool | Omit = omit,
        include_24hr_vol: bool | Omit = omit,
        include_24hr_change: bool | Omit = omit,
        include_last_updated_at: bool | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListSimplePricesResponse:
        """
        Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            ids: Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
            symbols: Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
            vs_currencies: Only one currency, USD.
            include_market_cap: Include the corresponding optional response field.
            include_24hr_vol: Include the corresponding optional response field.
            include_24hr_change: Include the corresponding optional response field.
            include_last_updated_at: Include the corresponding optional response field.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListSimplePricesResponse: Successful response.

        Example:
            ```python
            coin_gecko = client.coin_gecko.list_simple_prices(
                vs_currencies="usd",
                include_market_cap=False,
                include_24hr_vol=False,
                include_24hr_change=False,
                include_last_updated_at=False,
            )
            ```
        """
        return self._get(
            "/api/v3/simple/price",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {
                        "ids": ids,
                        "symbols": symbols,
                        "vs_currencies": vs_currencies,
                        "include_market_cap": include_market_cap,
                        "include_24hr_vol": include_24hr_vol,
                        "include_24hr_change": include_24hr_change,
                        "include_last_updated_at": include_last_updated_at,
                    },
                    coin_gecko_list_simple_prices_params.CoinGeckoListSimplePricesParams,
                ),
            ),
            cast_to=CoinGeckoListSimplePricesResponse,
        )


class AsyncCoinGeckoResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncCoinGeckoResourceWithRawResponse:
        return AsyncCoinGeckoResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncCoinGeckoResourceWithStreamingResponse:
        return AsyncCoinGeckoResourceWithStreamingResponse(self)

    async def list(
        self,
        *,
        include_platform: bool | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListResponse:
        """
        List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            include_platform: Only false is supported; true returns 400.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListResponse: Successful response.

        Example:
            ```python
            coin_gecko = await client.coin_gecko.list(
                include_platform=False,
            )
            ```
        """
        return await self._get(
            "/api/v3/coins/list",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"include_platform": include_platform}, coin_gecko_list_params.CoinGeckoListParams
                ),
            ),
            cast_to=CoinGeckoListResponse,
        )

    async def list_markets(
        self,
        *,
        vs_currency: Literal["usd", "USD"] | Omit = omit,
        ids: List[CoinID] | Omit = omit,
        symbols: List[Market] | Omit = omit,
        page: int | Omit = omit,
        per_page: int | Omit = omit,
        order: Literal["market_cap_asc", "market_cap_desc", "volume_asc", "volume_desc", "id_asc", "id_desc"]
        | Omit = omit,
        sparkline: bool | Omit = omit,
        price_change_percentage: List[Literal["1h", "24h"]] | Omit = omit,
        precision: str | Omit = omit,
        locale: Literal["en"] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListMarketsResponse:
        """
        Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            vs_currency: Only USD is supported.
            ids: Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
            symbols: Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
            page: One-based page.
            per_page: Page size.
            order: Unavailable values sort last; ID breaks ties.
            sparkline: Only false is supported; true returns 400.
            price_change_percentage: Optional extra percentage-change fields.
            precision: full or decimal count 0..18.
            locale: Only English metadata is supported.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListMarketsResponse: Successful response.

        Example:
            ```python
            coin_gecko = await client.coin_gecko.list_markets(
                vs_currency="usd",
                page=1,
                per_page=100,
                order="market_cap_desc",
                sparkline=False,
                precision="full",
                locale="en",
            )
            ```
        """
        return await self._get(
            "/api/v3/coins/markets",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {
                        "vs_currency": vs_currency,
                        "ids": ids,
                        "symbols": symbols,
                        "page": page,
                        "per_page": per_page,
                        "order": order,
                        "sparkline": sparkline,
                        "price_change_percentage": price_change_percentage,
                        "precision": precision,
                        "locale": locale,
                    },
                    coin_gecko_list_markets_params.CoinGeckoListMarketsParams,
                ),
            ),
            cast_to=CoinGeckoListMarketsResponse,
        )

    async def retrieve(
        self,
        id: Literal[
            "bitcoin",
            "ethereum",
            "solana",
            "avalanche-2",
            "chainlink",
            "hyperliquid",
            "ripple",
            "aster",
            "official-trump",
            "binancecoin",
            "sidiora",
            "paxeer",
            "tesla",
            "nvidia",
            "alphabet",
            "gold",
            "nasdaq-100",
            "sp-500",
        ],
        *,
        localization: bool | Omit = omit,
        tickers: bool | Omit = omit,
        community_data: bool | Omit = omit,
        developer_data: bool | Omit = omit,
        sparkline: bool | Omit = omit,
        market_data: Literal[True] | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinDetail:
        """
        Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            id: Local adapter identifier; not an official provider registry ID.
            localization: Only false is supported; true returns 400.
            tickers: Only false is supported; true returns 400.
            community_data: Only false is supported; true returns 400.
            developer_data: Only false is supported; true returns 400.
            sparkline: Only false is supported; true returns 400.
            market_data: Must be true.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinDetail: Successful response.

        Example:
            ```python
            coin_gecko = await client.coin_gecko.retrieve(
                id="bitcoin",
                localization=False,
                tickers=False,
                community_data=False,
                developer_data=False,
                sparkline=False,
                market_data=True,
            )
            ```
        """
        if id is None or (isinstance(id, str) and not id):
            raise ValueError(f"Expected a non-empty value for `id` but received {id!r}")
        return await self._get(
            path_template("/api/v3/coins/{id}", **{"id": id}),
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {
                        "localization": localization,
                        "tickers": tickers,
                        "community_data": community_data,
                        "developer_data": developer_data,
                        "sparkline": sparkline,
                        "market_data": market_data,
                    },
                    coin_gecko_retrieve_params.CoinGeckoRetrieveParams,
                ),
            ),
            cast_to=CoinDetail,
        )

    async def list_simple_prices(
        self,
        *,
        ids: List[CoinID] | Omit = omit,
        symbols: List[Market] | Omit = omit,
        vs_currencies: Literal["usd", "USD"] | Omit = omit,
        include_market_cap: bool | Omit = omit,
        include_24hr_vol: bool | Omit = omit,
        include_24hr_change: bool | Omit = omit,
        include_last_updated_at: bool | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> CoinGeckoListSimplePricesResponse:
        """
        Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.

        Args:
            ids: Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
            symbols: Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
            vs_currencies: Only one currency, USD.
            include_market_cap: Include the corresponding optional response field.
            include_24hr_vol: Include the corresponding optional response field.
            include_24hr_change: Include the corresponding optional response field.
            include_last_updated_at: Include the corresponding optional response field.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            CoinGeckoListSimplePricesResponse: Successful response.

        Example:
            ```python
            coin_gecko = await client.coin_gecko.list_simple_prices(
                vs_currencies="usd",
                include_market_cap=False,
                include_24hr_vol=False,
                include_24hr_change=False,
                include_last_updated_at=False,
            )
            ```
        """
        return await self._get(
            "/api/v3/simple/price",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {
                        "ids": ids,
                        "symbols": symbols,
                        "vs_currencies": vs_currencies,
                        "include_market_cap": include_market_cap,
                        "include_24hr_vol": include_24hr_vol,
                        "include_24hr_change": include_24hr_change,
                        "include_last_updated_at": include_last_updated_at,
                    },
                    coin_gecko_list_simple_prices_params.CoinGeckoListSimplePricesParams,
                ),
            ),
            cast_to=CoinGeckoListSimplePricesResponse,
        )


class CoinGeckoResourceWithRawResponse:
    def __init__(self, coin_gecko: CoinGeckoResource) -> None:
        self._coin_gecko = coin_gecko

        self.list = to_raw_response_wrapper(
            coin_gecko.list,
        )
        self.list_markets = to_raw_response_wrapper(
            coin_gecko.list_markets,
        )
        self.retrieve = to_raw_response_wrapper(
            coin_gecko.retrieve,
        )
        self.list_simple_prices = to_raw_response_wrapper(
            coin_gecko.list_simple_prices,
        )


class AsyncCoinGeckoResourceWithRawResponse:
    def __init__(self, coin_gecko: AsyncCoinGeckoResource) -> None:
        self._coin_gecko = coin_gecko

        self.list = async_to_raw_response_wrapper(
            coin_gecko.list,
        )
        self.list_markets = async_to_raw_response_wrapper(
            coin_gecko.list_markets,
        )
        self.retrieve = async_to_raw_response_wrapper(
            coin_gecko.retrieve,
        )
        self.list_simple_prices = async_to_raw_response_wrapper(
            coin_gecko.list_simple_prices,
        )


class CoinGeckoResourceWithStreamingResponse:
    def __init__(self, coin_gecko: CoinGeckoResource) -> None:
        self._coin_gecko = coin_gecko

        self.list = to_streamed_response_wrapper(
            coin_gecko.list,
        )
        self.list_markets = to_streamed_response_wrapper(
            coin_gecko.list_markets,
        )
        self.retrieve = to_streamed_response_wrapper(
            coin_gecko.retrieve,
        )
        self.list_simple_prices = to_streamed_response_wrapper(
            coin_gecko.list_simple_prices,
        )


class AsyncCoinGeckoResourceWithStreamingResponse:
    def __init__(self, coin_gecko: AsyncCoinGeckoResource) -> None:
        self._coin_gecko = coin_gecko

        self.list = async_to_streamed_response_wrapper(
            coin_gecko.list,
        )
        self.list_markets = async_to_streamed_response_wrapper(
            coin_gecko.list_markets,
        )
        self.retrieve = async_to_streamed_response_wrapper(
            coin_gecko.retrieve,
        )
        self.list_simple_prices = async_to_streamed_response_wrapper(
            coin_gecko.list_simple_prices,
        )
