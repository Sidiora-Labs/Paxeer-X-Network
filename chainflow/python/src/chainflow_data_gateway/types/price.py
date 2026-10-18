# File generated from our OpenAPI spec by Scalar. See README.md for details.

from datetime import datetime

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["Price"]


class Price(BaseModel):
    symbol: Symbol

    price: float
    """Current feed close in USD."""

    timestamp: datetime
    """Start of the current/latest 1-minute candle, NOT the exact quote tick time."""

    open: float

    high: float

    low: float
