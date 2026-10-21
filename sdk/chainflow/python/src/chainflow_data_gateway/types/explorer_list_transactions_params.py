# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

from typing_extensions import TypedDict

__all__ = ["ExplorerListTransactionsParams"]


class ExplorerListTransactionsParams(TypedDict, total=False):
    limit: int
    """Newest-first count. Numeric values are clamped to 1..200."""
