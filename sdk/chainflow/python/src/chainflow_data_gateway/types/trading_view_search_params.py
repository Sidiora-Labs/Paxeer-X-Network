# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import TypedDict

__all__ = ["TradingViewSearchParams"]


class TradingViewSearchParams(TypedDict, total=False):
    query: str
    """Case-insensitive substring of symbol or full description."""

    limit: int
    """Maximum results; one feed exposes at most one symbol."""
