# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing_extensions import Literal

from pydantic import Field as FieldInfo

from .._models import BaseModel

from .ws_scope import WsScope
from .iso_timestamp import IsoTimestamp

__all__ = ["WsToken"]


class WsToken(BaseModel):
    token: str
    """Opaque signed credential; treat as secret. Not an OAuth token or a JWT."""

    symbol: WsScope

    expires_at: IsoTimestamp = FieldInfo(alias="expiresAt")

    expires_in: Literal[300] = FieldInfo(alias="expiresIn")
