# File generated from our OpenAPI spec by Scalar. See README.md for details.

from typing import List

from .._models import BaseModel

__all__ = ["UdfConfig", "Exchange", "SymbolsType"]


class SymbolsType(BaseModel):
    name: str

    value: str


class Exchange(BaseModel):
    value: str

    name: str

    desc: str


class UdfConfig(BaseModel):
    supports_search: bool

    supports_group_request: bool

    supports_marks: bool

    supports_timescale_marks: bool

    supports_time: bool

    exchanges: List[Exchange]

    symbols_types: List[SymbolsType]

    supported_resolutions: List[str]

    currency_codes: List[str]
