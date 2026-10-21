# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .symbol import Symbol

__all__ = ["Ticker"]


class Ticker(BaseModel):
    symbol: str
    """PERP_<SYMBOL>_USDC."""

    paxeer_symbol: Symbol = FieldInfo(alias="paxeerSymbol")

    mark_price: float

    index_price: float

    open_interest: float

    est_funding_rate: float

    last_funding_rate: float

    field_24h_open: Optional[float] = FieldInfo(alias="24h_open", default=None)

    field_24h_close: float = FieldInfo(alias="24h_close")

    field_24h_high: Optional[float] = FieldInfo(alias="24h_high", default=None)

    field_24h_low: Optional[float] = FieldInfo(alias="24h_low", default=None)

    field_24h_amount: Optional[float] = FieldInfo(alias="24h_amount", default=None)

    field_24h_volume: float = FieldInfo(alias="24h_volume")
    """Unscaled reported volume."""

    updated_at: Optional[int] = None
    """Price ingestion time in Unix milliseconds."""
