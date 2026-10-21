# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .unix_milliseconds import UnixMilliseconds
from .market_record import MarketRecord

__all__ = ["MarketRetrieveResponse"]


class MarketRetrieveResponse(BaseModel):
    ts: UnixMilliseconds
    """Unix epoch milliseconds."""

    market: MarketRecord
    """Display view: volume_24h, open_interest, volume_7d, volume_30d use operator-configured display divisors; reported_volume_24h and reported_open_interest preserve unscaled service values."""
