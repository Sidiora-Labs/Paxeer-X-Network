# File generated from our OpenAPI spec by Scalar. See README.md for details.

import typing as _t

from . import types
from ._types import NOT_GIVEN, Omit, NoneType, NotGiven, Transport, ProxiesTypes, omit, not_given
from ._utils import file_from_path
from ._client import (
    Client,
    Stream,
    ChainflowDataGateway,
    Timeout,
    Transport,
    AsyncClient,
    AsyncStream,
    AsyncChainflowDataGateway,
    RequestOptions,
)
from ._models import BaseModel
from ._version import __title__, __version__
from ._response import APIResponse as APIResponse, AsyncAPIResponse as AsyncAPIResponse
from ._constants import DEFAULT_TIMEOUT, DEFAULT_MAX_RETRIES, DEFAULT_CONNECTION_LIMITS
from ._exceptions import (
    APIError,
    ChainflowDataGatewayError,
    ConflictError,
    NotFoundError,
    APIStatusError,
    RateLimitError,
    APITimeoutError,
    BadRequestError,
    APIConnectionError,
    AuthenticationError,
    InternalServerError,
    PermissionDeniedError,
    UnprocessableEntityError,
    WebSocketConnectionClosedError,
    WebSocketQueueFullError,
    APIResponseValidationError,
)
from ._base_client import DefaultHttpxClient, DefaultAioHttpClient, DefaultAsyncHttpxClient
from ._utils._logs import setup_logging as _setup_logging
from .types.websocket_reconnection import ReconnectingEvent, ReconnectingOverrides

__all__ = [
    "types",
    "__version__",
    "__title__",
    "NoneType",
    "Transport",
    "ProxiesTypes",
    "NotGiven",
    "NOT_GIVEN",
    "not_given",
    "Omit",
    "omit",
    "ChainflowDataGatewayError",
    "APIError",
    "APIStatusError",
    "APITimeoutError",
    "APIConnectionError",
    "APIResponseValidationError",
    "BadRequestError",
    "AuthenticationError",
    "PermissionDeniedError",
    "NotFoundError",
    "ConflictError",
    "UnprocessableEntityError",
    "RateLimitError",
    "InternalServerError",
    "Timeout",
    "RequestOptions",
    "Client",
    "AsyncClient",
    "Stream",
    "AsyncStream",
    "ChainflowDataGateway",
    "AsyncChainflowDataGateway",
    "file_from_path",
    "BaseModel",
    "DEFAULT_TIMEOUT",
    "DEFAULT_MAX_RETRIES",
    "DEFAULT_CONNECTION_LIMITS",
    "DefaultHttpxClient",
    "DefaultAsyncHttpxClient",
    "DefaultAioHttpClient",
    "ReconnectingEvent",
    "ReconnectingOverrides",
    "WebSocketQueueFullError",
    "WebSocketConnectionClosedError",
]

if not _t.TYPE_CHECKING:
    from ._utils._resources_proxy import resources as resources

_setup_logging()

__locals = locals()
__module_name = __name__
for __export_name in __all__:
    if not __export_name.startswith("__"):
        try:
            __locals[__export_name].__module__ = __module_name
        except (TypeError, AttributeError):
            pass
