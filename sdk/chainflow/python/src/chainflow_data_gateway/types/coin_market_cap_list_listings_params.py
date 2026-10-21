# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Literal, TypedDict

__all__ = ["CoinMarketCapListListingsParams"]


class CoinMarketCapListListingsParams(TypedDict, total=False):
    convert: Literal["USD", "usd"]
    """Only USD is supported."""

    start: int
    """One-based offset."""

    limit: int
    """Page size; there are 18 local markets."""

    sort: Literal["market_cap", "price", "name", "symbol", "volume_24h", "percent_change_24h"]
    """Unavailable values sort last."""

    sort_dir: Literal["asc", "desc"]
    """Sort direction."""
