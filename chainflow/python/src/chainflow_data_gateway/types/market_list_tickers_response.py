# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import Literal

from .._models import BaseModel

from .ticker import Ticker

__all__ = ["MarketListTickersResponse"]


class MarketListTickersResponse(BaseModel):
    success: Literal[True]

    data: List[Ticker]
