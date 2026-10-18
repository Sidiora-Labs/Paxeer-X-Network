# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List, Union
from typing_extensions import Literal, TypeAlias

from .._models import BaseModel

from .quote import Quote
from .missing_quote import MissingQuote

__all__ = ["Quotes", "D"]


D: TypeAlias = Union[Quote, MissingQuote]


class Quotes(BaseModel):
    s: Literal["ok"]

    d: List[D]
