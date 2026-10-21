# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Required, TypedDict

from .ws_scope import WsScope

__all__ = ["GatewayCreateWebSocketTokenParams"]


class GatewayCreateWebSocketTokenParams(TypedDict, total=False):
    symbol: Required[WsScope]
