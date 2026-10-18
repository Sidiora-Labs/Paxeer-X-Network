# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import List
from typing_extensions import Literal, TypedDict

from .coin_id import CoinID
from .market import Market

__all__ = ["CoinGeckoListSimplePricesParams"]


class CoinGeckoListSimplePricesParams(TypedDict, total=False):
    ids: List[CoinID]
    """Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols."""

    symbols: List[Market]
    """Comma-separated symbols; case-insensitive. Mutually exclusive with ids."""

    vs_currencies: Literal["usd", "USD"]
    """Only one currency, USD."""

    include_market_cap: bool
    """Include the corresponding optional response field."""

    include_24hr_vol: bool
    """Include the corresponding optional response field."""

    include_24hr_change: bool
    """Include the corresponding optional response field."""

    include_last_updated_at: bool
    """Include the corresponding optional response field."""
