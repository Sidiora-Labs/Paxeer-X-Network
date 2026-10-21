# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import TypeAlias

from .coin_list_item import CoinListItem

__all__ = ["CoinGeckoListResponse"]

CoinGeckoListResponse: TypeAlias = List[CoinListItem]
