# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import TypeAlias

from .coin_market import CoinMarket

__all__ = ["CoinGeckoListMarketsResponse"]

CoinGeckoListMarketsResponse: TypeAlias = List[CoinMarket]
