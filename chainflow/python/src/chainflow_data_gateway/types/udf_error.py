# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from .._models import BaseModel

__all__ = ["UdfError"]


class UdfError(BaseModel):
    s: Literal["error"]

    errmsg: str
