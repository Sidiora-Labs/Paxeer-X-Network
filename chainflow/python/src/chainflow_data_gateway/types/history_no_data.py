# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .unix_seconds import UnixSeconds

__all__ = ["HistoryNoData"]


class HistoryNoData(BaseModel):
    """nextTime, when present, is the last candle strictly before from; it is not a forward page cursor."""

    s: Literal["no_data"]

    next_time: Optional[UnixSeconds] = FieldInfo(alias="nextTime", default=None)
    """Unix epoch seconds, not milliseconds."""
