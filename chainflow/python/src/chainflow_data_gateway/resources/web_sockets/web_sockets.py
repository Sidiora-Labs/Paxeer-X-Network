# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import httpx

from typing import Callable

from ..._types import Query, Headers
from ..._compat import cached_property
from ..._resource import SyncAPIResource, AsyncAPIResource
from ..._response import (
    to_raw_response_wrapper,
    to_streamed_response_wrapper,
    async_to_raw_response_wrapper,
    async_to_streamed_response_wrapper,
)
from ..._base_client import make_request_options
from ...types.websocket_connection_options import WebSocketConnectionOptions
from ...types.websocket_reconnection import ReconnectingEvent, ReconnectingOverrides
from .ws import (
    WebSocketsResourceConnectionManager as WebSocketsResourceConnectionManager,
    AsyncWebSocketsResourceConnectionManager as AsyncWebSocketsResourceConnectionManager,
    WebSocketsResourceConnection as WebSocketsResourceConnection,
    AsyncWebSocketsResourceConnection as AsyncWebSocketsResourceConnection,
)

__all__ = ["WebSocketsResource", "AsyncWebSocketsResource"]


class WebSocketsResource(SyncAPIResource):
    @cached_property
    def with_raw_response(self) -> WebSocketsResourceWithRawResponse:
        return WebSocketsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> WebSocketsResourceWithStreamingResponse:
        return WebSocketsResourceWithStreamingResponse(self)

    def connect_price_feed(
        self,
        market: str,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> WebSocketsResourceConnectionManager:
        return WebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/{market}",
            market=market,
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_markets(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> WebSocketsResourceConnectionManager:
        return WebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/markets",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_explorer(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> WebSocketsResourceConnectionManager:
        return WebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/explorer",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_explorer_alias(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> WebSocketsResourceConnectionManager:
        return WebSocketsResourceConnectionManager(
            client=self._client,
            path="/explorer",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )


class AsyncWebSocketsResource(AsyncAPIResource):
    @cached_property
    def with_raw_response(self) -> AsyncWebSocketsResourceWithRawResponse:
        return AsyncWebSocketsResourceWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncWebSocketsResourceWithStreamingResponse:
        return AsyncWebSocketsResourceWithStreamingResponse(self)

    def connect_price_feed(
        self,
        market: str,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> AsyncWebSocketsResourceConnectionManager:
        return AsyncWebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/{market}",
            market=market,
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_markets(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> AsyncWebSocketsResourceConnectionManager:
        return AsyncWebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/markets",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_explorer(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> AsyncWebSocketsResourceConnectionManager:
        return AsyncWebSocketsResourceConnectionManager(
            client=self._client,
            path="/ws/explorer",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )

    def connect_explorer_alias(
        self,
        extra_query: Query = {},
        extra_headers: Headers = {},
        websocket_connection_options: WebSocketConnectionOptions = {},
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> AsyncWebSocketsResourceConnectionManager:
        return AsyncWebSocketsResourceConnectionManager(
            client=self._client,
            path="/explorer",
            extra_query=extra_query,
            extra_headers=extra_headers,
            websocket_connection_options=websocket_connection_options,
            on_reconnecting=on_reconnecting,
            max_retries=max_retries,
            initial_delay=initial_delay,
            max_delay=max_delay,
            max_queue_size=max_queue_size,
        )


class WebSocketsResourceWithRawResponse:
    def __init__(self, web_sockets: WebSocketsResource) -> None:
        self._web_sockets = web_sockets


class AsyncWebSocketsResourceWithRawResponse:
    def __init__(self, web_sockets: AsyncWebSocketsResource) -> None:
        self._web_sockets = web_sockets


class WebSocketsResourceWithStreamingResponse:
    def __init__(self, web_sockets: WebSocketsResource) -> None:
        self._web_sockets = web_sockets


class AsyncWebSocketsResourceWithStreamingResponse:
    def __init__(self, web_sockets: AsyncWebSocketsResource) -> None:
        self._web_sockets = web_sockets
