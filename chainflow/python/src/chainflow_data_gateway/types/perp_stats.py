# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["PerpStats"]


class PerpStats(BaseModel):
    """perpetual-market statistics. basis_bps is basis points; funding rates are fractions. liq_volume_24h, oi_usd and volume_24h_usd are USD notionals."""

    symbol: Symbol

    mark_price: float

    index_price: float

    basis_bps: float

    funding_rate: float

    last_funding_rate: float

    long_short_ratio: float

    liq_volume_24h: float

    oi_contracts: float

    oi_usd: float

    volume_24h_contracts: float

    volume_24h_usd: float

    next_funding_ms: int
    """Remaining milliseconds until funding; a duration, NOT an epoch timestamp."""
