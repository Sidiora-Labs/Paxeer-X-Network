# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict
from typing_extensions import TypeAlias

from .coin_simple_quote import CoinSimpleQuote

__all__ = ["CoinGeckoListSimplePricesResponse"]

CoinGeckoListSimplePricesResponse: TypeAlias = Dict[str, CoinSimpleQuote]
