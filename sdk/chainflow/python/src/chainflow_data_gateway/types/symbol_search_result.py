# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["SymbolSearchResult"]


class SymbolSearchResult(BaseModel):
    symbol: Symbol

    full_name: str

    description: str

    exchange: str

    ticker: Symbol

    type: str
