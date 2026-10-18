# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Required, TypedDict

from .symbol import Symbol

__all__ = ["TradingViewResolveSymbolParams"]


class TradingViewResolveSymbolParams(TypedDict, total=False):
    symbol: Required[Symbol]
    """Uppercase symbol matching the selected market."""
