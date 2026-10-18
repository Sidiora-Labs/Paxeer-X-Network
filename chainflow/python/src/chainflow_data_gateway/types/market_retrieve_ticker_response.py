# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from .._models import BaseModel

from .ticker import Ticker

__all__ = ["MarketRetrieveTickerResponse"]


class MarketRetrieveTickerResponse(BaseModel):
    success: Literal[True]

    data: Ticker
