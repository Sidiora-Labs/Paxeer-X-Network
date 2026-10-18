# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import TYPE_CHECKING
from typing_extensions import Sequence, TypedDict

if TYPE_CHECKING:
    from websockets import Subprotocol
    from websockets.extensions import ClientExtensionFactory


class WebSocketConnectionOptions(TypedDict, total=False):
    """WebSocket connection options forwarded to `websockets.connect`."""

    extensions: Sequence[ClientExtensionFactory] | None
    subprotocols: Sequence[Subprotocol] | None
    compression: str | None
    max_size: int | None
    max_queue: int | None | tuple[int | None, int | None]
    write_limit: int | tuple[int, int | None]
