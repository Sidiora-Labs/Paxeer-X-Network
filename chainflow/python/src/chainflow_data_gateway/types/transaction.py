# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from .._models import BaseModel

from .iso_timestamp import IsoTimestamp
from .symbol import Symbol

__all__ = ["Transaction"]


class Transaction(BaseModel):
    """Block is the latest observed block or null. Prices are authoritative pricefeed quotes."""

    hash: str

    action: Literal[
        "OROB Fill", "Place Order", "Cancel Order", "Modify Order", "Oracle Updated", "Sealed Batch Executed"
    ]

    block: Optional[int] = None

    time: IsoTimestamp

    user: str
    """pax1."""

    symbol: Symbol

    side: Literal["buy", "sell"]

    price: float

    size: float
