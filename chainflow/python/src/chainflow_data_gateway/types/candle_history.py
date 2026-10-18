# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import Literal

from .._models import BaseModel

from .unix_seconds import UnixSeconds

__all__ = ["CandleHistory"]


class CandleHistory(BaseModel):
    """Parallel arrays in ascending timestamp order; all arrays have the same length. OHLC is USD."""

    s: Literal["ok"]

    t: List[UnixSeconds]

    o: List[float]

    h: List[float]

    l: List[float]

    c: List[float]

    v: List[float]
