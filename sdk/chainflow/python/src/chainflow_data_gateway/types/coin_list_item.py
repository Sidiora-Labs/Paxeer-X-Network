# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .coin_id import CoinID
from .market import Market

__all__ = ["CoinListItem"]


class CoinListItem(BaseModel):
    id: CoinID

    symbol: Market

    name: str
