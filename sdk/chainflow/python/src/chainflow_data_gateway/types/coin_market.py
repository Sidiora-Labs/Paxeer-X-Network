# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict, Optional
from datetime import datetime

from .._models import BaseModel

from .coin_id import CoinID
from .market import Market
from .iso_timestamp import IsoTimestamp

__all__ = ["CoinMarket"]


class CoinMarket(BaseModel):
    """CoinGecko-compatible local-feed metadata."""

    id: CoinID

    symbol: Market

    name: str

    image: Optional[str] = None

    current_price: float

    market_cap: Optional[float] = None

    market_cap_rank: Optional[float] = None

    fully_diluted_valuation: Optional[float] = None

    total_volume: Optional[float] = None

    high_24h: Optional[float] = None

    low_24h: Optional[float] = None

    price_change_24h: Optional[float] = None

    price_change_percentage_24h: Optional[float] = None

    market_cap_change_24h: Optional[float] = None

    market_cap_change_percentage_24h: Optional[float] = None

    circulating_supply: Optional[float] = None

    total_supply: Optional[float] = None

    max_supply: Optional[float] = None

    ath: Optional[float] = None

    ath_change_percentage: Optional[float] = None

    atl: Optional[float] = None

    atl_change_percentage: Optional[float] = None

    ath_date: Optional[datetime] = None

    atl_date: Optional[datetime] = None

    roi: Optional[Dict[str, object]] = None

    last_updated: IsoTimestamp

    price_change_percentage_1h_in_currency: Optional[float] = None

    price_change_percentage_24h_in_currency: Optional[float] = None
