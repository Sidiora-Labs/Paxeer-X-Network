# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .cmc_status import CmcStatus
from .cmc_asset import CmcAsset

__all__ = ["CoinMarketCapListListingsResponse"]


class CoinMarketCapListListingsResponse(BaseModel):
    status: CmcStatus

    data: List[CmcAsset]
