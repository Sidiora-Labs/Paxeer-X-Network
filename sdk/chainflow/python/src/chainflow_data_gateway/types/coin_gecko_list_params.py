# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import TypedDict

__all__ = ["CoinGeckoListParams"]


class CoinGeckoListParams(TypedDict, total=False):
    include_platform: bool
    """Only false is supported; true returns 400."""
