# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .perp_trade import PerpTrade

__all__ = ["PerpTrades"]


class PerpTrades(BaseModel):
    symbol: str

    count: int

    trades: List[PerpTrade]
