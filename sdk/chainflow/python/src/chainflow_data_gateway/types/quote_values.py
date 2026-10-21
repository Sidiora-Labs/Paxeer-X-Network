# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["QuoteValues"]


class QuoteValues(BaseModel):
    ch: float
    """Currently not a computed 24h change."""

    chp: float
    """Currently not a computed 24h change."""

    short_name: Symbol

    exchange: str

    description: str

    lp: float

    ask: float
    """Same as lp; not the orderbook best ask."""

    bid: float
    """Same as lp; not the orderbook best bid."""

    open_price: float

    high_price: float

    low_price: float

    prev_close_price: float
    """Current candle open, not previous daily close."""

    volume: float
    """Current candle volume."""
