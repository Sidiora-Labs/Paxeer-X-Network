# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from .._models import BaseModel

from .symbol import Symbol
from .unix_milliseconds import UnixMilliseconds

__all__ = ["PerpTrade"]


class PerpTrade(BaseModel):
    id: Optional[str] = None
    """Optional Dragonfly stream ID."""

    symbol: Symbol

    side: Literal["BUY", "SELL"]

    price: float

    contracts: float

    notional: float
    """USD notional."""

    size: Optional[float] = None
    """Optional alias for contracts."""

    liquidation: bool

    trade_time: UnixMilliseconds
    """Unix epoch milliseconds."""
