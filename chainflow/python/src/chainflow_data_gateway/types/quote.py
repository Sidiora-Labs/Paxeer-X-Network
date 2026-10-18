# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from .._models import BaseModel

from .symbol import Symbol
from .quote_values import QuoteValues

__all__ = ["Quote"]


class Quote(BaseModel):
    s: Literal["ok"]

    n: Symbol

    v: QuoteValues
