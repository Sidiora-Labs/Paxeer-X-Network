# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Union
from typing_extensions import TypeAlias

from .candle_history import CandleHistory
from .history_no_data import HistoryNoData
from .udf_error import UdfError

__all__ = ["TradingViewListCandleHistoryResponse"]

TradingViewListCandleHistoryResponse: TypeAlias = Union[CandleHistory, HistoryNoData, UdfError]
