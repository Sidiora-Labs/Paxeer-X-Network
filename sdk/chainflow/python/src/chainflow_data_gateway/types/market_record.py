# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional

from .._models import BaseModel

from .symbol import Symbol
from .unix_milliseconds import UnixMilliseconds

__all__ = ["MarketRecord"]


class MarketRecord(BaseModel):
    """Display view: volume_24h, open_interest, volume_7d, volume_30d use operator-configured display divisors; reported_volume_24h and reported_open_interest preserve unscaled service values."""

    symbol: Symbol

    symbol_full: str

    price: float

    change_24h: float

    volume_24h: float

    open_interest: float

    funding_8h: float

    est_funding_rate: float

    volume_7d: float

    volume_30d: float

    trades_count: float

    perp_mark_price: Optional[float] = None

    perp_index_price: Optional[float] = None

    perp_basis_bps: Optional[float] = None

    perp_funding_rate: Optional[float] = None

    perp_next_funding_ms: Optional[float] = None

    perp_long_short_ratio: Optional[float] = None

    perp_liq_volume_24h: Optional[float] = None

    perp_oi_contracts: Optional[float] = None

    perp_oi_usd: Optional[float] = None

    perp_volume_24h_contracts: Optional[float] = None

    perp_volume_24h_usd: Optional[float] = None

    reported_volume_24h: float

    reported_open_interest: float

    last_updated: UnixMilliseconds
    """Unix epoch milliseconds."""

    price_updated_at: Optional[UnixMilliseconds] = None
    """Unix epoch milliseconds."""

    price_24h: Optional[float] = None

    high_24h: Optional[float] = None

    low_24h: Optional[float] = None

    amount_24h: Optional[float] = None
