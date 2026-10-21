# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

from .._types import Body, Query, Headers, NotGiven, not_given
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
from ..types.ws_token import WsToken
from ..types.ws_scope import WsScope
from ..types import gateway_create_web_socket_token_params

__all__ = ["GatewayResource", "AsyncGatewayResource"]


class GatewayResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> GatewayResourceWithRawResponse:
        return GatewayResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> GatewayResourceWithStreamingResponse:
        return GatewayResourceWithStreamingResponse(self)

    def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> str:
        """
        Checks Nginx only; does not probe the auth helper, database or market services.

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            str: Gateway is responding.

        Example:
            ```python
            gateway = client.gateway.list_health()
            ```
        """
        extra_headers = {"Accept": "text/plain", **(extra_headers or {})}
        return self._get(
            "/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=str,
        )

    def create_web_socket_token(
        self,
        *,
        symbol: WsScope,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> WsToken:
        """
        Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

        Args:
            symbol: Body parameter.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            WsToken: Opaque credential for the requested stream scope.

        Example:
            ```python
            gateway = client.gateway.create_web_socket_token(
                symbol="btc",
            )
            ```
        """
        return self._post(
            "/auth/ws-token",
            body=maybe_transform(
                {"symbol": symbol},
                gateway_create_web_socket_token_params.GatewayCreateWebSocketTokenParams,
            ),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=WsToken,
        )


class AsyncGatewayResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncGatewayResourceWithRawResponse:
        return AsyncGatewayResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncGatewayResourceWithStreamingResponse:
        return AsyncGatewayResourceWithStreamingResponse(self)

    async def list_health(
        self,
        *,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> str:
        """
        Checks Nginx only; does not probe the auth helper, database or market services.

        Args:
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            str: Gateway is responding.

        Example:
            ```python
            gateway = await client.gateway.list_health()
            ```
        """
        extra_headers = {"Accept": "text/plain", **(extra_headers or {})}
        return await self._get(
            "/health",
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=str,
        )

    async def create_web_socket_token(
        self,
        *,
        symbol: WsScope,
        # Use the following arguments if you need to pass additional parameters to the API that aren't available via kwargs.
        # The extra values given here take precedence over values defined on the client or passed to this method.
        extra_headers: Headers | None = None,
        extra_query: Query | None = None,
        extra_body: Body | None = None,
        timeout: float | httpx.Timeout | None | NotGiven = not_given,
    ) -> WsToken:
        """
        Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.

        Args:
            symbol: Body parameter.
            extra_headers: Send extra headers with the request.
            extra_query: Send extra query parameters with the request.
            extra_body: Send extra JSON properties with the request.
            timeout: Override the client-level default timeout for this request, in seconds.

        Returns:
            WsToken: Opaque credential for the requested stream scope.

        Example:
            ```python
            gateway = await client.gateway.create_web_socket_token(
                symbol="btc",
            )
            ```
        """
        return await self._post(
            "/auth/ws-token",
            body=await async_maybe_transform(
                {"symbol": symbol},
                gateway_create_web_socket_token_params.GatewayCreateWebSocketTokenParams,
            ),
            options=make_request_options(
                extra_headers=extra_headers, extra_query=extra_query, extra_body=extra_body, timeout=timeout
            ),
            cast_to=WsToken,
        )


class GatewayResourceWithRawResponse:
    def __init__(self, gateway: GatewayResource) -> None:
        self._gateway = gateway

        self.list_health = to_raw_response_wrapper(
            gateway.list_health,
        )
        self.create_web_socket_token = to_raw_response_wrapper(
            gateway.create_web_socket_token,
        )


class AsyncGatewayResourceWithRawResponse:
    def __init__(self, gateway: AsyncGatewayResource) -> None:
        self._gateway = gateway

        self.list_health = async_to_raw_response_wrapper(
            gateway.list_health,
        )
        self.create_web_socket_token = async_to_raw_response_wrapper(
            gateway.create_web_socket_token,
        )


class GatewayResourceWithStreamingResponse:
    def __init__(self, gateway: GatewayResource) -> None:
        self._gateway = gateway

        self.list_health = to_streamed_response_wrapper(
            gateway.list_health,
        )
        self.create_web_socket_token = to_streamed_response_wrapper(
            gateway.create_web_socket_token,
        )


class AsyncGatewayResourceWithStreamingResponse:
    def __init__(self, gateway: AsyncGatewayResource) -> None:
        self._gateway = gateway

        self.list_health = async_to_streamed_response_wrapper(
            gateway.list_health,
        )
        self.create_web_socket_token = async_to_streamed_response_wrapper(
            gateway.create_web_socket_token,
        )
