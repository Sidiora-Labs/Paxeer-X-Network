# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Required, TypedDict

from .market import Market

__all__ = ["PriceFeedRetrieveRecentSpotTradesParams"]


class PriceFeedRetrieveRecentSpotTradesParams(TypedDict, total=False):
    market: Required[Market]
    """Lowercase feed identifier. This route addresses ONE feed, not all markets."""

    limit: int
    """Most recent trades, newest first."""
