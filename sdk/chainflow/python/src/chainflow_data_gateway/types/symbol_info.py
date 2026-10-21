# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["SymbolInfo"]


class SymbolInfo(BaseModel):
    name: Symbol

    ticker: Symbol

    description: str

    type: str

    session: str

    exchange: str

    listed_exchange: str

    timezone: str

    format: str

    pricescale: int

    minmov: int

    has_intraday: bool

    has_daily: bool

    has_weekly_and_monthly: bool

    supported_resolutions: List[str]

    volume_precision: int

    data_status: str

    currency_code: str
