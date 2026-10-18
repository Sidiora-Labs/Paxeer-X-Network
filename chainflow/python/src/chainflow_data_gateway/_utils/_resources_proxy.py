from __future__ import annotations

from typing import Any
from typing_extensions import override

from ._proxy import LazyProxy


class ResourcesProxy(LazyProxy[Any]):
    """A proxy for the `chainflow_data_gateway.resources` module.

    This is used so that we can lazily import `chainflow_data_gateway.resources` only when
    needed *and* so that users can just import `chainflow_data_gateway` and reference `chainflow_data_gateway.resources`
    """

    @override
    def __load__(self) -> Any:
        import importlib

        mod = importlib.import_module("chainflow_data_gateway.resources")
        return mod


resources = ResourcesProxy().__as_proxied__()
