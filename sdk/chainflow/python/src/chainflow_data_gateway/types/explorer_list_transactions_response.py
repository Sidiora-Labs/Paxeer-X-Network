# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

from .transaction import Transaction

__all__ = ["ExplorerListTransactionsResponse"]


class ExplorerListTransactionsResponse(BaseModel):
    transactions: List[Transaction]
