# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import json
import time
import random
import logging
from types import TracebackType
from typing import TYPE_CHECKING, Any, Union, Callable, Awaitable, Iterator, cast
from typing_extensions import AsyncIterator

import httpx

from ..._models import BaseModel, construct_type_unchecked
from ..._types import Headers, Query
from ..._exceptions import ChainflowDataGatewayError, WebSocketConnectionClosedError
from ..._send_queue import SendQueue
from ..._base_client import _merge_mappings
from ..._event_handler import EventHandlerRegistry
from ..._utils import path_template, maybe_transform, async_maybe_transform
from ...types.websocket_connection_options import WebSocketConnectionOptions
from ...types.websocket_reconnection import ReconnectingEvent, ReconnectingOverrides, is_recoverable_close

if TYPE_CHECKING:
    from websockets.sync.client import ClientConnection as WebSocketConnection
    from websockets.asyncio.client import ClientConnection as AsyncWebSocketConnection
    from ..._client import ChainflowDataGateway, AsyncChainflowDataGateway

__all__ = [
    "WebSocketsResourceConnectionManager",
    "AsyncWebSocketsResourceConnectionManager",
    "WebSocketsResourceConnection",
    "AsyncWebSocketsResourceConnection",
]

log: logging.Logger = logging.getLogger(__name__)


def _cookie_header(cookies: dict[str, str]) -> Headers:
    """Serialize auth cookies into the WebSocket upgrade headers."""
    if not cookies:
        return {}

    return {"Cookie": "; ".join(f"{key}={value}" for key, value in cookies.items())}


