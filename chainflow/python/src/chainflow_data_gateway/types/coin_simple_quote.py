# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional

from .._models import BaseModel

from .unix_seconds import UnixSeconds

__all__ = ["CoinSimpleQuote"]


class CoinSimpleQuote(BaseModel):
    usd: float

    usd_market_cap: Optional[float] = None

    usd_24h_vol: Optional[float] = None

    usd_24h_change: Optional[float] = None

    last_updated_at: Optional[UnixSeconds] = None
    """Unix epoch seconds, not milliseconds."""
