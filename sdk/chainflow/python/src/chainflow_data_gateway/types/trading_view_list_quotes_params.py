# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import List
from typing_extensions import TypedDict

from .symbol import Symbol

__all__ = ["TradingViewListQuotesParams"]


class TradingViewListQuotesParams(TypedDict, total=False):
    symbols: List[Symbol]
    """Comma-separated symbols. Only the selected feed's symbol is returned; other symbols are silently omitted."""
