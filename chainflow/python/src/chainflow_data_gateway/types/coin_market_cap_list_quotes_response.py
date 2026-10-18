# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict

from .._models import BaseModel

from .cmc_status import CmcStatus
from .cmc_asset import CmcAsset

__all__ = ["CoinMarketCapListQuotesResponse"]


class CoinMarketCapListQuotesResponse(BaseModel):
    status: CmcStatus

    data: Dict[str, CmcAsset]
