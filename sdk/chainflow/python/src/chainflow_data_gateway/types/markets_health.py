# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Dict
from typing_extensions import Literal

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .ws_runtime_stats import WsRuntimeStats

__all__ = ["MarketsHealth", "Poller"]


class Poller(BaseModel):
    poll_count: int = FieldInfo(alias="pollCount")

    error_counts: Dict[str, int] = FieldInfo(alias="errorCounts")


class MarketsHealth(BaseModel):
    ok: bool

    service: Literal["markets-aggregator"]

    symbols: int

    uptime_sec: int = FieldInfo(alias="uptimeSec")

    ws: WsRuntimeStats

    poller: Poller
