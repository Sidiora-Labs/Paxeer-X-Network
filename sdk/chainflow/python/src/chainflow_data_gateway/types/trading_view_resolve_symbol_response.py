# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Union
from typing_extensions import TypeAlias

from .symbol_info import SymbolInfo
from .udf_error import UdfError

__all__ = ["TradingViewResolveSymbolResponse"]

TradingViewResolveSymbolResponse: TypeAlias = Union[SymbolInfo, UdfError]
