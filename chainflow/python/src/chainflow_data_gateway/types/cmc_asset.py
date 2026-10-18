# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict, List, Optional
from datetime import datetime

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .symbol import Symbol
from .coin_id import CoinID
from .iso_timestamp import IsoTimestamp
from .cmc_usd_quote import CmcUsdQuote

__all__ = ["CmcAsset", "Quote"]


class Quote(BaseModel):
    usd: CmcUsdQuote = FieldInfo(alias="USD")


class CmcAsset(BaseModel):
    id: Optional[int] = None
    """No official CMC ID is assigned; always null."""

    name: str

    symbol: Symbol

    slug: CoinID

    num_market_pairs: Optional[int] = None

    date_added: Optional[datetime] = None

    tags: List[str]

    max_supply: Optional[float] = None

    circulating_supply: Optional[float] = None

    total_supply: Optional[float] = None

    infinite_supply: Optional[bool] = None

    platform: Optional[Dict[str, object]] = None

    cmc_rank: Optional[int] = None

    self_reported_circulating_supply: Optional[float] = None

    self_reported_market_cap: Optional[float] = None

    last_updated: IsoTimestamp

    quote: Quote