class WebSocketsResourceConnection:
    """Represents a live synchronous WebSocket connection."""

    def __init__(
        self,
        connection: WebSocketConnection,
        *,
        make_ws: Callable[[Query, Headers], WebSocketConnection] | None = None,
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        extra_query: Query = {},
        extra_headers: Headers = {},
        send_queue: SendQueue | None = None,
    ) -> None:
        self._connection = connection
        self._make_ws = make_ws
        self._on_reconnecting = on_reconnecting
        self._max_retries = max_retries
        self._initial_delay = initial_delay
        self._max_delay = max_delay
        self._extra_query = extra_query
        self._extra_headers = extra_headers
        self._intentionally_closed = False
        self._is_reconnecting = False
        self._send_queue = send_queue or SendQueue()
        self._event_handler_registry = EventHandlerRegistry(use_lock=True)

    def __iter__(self) -> Iterator[Any]:
        """Yield events until the connection closes."""
        from websockets.exceptions import ConnectionClosedOK, ConnectionClosedError

        while True:
            try:
                yield self.recv()
            except ConnectionClosedOK:
                return
            except ConnectionClosedError as exc:
                if not self._reconnect(exc):
                    unsent = self._send_queue.drain()
                    if unsent:
                        raise WebSocketConnectionClosedError(
                            "WebSocket connection closed with unsent messages",
                            unsent_messages=unsent,
                        ) from exc
                    raise

    def recv(self) -> Any:
        """Receive and parse the next websocket message."""
        return self.parse_event(self.recv_bytes())

    def recv_bytes(self) -> bytes:
        """Receive the next websocket message as raw bytes."""
        message = self._connection.recv(decode=False)
        log.debug("Received WebSocket message: %s", message)
        return message

    def send(self, event: Any) -> None:
        data = (
            event.to_json(use_api_names=True, exclude_defaults=True, exclude_unset=True)
            if isinstance(event, BaseModel)
            else json.dumps(maybe_transform(event, ConnectPriceFeedClientEventParam))
        )
        if self._is_reconnecting:
            self._send_queue.enqueue(data)
            return
        try:
            self._connection.send(data)
        except Exception:
            self._send_queue.enqueue(data)
            raise

    def send_raw(self, data: bytes | str) -> None:
        if self._is_reconnecting:
            raw = data if isinstance(data, str) else data.decode("utf-8")
            self._send_queue.enqueue(raw)
            return
        self._connection.send(data)

    def close(self, *, code: int = 1000, reason: str = "") -> None:
        self._intentionally_closed = True
        self._connection.close(code=code, reason=reason)

    def parse_event(self, data: str | bytes) -> Any:
        """Convert a raw websocket message into the generated server event type."""
        return cast(Any, construct_type_unchecked(value=json.loads(data), type_=cast(Any, Any)))

    def _reconnect(self, exc: Exception) -> bool:
        """Attempt to reconnect after a recoverable close exception."""
        if self._on_reconnecting is None or self._make_ws is None:
            return False

        from websockets.exceptions import ConnectionClosedError

        close_code = 1006
        if isinstance(exc, ConnectionClosedError) and exc.rcvd is not None:
            close_code = exc.rcvd.code

        if not is_recoverable_close(close_code):
            return False

        self._is_reconnecting = True
        for attempt in range(1, self._max_retries + 1):
            base_delay = min(self._initial_delay * (2 ** (attempt - 1)), self._max_delay)
            jitter = 0.75 + random.random() * 0.25
            delay = base_delay * jitter

            event = ReconnectingEvent(
                attempt=attempt,
                max_attempts=self._max_retries,
                delay=delay,
                close_code=close_code,
                extra_query=self._extra_query,
                extra_headers=self._extra_headers,
            )
            try:
                overrides = self._on_reconnecting(event)
            except Exception:
                self._is_reconnecting = False
                return False
            if overrides is not None and overrides.get("abort"):
                self._is_reconnecting = False
                return False
            if overrides is not None:
                if "extra_query" in overrides:
                    self._extra_query = overrides["extra_query"]
                if "extra_headers" in overrides:
                    self._extra_headers = overrides["extra_headers"]
            log.info(
                "Reconnecting to WebSocket API (attempt %d/%d) after %.1fs delay",
                attempt,
                self._max_retries,
                delay,
            )
            time.sleep(delay)
            if self._intentionally_closed:
                self._is_reconnecting = False
                return False
            try:
                self._connection = self._make_ws(self._extra_query, self._extra_headers)
                log.info("Reconnected to WebSocket API")
                self._flush_send_queue()
                self._is_reconnecting = False
                return True
            except Exception:
                pass
        self._is_reconnecting = False
        return False

    def _flush_send_queue(self) -> None:
        """Send queued messages over the current connection after it opens or reconnects."""
        self._send_queue.flush_sync(lambda data: self._connection.send(data))

    def on(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[WebSocketsResourceConnection, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register an event handler, or return a decorator that registers one."""
        if handler is not None:
            self._event_handler_registry.add(event_type, handler)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self._event_handler_registry.add(event_type, fn)
            return fn

        return decorator

    def off(self, event_type: str, handler: Callable[..., Any]) -> WebSocketsResourceConnection:
        """Remove a previously registered event handler."""
        self._event_handler_registry.remove(event_type, handler)
        return self

    def once(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[WebSocketsResourceConnection, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register a one-time event handler."""
        if handler is not None:
            self._event_handler_registry.add(event_type, handler, once=True)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self._event_handler_registry.add(event_type, fn, once=True)
            return fn

        return decorator

    def dispatch_events(self) -> None:
        """Dispatch incoming events to registered handlers until the connection closes."""
        for event in self:
            event_type = event.type
            specific = self._event_handler_registry.get_handlers(event_type)
            generic = self._event_handler_registry.get_handlers("event")

            for handler in specific:
                handler(event)

            for handler in generic:
                handler(event)


class AsyncWebSocketsResourceConnection:
    """Represents a live asynchronous WebSocket connection."""

    def __init__(
        self,
        connection: AsyncWebSocketConnection,
        *,
        make_ws: Callable[[Query, Headers], Awaitable[AsyncWebSocketConnection]] | None = None,
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        extra_query: Query = {},
        extra_headers: Headers = {},
        send_queue: SendQueue | None = None,
    ) -> None:
        self._connection = connection
        self._make_ws = make_ws
        self._on_reconnecting = on_reconnecting
        self._max_retries = max_retries
        self._initial_delay = initial_delay
        self._max_delay = max_delay
        self._extra_query = extra_query
        self._extra_headers = extra_headers
        self._intentionally_closed = False
        self._is_reconnecting = False
        self._send_queue = send_queue or SendQueue()
        self._event_handler_registry = EventHandlerRegistry(use_lock=False)

    async def __aiter__(self) -> AsyncIterator[Any]:
        """Yield events until the connection closes."""
        from websockets.exceptions import ConnectionClosedOK, ConnectionClosedError

        while True:
            try:
                yield await self.recv()
            except ConnectionClosedOK:
                return
            except ConnectionClosedError as exc:
                if not await self._reconnect(exc):
                    unsent = self._send_queue.drain()
                    if unsent:
                        raise WebSocketConnectionClosedError(
                            "WebSocket connection closed with unsent messages",
                            unsent_messages=unsent,
                        ) from exc
                    raise

    async def recv(self) -> Any:
        """Receive and parse the next websocket message."""
        return self.parse_event(await self.recv_bytes())

    async def recv_bytes(self) -> bytes:
        """Receive the next websocket message as raw bytes."""
        message = await self._connection.recv(decode=False)
        log.debug("Received WebSocket message: %s", message)
        return message

    async def send(self, event: Any) -> None:
        data = (
            event.to_json(use_api_names=True, exclude_defaults=True, exclude_unset=True)
            if isinstance(event, BaseModel)
            else json.dumps(await async_maybe_transform(event, ConnectPriceFeedClientEventParam))
        )
        if self._is_reconnecting:
            self._send_queue.enqueue(data)
            return
        try:
            await self._connection.send(data)
        except Exception:
            self._send_queue.enqueue(data)
            raise

    async def send_raw(self, data: bytes | str) -> None:
        if self._is_reconnecting:
            raw = data if isinstance(data, str) else data.decode("utf-8")
            self._send_queue.enqueue(raw)
            return
        await self._connection.send(data)

    async def close(self, *, code: int = 1000, reason: str = "") -> None:
        self._intentionally_closed = True
        await self._connection.close(code=code, reason=reason)

    def parse_event(self, data: str | bytes) -> Any:
        """Convert a raw websocket message into the generated server event type."""
        return cast(Any, construct_type_unchecked(value=json.loads(data), type_=cast(Any, Any)))

    async def _reconnect(self, exc: Exception) -> bool:
        """Attempt to reconnect after a recoverable close exception."""
        import asyncio

        if self._on_reconnecting is None or self._make_ws is None:
            return False

        from websockets.exceptions import ConnectionClosedError

        close_code = 1006
        if isinstance(exc, ConnectionClosedError) and exc.rcvd is not None:
            close_code = exc.rcvd.code

        if not is_recoverable_close(close_code):
            return False

        self._is_reconnecting = True
        for attempt in range(1, self._max_retries + 1):
            base_delay = min(self._initial_delay * (2 ** (attempt - 1)), self._max_delay)
            jitter = 0.75 + random.random() * 0.25
            delay = base_delay * jitter

            event = ReconnectingEvent(
                attempt=attempt,
                max_attempts=self._max_retries,
                delay=delay,
                close_code=close_code,
                extra_query=self._extra_query,
                extra_headers=self._extra_headers,
            )
            try:
                overrides = self._on_reconnecting(event)
            except Exception:
                self._is_reconnecting = False
                return False
            if overrides is not None and overrides.get("abort"):
                self._is_reconnecting = False
                return False
            if overrides is not None:
                if "extra_query" in overrides:
                    self._extra_query = overrides["extra_query"]
                if "extra_headers" in overrides:
                    self._extra_headers = overrides["extra_headers"]
            log.info(
                "Reconnecting to WebSocket API (attempt %d/%d) after %.1fs delay",
                attempt,
                self._max_retries,
                delay,
            )
            await asyncio.sleep(delay)
            if self._intentionally_closed:
                self._is_reconnecting = False
                return False
            try:
                self._connection = await self._make_ws(self._extra_query, self._extra_headers)
                log.info("Reconnected to WebSocket API")
                await self._flush_send_queue()
                self._is_reconnecting = False
                return True
            except Exception:
                pass
        self._is_reconnecting = False
        return False

    async def _flush_send_queue(self) -> None:
        """Send queued messages over the current connection after it opens or reconnects."""

        async def _send(data: str) -> None:
            await self._connection.send(data)

        await self._send_queue.flush_async(_send)

    def on(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[AsyncWebSocketsResourceConnection, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register an event handler, or return a decorator that registers one."""
        if handler is not None:
            self._event_handler_registry.add(event_type, handler)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self._event_handler_registry.add(event_type, fn)
            return fn

        return decorator

    def off(self, event_type: str, handler: Callable[..., Any]) -> AsyncWebSocketsResourceConnection:
        """Remove a previously registered event handler."""
        self._event_handler_registry.remove(event_type, handler)
        return self

    def once(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[AsyncWebSocketsResourceConnection, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register a one-time event handler."""
        if handler is not None:
            self._event_handler_registry.add(event_type, handler, once=True)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self._event_handler_registry.add(event_type, fn, once=True)
            return fn

        return decorator

    async def dispatch_events(self) -> None:
        """Dispatch incoming events to registered handlers until the connection closes."""
        import asyncio

        async for event in self:
            event_type = event.type
            specific = self._event_handler_registry.get_handlers(event_type)
            generic = self._event_handler_registry.get_handlers("event")

            for handler in specific:
                result = handler(event)
                if asyncio.iscoroutine(result):
                    await result

            for handler in generic:
                result = handler(event)
                if asyncio.iscoroutine(result):
                    await result


class WebSocketsResourceConnectionManager:
    """Context-manager wrapper used by `connect()` to open a websocket lifecycle."""

    def __init__(
        self,
        *,
        client: ChainflowDataGateway,
        market: str,
        extra_query: Query,
        extra_headers: Headers,
        websocket_connection_options: WebSocketConnectionOptions,
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> None:
        self.__client = client
        self.__market = market
        self.__connection: WebSocketsResourceConnection | None = None
        self.__extra_query = extra_query
        self.__extra_headers = extra_headers
        self.__websocket_connection_options = websocket_connection_options
        self.__on_reconnecting = on_reconnecting
        self.__max_retries = max_retries
        self.__initial_delay = initial_delay
        self.__max_delay = max_delay
        self.__send_queue = SendQueue(max_bytes=max_queue_size)
        self.__event_handler_registry = EventHandlerRegistry(use_lock=True)

    def send(self, event: Any) -> None:
        """Queue a message to send as soon as the websocket opens."""
        data = (
            event.to_json(use_api_names=True, exclude_defaults=True, exclude_unset=True)
            if isinstance(event, BaseModel)
            else json.dumps(maybe_transform(event, ConnectPriceFeedClientEventParam))
        )
        self.__send_queue.enqueue(data)

    def on(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[WebSocketsResourceConnectionManager, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register an event handler before the connection opens."""
        if handler is not None:
            self.__event_handler_registry.add(event_type, handler)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self.__event_handler_registry.add(event_type, fn)
            return fn

        return decorator

    def off(self, event_type: str, handler: Callable[..., Any]) -> WebSocketsResourceConnectionManager:
        """Remove a previously registered event handler."""
        self.__event_handler_registry.remove(event_type, handler)
        return self

    def once(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[WebSocketsResourceConnectionManager, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register a one-time event handler before the connection opens."""
        if handler is not None:
            self.__event_handler_registry.add(event_type, handler, once=True)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self.__event_handler_registry.add(event_type, fn, once=True)
            return fn

        return decorator

    def __enter__(self) -> WebSocketsResourceConnection:
        ws = self._connect_ws(self.__extra_query, self.__extra_headers)

        self.__connection = WebSocketsResourceConnection(
            ws,
            make_ws=self._connect_ws if self.__on_reconnecting is not None else None,
            on_reconnecting=self.__on_reconnecting,
            max_retries=self.__max_retries,
            initial_delay=self.__initial_delay,
            max_delay=self.__max_delay,
            extra_query=self.__extra_query,
            extra_headers=self.__extra_headers,
            send_queue=self.__send_queue,
        )

        self.__event_handler_registry.merge_into(self.__connection._event_handler_registry)
        self.__connection._flush_send_queue()

        return self.__connection

    enter = __enter__

    def _connect_ws(self, extra_query: Query, extra_headers: Headers) -> WebSocketConnection:
        try:
            from websockets.sync.client import connect
        except ImportError as exc:
            raise ChainflowDataGatewayError(
                "You need to install `chainflowDataGateway[websockets]` to use this method"
            ) from exc

        if not self.__market:
            raise ValueError(f"Expected a non-empty value for `market` but received {self.__market!r}")
        url = self._prepare_url()
        params = _merge_mappings(
            {
                **url.params,
                **self.__client._auth_query({}),
                **self.__client.default_query,
            },
            extra_query,
        )
        cookies = self.__client._auth_cookies({})
        headers = _merge_mappings(
            {
                **self.__client.auth_headers,
                **_cookie_header(cookies),
            },
            extra_headers,
        )
        self.__client._validate_headers(headers, extra_headers, params, cookies)
        url = url.copy_with(params=params)
        log.debug("Connecting to %s", url)
        if self.__websocket_connection_options:
            log.debug("Connection options: %s", self.__websocket_connection_options)

        return connect(
            str(url),
            user_agent_header=self.__client.user_agent,
            additional_headers=headers,
            **self.__websocket_connection_options,
        )

    def _prepare_url(self) -> httpx.URL:
        if self.__client.websocket_base_url is not None:
            base_url = httpx.URL(self.__client.websocket_base_url)
        else:
            scheme = self.__client._base_url.scheme
            ws_scheme = "ws" if scheme == "http" else "wss"
            base_url = self.__client._base_url.copy_with(scheme=ws_scheme)

        merge_raw_path = base_url.raw_path.rstrip(b"/") + path_template("/ws/{market}", market=self.__market).encode(
            "utf-8"
        )
        return base_url.copy_with(raw_path=merge_raw_path)

    def __exit__(
        self, exc_type: type[BaseException] | None, exc: BaseException | None, tb: TracebackType | None
    ) -> None:
        if self.__connection is not None:
            self.__connection.close()


class AsyncWebSocketsResourceConnectionManager:
    """Async context-manager wrapper used by `connect()` to open a websocket lifecycle."""

    def __init__(
        self,
        *,
        client: AsyncChainflowDataGateway,
        market: str,
        extra_query: Query,
        extra_headers: Headers,
        websocket_connection_options: WebSocketConnectionOptions,
        on_reconnecting: Callable[[ReconnectingEvent], ReconnectingOverrides | None] | None = None,
        max_retries: int = 5,
        initial_delay: float = 0.5,
        max_delay: float = 8.0,
        max_queue_size: int = 1_048_576,
    ) -> None:
        self.__client = client
        self.__market = market
        self.__connection: AsyncWebSocketsResourceConnection | None = None
        self.__extra_query = extra_query
        self.__extra_headers = extra_headers
        self.__websocket_connection_options = websocket_connection_options
        self.__on_reconnecting = on_reconnecting
        self.__max_retries = max_retries
        self.__initial_delay = initial_delay
        self.__max_delay = max_delay
        self.__send_queue = SendQueue(max_bytes=max_queue_size)
        self.__event_handler_registry = EventHandlerRegistry(use_lock=False)

    def send(self, event: Any) -> None:
        """Queue a message to send as soon as the websocket opens."""
        data = (
            event.to_json(use_api_names=True, exclude_defaults=True, exclude_unset=True)
            if isinstance(event, BaseModel)
            else json.dumps(maybe_transform(event, ConnectPriceFeedClientEventParam))
        )
        self.__send_queue.enqueue(data)

    def on(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[AsyncWebSocketsResourceConnectionManager, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register an event handler before the connection opens."""
        if handler is not None:
            self.__event_handler_registry.add(event_type, handler)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self.__event_handler_registry.add(event_type, fn)
            return fn

        return decorator

    def off(self, event_type: str, handler: Callable[..., Any]) -> AsyncWebSocketsResourceConnectionManager:
        """Remove a previously registered event handler."""
        self.__event_handler_registry.remove(event_type, handler)
        return self

    def once(
        self, event_type: str, handler: Callable[..., Any] | None = None
    ) -> Union[AsyncWebSocketsResourceConnectionManager, Callable[[Callable[..., Any]], Callable[..., Any]]]:
        """Register a one-time event handler before the connection opens."""
        if handler is not None:
            self.__event_handler_registry.add(event_type, handler, once=True)
            return self

        def decorator(fn: Callable[..., Any]) -> Callable[..., Any]:
            self.__event_handler_registry.add(event_type, fn, once=True)
            return fn

        return decorator

    async def __aenter__(self) -> AsyncWebSocketsResourceConnection:
        ws = await self._connect_ws(self.__extra_query, self.__extra_headers)

        self.__connection = AsyncWebSocketsResourceConnection(
            ws,
            make_ws=self._connect_ws if self.__on_reconnecting is not None else None,
            on_reconnecting=self.__on_reconnecting,
            max_retries=self.__max_retries,
            initial_delay=self.__initial_delay,
            max_delay=self.__max_delay,
            extra_query=self.__extra_query,
            extra_headers=self.__extra_headers,
            send_queue=self.__send_queue,
        )

        self.__event_handler_registry.merge_into(self.__connection._event_handler_registry)
        await self.__connection._flush_send_queue()

        return self.__connection

    enter = __aenter__

    async def _connect_ws(self, extra_query: Query, extra_headers: Headers) -> AsyncWebSocketConnection:
        try:
            from websockets.asyncio.client import connect
        except ImportError as exc:
            raise ChainflowDataGatewayError(
                "You need to install `chainflowDataGateway[websockets]` to use this method"
            ) from exc

        if not self.__market:
            raise ValueError(f"Expected a non-empty value for `market` but received {self.__market!r}")
        url = self._prepare_url()
        params = _merge_mappings(
            {
                **url.params,
                **self.__client._auth_query({}),
                **self.__client.default_query,
            },
            extra_query,
        )
        cookies = self.__client._auth_cookies({})
        headers = _merge_mappings(
            {
                **self.__client.auth_headers,
                **_cookie_header(cookies),
            },
            extra_headers,
        )
        self.__client._validate_headers(headers, extra_headers, params, cookies)
        url = url.copy_with(params=params)
        log.debug("Connecting to %s", url)
        if self.__websocket_connection_options:
            log.debug("Connection options: %s", self.__websocket_connection_options)

        return await connect(
            str(url),
            user_agent_header=self.__client.user_agent,
            additional_headers=headers,
            **self.__websocket_connection_options,
        )

    def _prepare_url(self) -> httpx.URL:
        if self.__client.websocket_base_url is not None:
            base_url = httpx.URL(self.__client.websocket_base_url)
        else:
            scheme = self.__client._base_url.scheme
            ws_scheme = "ws" if scheme == "http" else "wss"
            base_url = self.__client._base_url.copy_with(scheme=ws_scheme)

        merge_raw_path = base_url.raw_path.rstrip(b"/") + path_template("/ws/{market}", market=self.__market).encode(
            "utf-8"
        )
        return base_url.copy_with(raw_path=merge_raw_path)

    async def __aexit__(
        self, exc_type: type[BaseException] | None, exc: BaseException | None, tb: TracebackType | None
    ) -> None:
        if self.__connection is not None:
            await self.__connection.close()
