# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List
from typing_extensions import TypeAlias

from .symbol_search_result import SymbolSearchResult

__all__ = ["TradingViewSearchResponse"]

TradingViewSearchResponse: TypeAlias = List[SymbolSearchResult]
