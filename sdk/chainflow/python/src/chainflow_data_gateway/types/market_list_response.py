# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .unix_milliseconds import UnixMilliseconds
from .market_record import MarketRecord

__all__ = ["MarketListResponse"]


class MarketListResponse(BaseModel):
    ts: UnixMilliseconds
    """Unix epoch milliseconds."""

    markets: List[MarketRecord]
