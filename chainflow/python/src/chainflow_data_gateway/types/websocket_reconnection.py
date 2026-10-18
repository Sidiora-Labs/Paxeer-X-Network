# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import dataclasses
from typing_extensions import TypedDict

from .._types import Query, Headers


@dataclasses.dataclass(frozen=True)
class ReconnectingEvent:
    """Information about a reconnection attempt passed to `on_reconnecting`."""

    attempt: int
    max_attempts: int
    delay: float
    close_code: int
    extra_query: Query
    extra_headers: Headers


class ReconnectingOverrides(TypedDict, total=False):
    """Optional overrides for the next WebSocket reconnection attempt."""

    extra_query: Query
    extra_headers: Headers
    abort: bool


_RECOVERABLE_CLOSE_CODES: frozenset[int] = frozenset({1001, 1005, 1006, 1011, 1012, 1013, 1015})


def is_recoverable_close(code: int) -> bool:
    """Return `True` if the WebSocket close code is worth retrying."""
    return code in _RECOVERABLE_CLOSE_CODES
