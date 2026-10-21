# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Literal, TypedDict

__all__ = ["CoinGeckoRetrieveParams"]


class CoinGeckoRetrieveParams(TypedDict, total=False):
    localization: bool
    """Only false is supported; true returns 400."""

    tickers: bool
    """Only false is supported; true returns 400."""

    community_data: bool
    """Only false is supported; true returns 400."""

    developer_data: bool
    """Only false is supported; true returns 400."""

    sparkline: bool
    """Only false is supported; true returns 400."""

    market_data: Literal[True]
    """Must be true."""
