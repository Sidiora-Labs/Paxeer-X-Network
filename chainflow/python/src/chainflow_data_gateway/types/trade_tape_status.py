# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict, List

from pydantic import Field as FieldInfo

from .._models import BaseModel

__all__ = ["TradeTapeStatus"]


class TradeTapeStatus(BaseModel):
    configured: bool

    connected: bool

    ring_symbols: List[str] = FieldInfo(alias="ringSymbols")

    ring_sizes: Dict[str, int] = FieldInfo(alias="ringSizes")
