# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from .._models import BaseModel

from .symbol import Symbol
from .unix_milliseconds import UnixMilliseconds

__all__ = ["SpotTrade"]


class SpotTrade(BaseModel):
    """orderbook-crossing trade."""

    id: Optional[str] = None
    """."""

    symbol: Symbol

    side: Literal["BUY", "SELL"]

    price: float

    size: float

    trade_time: UnixMilliseconds
    """Unix epoch milliseconds."""
