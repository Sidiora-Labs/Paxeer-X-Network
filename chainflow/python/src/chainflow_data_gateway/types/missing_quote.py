# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["MissingQuote"]


class MissingQuote(BaseModel):
    s: Literal["error"]

    n: Symbol

    v: object
