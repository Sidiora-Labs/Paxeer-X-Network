# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .unix_milliseconds import UnixMilliseconds
from .totals import Totals

__all__ = ["MarketListTotalsResponse"]


class MarketListTotalsResponse(BaseModel):
    ts: UnixMilliseconds
    """Unix epoch milliseconds."""

    totals: Totals
    """Totals are sums of reported volume/OI."""
