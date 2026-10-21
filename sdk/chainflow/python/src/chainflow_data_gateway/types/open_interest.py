# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["OpenInterest"]


class OpenInterest(BaseModel):
    """Estimated USD split of perpetual open interest using the verified account-level exposure."""

    paxeer_symbol: Symbol = FieldInfo(alias="paxeerSymbol")

    long_oi: float

    short_oi: float

    source: Literal["feed"]

    estimated_split: Literal[True]
