# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import Optional
from typing_extensions import Literal

from .._models import BaseModel

from .unix_milliseconds import UnixMilliseconds

__all__ = ["ExplorerHealth"]


class ExplorerHealth(BaseModel):
    status: Literal["ok", "degraded"]

    chain_id: Literal[125]

    latest_block: Optional[int] = None

    ts: UnixMilliseconds
    """Unix epoch milliseconds."""

    chain_connected: bool

    blocks_seen: int

    txs_seen: int
