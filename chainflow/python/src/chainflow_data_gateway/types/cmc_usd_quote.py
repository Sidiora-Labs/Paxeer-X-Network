# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional

from .._models import BaseModel

from .iso_timestamp import IsoTimestamp

__all__ = ["CmcUsdQuote"]


class CmcUsdQuote(BaseModel):
    price: float

    volume_24h: Optional[float] = None

    volume_change_24h: Optional[float] = None

    percent_change_1h: Optional[float] = None

    percent_change_24h: Optional[float] = None

    percent_change_7d: Optional[float] = None

    percent_change_30d: Optional[float] = None

    percent_change_60d: Optional[float] = None

    percent_change_90d: Optional[float] = None

    market_cap: Optional[float] = None

    market_cap_dominance: Optional[float] = None

    fully_diluted_market_cap: Optional[float] = None

    tvl: Optional[float] = None

    last_updated: IsoTimestamp
