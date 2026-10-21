# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing import Optional
from datetime import datetime

from .._models import BaseModel

from .coin_id import CoinID
from .market import Market
from .iso_timestamp import IsoTimestamp

__all__ = [
    "CoinDetail",
    "Image",
    "MarketData",
    "MarketDataCurrentPrice",
    "MarketDataMarketCap",
    "MarketDataFullyDilutedValuation",
    "MarketDataTotalVolume",
    "MarketDataHigh24h",
    "MarketDataLow24h",
    "MarketDataAth",
    "MarketDataAthChangePercentage",
    "MarketDataAthDate",
    "MarketDataAtl",
    "MarketDataAtlChangePercentage",
    "MarketDataAtlDate",
]


class MarketDataAtlDate(BaseModel):
    usd: Optional[datetime] = None


class MarketDataAtlChangePercentage(BaseModel):
    usd: Optional[float] = None


class MarketDataAtl(BaseModel):
    usd: Optional[float] = None


class MarketDataAthDate(BaseModel):
    usd: Optional[datetime] = None


class MarketDataAthChangePercentage(BaseModel):
    usd: Optional[float] = None


class MarketDataAth(BaseModel):
    usd: Optional[float] = None


class MarketDataLow24h(BaseModel):
    usd: Optional[float] = None


class MarketDataHigh24h(BaseModel):
    usd: Optional[float] = None


class MarketDataTotalVolume(BaseModel):
    usd: Optional[float] = None


class MarketDataFullyDilutedValuation(BaseModel):
    usd: Optional[float] = None


class MarketDataMarketCap(BaseModel):
    usd: Optional[float] = None


class MarketDataCurrentPrice(BaseModel):
    usd: float


class MarketData(BaseModel):
    current_price: MarketDataCurrentPrice

    market_cap: MarketDataMarketCap

    fully_diluted_valuation: MarketDataFullyDilutedValuation

    total_volume: MarketDataTotalVolume

    high_24h: MarketDataHigh24h

    low_24h: MarketDataLow24h

    ath: MarketDataAth

    ath_change_percentage: MarketDataAthChangePercentage

    ath_date: MarketDataAthDate

    atl: MarketDataAtl

    atl_change_percentage: MarketDataAtlChangePercentage

    atl_date: MarketDataAtlDate

    market_cap_rank: Optional[float] = None

    price_change_24h: Optional[float] = None

    price_change_percentage_24h: Optional[float] = None

    market_cap_change_24h: Optional[float] = None

    market_cap_change_percentage_24h: Optional[float] = None

    circulating_supply: Optional[float] = None

    total_supply: Optional[float] = None

    max_supply: Optional[float] = None

    last_updated: IsoTimestamp


class Image(BaseModel):
    thumb: Optional[str] = None

    small: Optional[str] = None

    large: Optional[str] = None


class CoinDetail(BaseModel):
    id: CoinID

    symbol: Market

    name: str

    image: Image

    market_cap_rank: Optional[int] = None

    market_data: MarketData

    last_updated: IsoTimestamp
