# File generated from our OpenAPI spec by Scalar. See README.md for details.

from .._models import BaseModel

from .unix_milliseconds import UnixMilliseconds

__all__ = ["Block"]


class Block(BaseModel):
    """Block from EVM chain 125. Newest-first in snapshots."""

    block_number: int

    time: UnixMilliseconds
    """Unix epoch milliseconds."""

    proposer: str
    """EVM miner/proposer address."""

    hash: str

    transactions: int
    """On-chain transaction count."""
