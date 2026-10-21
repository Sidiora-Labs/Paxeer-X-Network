# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .spot_trade import SpotTrade

__all__ = ["SpotTrades"]


class SpotTrades(BaseModel):
    symbol: str
    """Requested symbol, normalized to uppercase."""

    count: int

    trades: List[SpotTrade]
