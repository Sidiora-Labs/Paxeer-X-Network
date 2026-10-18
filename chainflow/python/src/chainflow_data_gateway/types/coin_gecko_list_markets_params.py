# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import List
from typing_extensions import Literal, TypedDict

from .coin_id import CoinID
from .market import Market

__all__ = ["CoinGeckoListMarketsParams"]


class CoinGeckoListMarketsParams(TypedDict, total=False):
    vs_currency: Literal["usd", "USD"]
    """Only USD is supported."""

    ids: List[CoinID]
    """Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols."""

    symbols: List[Market]
    """Comma-separated symbols; case-insensitive. Mutually exclusive with ids."""

    page: int
    """One-based page."""

    per_page: int
    """Page size."""

    order: Literal["market_cap_asc", "market_cap_desc", "volume_asc", "volume_desc", "id_asc", "id_desc"]
    """Unavailable values sort last; ID breaks ties."""

    sparkline: bool
    """Only false is supported; true returns 400."""

    price_change_percentage: List[Literal["1h", "24h"]]
    """Optional extra percentage-change fields."""

    precision: str
    """full or decimal count 0..18."""

    locale: Literal["en"]
    """Only English metadata is supported."""
