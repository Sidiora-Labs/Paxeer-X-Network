# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .block import Block

__all__ = ["ExplorerListBlocksResponse"]


class ExplorerListBlocksResponse(BaseModel):
    blocks: List[Block]
