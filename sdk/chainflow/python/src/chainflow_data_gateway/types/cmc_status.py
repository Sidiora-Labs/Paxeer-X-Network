# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from .._models import BaseModel

from .iso_timestamp import IsoTimestamp

__all__ = ["CmcStatus"]


class CmcStatus(BaseModel):
    timestamp: IsoTimestamp

    error_code: int

    error_message: Optional[str] = None

    elapsed: int
    """Elapsed handler time in milliseconds."""

    credit_count: Literal[0]

    notice: Optional[str] = None
