# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

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
from ..types.explorer_health import ExplorerHealth
from ..types.explorer_list_blocks_response import ExplorerListBlocksResponse
from ..types import explorer_list_blocks_params, explorer_list_transactions_params
from ..types.explorer_list_transactions_response import ExplorerListTransactionsResponse

__all__ = ["ExplorerResource", "AsyncExplorerResource"]


class ExplorerResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> ExplorerResourceWithRawResponse:
        return ExplorerResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> ExplorerResourceWithStreamingResponse:
        return ExplorerResourceWithStreamingResponse(self)

    def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerHealth:
        """
        Get EVM explorer health

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerHealth: Successful response.

        Example:
            ```python
            explorer = client.explorer.list_health()
            ```
        """
        return self._get(
            "/api/explorer/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=ExplorerHealth,
        )

    def list_blocks(
        self,
        *,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerListBlocksResponse:
        """
        In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

        Args:
            limit: Newest-first count. Numeric values are clamped to 1..50.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerListBlocksResponse: Successful response.

        Example:
            ```python
            explorer = client.explorer.list_blocks(
                limit=20,
            )
            ```
        """
        return self._get(
            "/api/explorer/blocks",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform({"limit": limit}, explorer_list_blocks_params.ExplorerListBlocksParams),
            ),
            cast_to=ExplorerListBlocksResponse,
        )

    def list_transactions(
        self,
        *,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerListTransactionsResponse:
        """
        Explorer activity.

        Args:
            limit: Newest-first count. Numeric values are clamped to 1..200.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerListTransactionsResponse: Successful response.

        Example:
            ```python
            explorer = client.explorer.list_transactions(
                limit=50,
            )
            ```
        """
        return self._get(
            "/api/explorer/transactions",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=maybe_transform(
                    {"limit": limit}, explorer_list_transactions_params.ExplorerListTransactionsParams
                ),
            ),
            cast_to=ExplorerListTransactionsResponse,
        )


class AsyncExplorerResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncExplorerResourceWithRawResponse:
        return AsyncExplorerResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncExplorerResourceWithStreamingResponse:
        return AsyncExplorerResourceWithStreamingResponse(self)

    async def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerHealth:
        """
        Get EVM explorer health

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerHealth: Successful response.

        Example:
            ```python
            explorer = await client.explorer.list_health()
            ```
        """
        return await self._get(
            "/api/explorer/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=ExplorerHealth,
        )

    async def list_blocks(
        self,
        *,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerListBlocksResponse:
        """
        In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.

        Args:
            limit: Newest-first count. Numeric values are clamped to 1..50.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerListBlocksResponse: Successful response.

        Example:
            ```python
            explorer = await client.explorer.list_blocks(
                limit=20,
            )
            ```
        """
        return await self._get(
            "/api/explorer/blocks",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"limit": limit}, explorer_list_blocks_params.ExplorerListBlocksParams
                ),
            ),
            cast_to=ExplorerListBlocksResponse,
        )

    async def list_transactions(
        self,
        *,
        limit: int | Omit = omit,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> ExplorerListTransactionsResponse:
        """
        Explorer activity.

        Args:
            limit: Newest-first count. Numeric values are clamped to 1..200.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            ExplorerListTransactionsResponse: Successful response.

        Example:
            ```python
            explorer = await client.explorer.list_transactions(
                limit=50,
            )
            ```
        """
        return await self._get(
            "/api/explorer/transactions",
            options=make_request_options(
                extra_headers=extra_headers,
                extra_query=extra_query,
                extra_body=extra_body,
                timeout=timeout,
                query=await async_maybe_transform(
                    {"limit": limit}, explorer_list_transactions_params.ExplorerListTransactionsParams
                ),
            ),
            cast_to=ExplorerListTransactionsResponse,
        )


class ExplorerResourceWithRawResponse:
    def __init__(self, explorer: ExplorerResource) -> None:
        self._explorer = explorer

        self.list_health = to_raw_response_wrapper(
            explorer.list_health,
        )
        self.list_blocks = to_raw_response_wrapper(
            explorer.list_blocks,
        )
        self.list_transactions = to_raw_response_wrapper(
            explorer.list_transactions,
        )


class AsyncExplorerResourceWithRawResponse:
    def __init__(self, explorer: AsyncExplorerResource) -> None:
        self._explorer = explorer

        self.list_health = async_to_raw_response_wrapper(
            explorer.list_health,
        )
        self.list_blocks = async_to_raw_response_wrapper(
            explorer.list_blocks,
        )
        self.list_transactions = async_to_raw_response_wrapper(
            explorer.list_transactions,
        )


class ExplorerResourceWithStreamingResponse:
    def __init__(self, explorer: ExplorerResource) -> None:
        self._explorer = explorer

        self.list_health = to_streamed_response_wrapper(
            explorer.list_health,
        )
        self.list_blocks = to_streamed_response_wrapper(
            explorer.list_blocks,
        )
        self.list_transactions = to_streamed_response_wrapper(
            explorer.list_transactions,
        )


class AsyncExplorerResourceWithStreamingResponse:
    def __init__(self, explorer: AsyncExplorerResource) -> None:
        self._explorer = explorer

        self.list_health = async_to_streamed_response_wrapper(
            explorer.list_health,
        )
        self.list_blocks = async_to_streamed_response_wrapper(
            explorer.list_blocks,
        )
        self.list_transactions = async_to_streamed_response_wrapper(
            explorer.list_transactions,
        )
