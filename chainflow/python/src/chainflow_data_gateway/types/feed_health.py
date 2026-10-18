# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from datetime import datetime
from typing_extensions import Literal

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["FeedHealth"]


class FeedHealth(BaseModel):
    status: Literal["ok"]

    backfill: Optional[str] = None
    """Stored backfill status; null if not set."""

    last_candle: Optional[datetime] = FieldInfo(alias="lastCandle", default=None)

    symbol: Symbol
