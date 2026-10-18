# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Union
from typing_extensions import TypeAlias

from .price import Price
from .error import Error

__all__ = ["PriceFeedListResponse"]

PriceFeedListResponse: TypeAlias = Union[Price, Error]
