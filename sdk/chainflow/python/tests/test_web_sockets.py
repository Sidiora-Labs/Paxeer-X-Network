from __future__ import annotations

import json
from typing import Any, List

import pytest
from websockets.frames import Close
from websockets.exceptions import ConnectionClosedOK

from chainflow_data_gateway import ChainflowDataGateway, AsyncChainflowDataGateway
from chainflow_data_gateway._exceptions import WebSocketQueueFullError
from chainflow_data_gateway._send_queue import SendQueue
from chainflow_data_gateway.resources.web_sockets.ws import (
    WebSocketsResourceConnection,
    AsyncWebSocketsResourceConnection,
    WebSocketsResourceConnectionManager,
    AsyncWebSocketsResourceConnectionManager,
)

BASE_URL = "http://gateway.test/api"
EVENT = {"type": "subscribe", "channels": ["ticker"]}


class RecordingSocket:
    """Synchronous socket stand-in that replays frames and records sends."""

    def __init__(self, frames: List[bytes] | None = None) -> None:
        self.frames = list(frames or [])
        self.sent: List[Any] = []
        self.closed = False

    def recv(self, decode: bool = False) -> bytes:
        if not self.frames:
            raise ConnectionClosedOK(Close(1000, ""), None)
        return self.frames.pop(0)

    def send(self, data: Any) -> None:
        self.sent.append(data)

    def close(self, code: int = 1000, reason: str = "") -> None:
        self.closed = True


class AsyncRecordingSocket(RecordingSocket):
    async def recv(self, decode: bool = False) -> bytes:  # type: ignore[override]
        return RecordingSocket.recv(self, decode)

    async def send(self, data: Any) -> None:  # type: ignore[override]
        RecordingSocket.send(self, data)

    async def close(self, code: int = 1000, reason: str = "") -> None:  # type: ignore[override]
        RecordingSocket.close(self, code, reason)


def _sync_client() -> ChainflowDataGateway:
    return ChainflowDataGateway(base_url=BASE_URL, rest_api_key="test-key")


def _async_client() -> AsyncChainflowDataGateway:
    return AsyncChainflowDataGateway(base_url=BASE_URL, rest_api_key="test-key")


ENDPOINTS = [
    ("connect_price_feed", ("BTC-USD",), "/api/ws/BTC-USD"),
    ("connect_markets", (), "/api/ws/markets"),
    ("connect_explorer", (), "/api/ws/explorer"),
    ("connect_explorer_alias", (), "/api/explorer"),
]


@pytest.mark.parametrize("method,args,expected_path", ENDPOINTS)
def test_sync_endpoint_families_construct_and_prepare_path(method: str, args: Any, expected_path: str) -> None:
    manager = getattr(_sync_client().web_sockets, method)(*args)
    assert isinstance(manager, WebSocketsResourceConnectionManager)
    url = manager._prepare_url()
    assert url.scheme == "ws"
    assert url.path == expected_path


@pytest.mark.parametrize("method,args,expected_path", ENDPOINTS)
def test_async_endpoint_families_construct_and_prepare_path(method: str, args: Any, expected_path: str) -> None:
    manager = getattr(_async_client().web_sockets, method)(*args)
    assert isinstance(manager, AsyncWebSocketsResourceConnectionManager)
    url = manager._prepare_url()
    assert url.scheme == "ws"
    assert url.path == expected_path


def test_price_feed_requires_market() -> None:
    manager = _sync_client().web_sockets.connect_price_feed("")
    with pytest.raises(ValueError):
        manager._prepare_url()


def test_sync_manager_send_dict_queues_until_open(monkeypatch: pytest.MonkeyPatch) -> None:
    sock = RecordingSocket()
    manager = _sync_client().web_sockets.connect_markets()
    monkeypatch.setattr(manager, "_connect_ws", lambda q, h: sock)
    manager.send(EVENT)
    assert sock.sent == []
    conn = manager.enter()
    assert [json.loads(s) for s in sock.sent] == [EVENT]
    conn.close()


