# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["Stats"]


class Stats(BaseModel):
    symbol: Symbol

    volume_24h: float
    """Rolling USD volume from trade tape."""

    open_interest: float
    """USD open interest."""

    est_funding_rate: float
    """Fractional rate, e.g. 0.0001 means 0.01%."""

    last_funding_rate: float
    """Last 8-hour settlement rate, as a fraction."""
