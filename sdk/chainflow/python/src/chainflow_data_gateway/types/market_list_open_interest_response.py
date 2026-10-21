# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import Literal

from .._models import BaseModel

from .open_interest import OpenInterest

__all__ = ["MarketListOpenInterestResponse"]


class MarketListOpenInterestResponse(BaseModel):
    success: Literal[True]

    data: List[OpenInterest]
