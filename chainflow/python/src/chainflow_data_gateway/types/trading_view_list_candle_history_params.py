# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import Annotated, Literal, Required, TypedDict

from .._utils import PropertyInfo

from .symbol import Symbol

__all__ = ["TradingViewListCandleHistoryParams"]


class TradingViewListCandleHistoryParams(TypedDict, total=False):
    symbol: Required[Symbol]
    """Uppercase symbol matching the selected market."""

    resolution: Required[Literal["1", "2", "3", "5", "15", "30", "60", "240", "480", "D", "W", "1D", "1W"]]
    """UDF resolution: numeric strings are minutes; D/1D and W/1W are aliases."""

    from_: Required[Annotated[int, PropertyInfo(alias="from")]]
    """Inclusive lower bound in Unix seconds."""

    to: Required[int]
    """Inclusive upper bound in Unix seconds; must be >= from."""

    countback: int
    """Row limit within from/to. Values above 5000 are clamped to 5000, not rejected. Live-candle merging can add one extra row."""
