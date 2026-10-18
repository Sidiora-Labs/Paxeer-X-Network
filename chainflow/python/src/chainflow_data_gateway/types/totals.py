# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

__all__ = ["Totals"]


class Totals(BaseModel):
    """Totals are sums of reported volume/OI."""

    total_trades: float

    volume_24h: float

    volume_7d: float

    volume_30d: float

    total_oi: float
