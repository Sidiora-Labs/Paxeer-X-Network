# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import List
from typing_extensions import Literal, TypedDict

from .symbol import Symbol
from .coin_id import CoinID

__all__ = ["CoinMarketCapListQuotesParams"]


class CoinMarketCapListQuotesParams(TypedDict, total=False):
    convert: Literal["USD", "usd"]
    """Only USD is supported."""

    symbol: List[Symbol]
    """Comma-separated symbols; case-insensitive. Mutually exclusive with slug."""

    slug: List[CoinID]
    """Comma-separated local IDs. Mutually exclusive with symbol."""
