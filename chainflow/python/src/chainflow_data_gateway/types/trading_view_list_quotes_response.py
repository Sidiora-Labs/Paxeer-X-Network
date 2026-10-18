# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Union
from typing_extensions import TypeAlias

from .quotes import Quotes
from .udf_error import UdfError

__all__ = ["TradingViewListQuotesResponse"]

TradingViewListQuotesResponse: TypeAlias = Union[Quotes, UdfError]