@pytest.mark.asyncio
async def test_async_manager_send_dict_queues_until_open(monkeypatch: pytest.MonkeyPatch) -> None:
    sock = AsyncRecordingSocket()
    manager = _async_client().web_sockets.connect_explorer()

    async def connect(q: Any, h: Any) -> AsyncRecordingSocket:
        return sock

    monkeypatch.setattr(manager, "_connect_ws", connect)
    manager.send(EVENT)
    assert sock.sent == []
    conn = await manager.enter()
    assert [json.loads(s) for s in sock.sent] == [EVENT]
    await conn.close()


def test_sync_connection_send_dict() -> None:
    sock = RecordingSocket()
    conn = WebSocketsResourceConnection(sock)  # type: ignore[arg-type]
    conn.send(EVENT)
    assert [json.loads(s) for s in sock.sent] == [EVENT]


@pytest.mark.asyncio
async def test_async_connection_send_dict() -> None:
    sock = AsyncRecordingSocket()
    conn = AsyncWebSocketsResourceConnection(sock)  # type: ignore[arg-type]
    await conn.send(EVENT)
    assert [json.loads(s) for s in sock.sent] == [EVENT]


FRAMES = [
    json.dumps({"type": "ticker", "price": "1"}).encode(),
    json.dumps({"type": "trade", "size": "2"}).encode(),
    json.dumps({"price": "3"}).encode(),
]


def test_sync_dispatch_dict_frames() -> None:
    conn = WebSocketsResourceConnection(RecordingSocket(FRAMES))  # type: ignore[arg-type]
    tickers: List[Any] = []
    events: List[Any] = []
    conn.on("ticker", tickers.append)
    conn.on("event", events.append)
    conn.dispatch_events()
    assert tickers == [{"type": "ticker", "price": "1"}]
    assert events == [json.loads(f) for f in FRAMES]


@pytest.mark.asyncio
async def test_async_dispatch_dict_frames() -> None:
    conn = AsyncWebSocketsResourceConnection(AsyncRecordingSocket(FRAMES))  # type: ignore[arg-type]
    tickers: List[Any] = []
    events: List[Any] = []

    async def on_ticker(event: Any) -> None:
        tickers.append(event)

    conn.on("ticker", on_ticker)
    conn.on("event", events.append)
    await conn.dispatch_events()
    assert tickers == [{"type": "ticker", "price": "1"}]
    assert events == [json.loads(f) for f in FRAMES]


def test_connection_keeps_empty_configured_queue() -> None:
    queue = SendQueue(max_bytes=16)
    assert WebSocketsResourceConnection(RecordingSocket(), send_queue=queue)._send_queue is queue  # type: ignore[arg-type]
    assert AsyncWebSocketsResourceConnection(AsyncRecordingSocket(), send_queue=queue)._send_queue is queue  # type: ignore[arg-type]


def test_sync_max_queue_size_governs_reconnect_buffering(monkeypatch: pytest.MonkeyPatch) -> None:
    manager = _sync_client().web_sockets.connect_markets(max_queue_size=16)
    monkeypatch.setattr(manager, "_connect_ws", lambda q, h: RecordingSocket())
    conn = manager.enter()
    conn._is_reconnecting = True
    with pytest.raises(WebSocketQueueFullError):
        conn.send_raw("x" * 17)
    conn.send_raw("x" * 16)
    assert conn._send_queue.drain() == ["x" * 16]


@pytest.mark.asyncio
async def test_async_max_queue_size_governs_reconnect_buffering(monkeypatch: pytest.MonkeyPatch) -> None:
    manager = _async_client().web_sockets.connect_explorer_alias(max_queue_size=16)

    async def connect(q: Any, h: Any) -> AsyncRecordingSocket:
        return AsyncRecordingSocket()

    monkeypatch.setattr(manager, "_connect_ws", connect)
    conn = await manager.enter()
    conn._is_reconnecting = True
    with pytest.raises(WebSocketQueueFullError):
        await conn.send_raw("x" * 17)
    await conn.send_raw("x" * 16)
    assert conn._send_queue.drain() == ["x" * 16]
