# File generated from our OpenAPI spec by Scalar. See README.md for details.

from __future__ import annotations

import os
import threading
from typing import TYPE_CHECKING, Any, Mapping
from typing_extensions import Self, override

import httpx

from . import _exceptions
from ._qs import Querystring
from ._types import (
    Omit,
    Headers,
    Timeout,
    NotGiven,
    Transport,
    ProxiesTypes,
    RequestOptions,
    not_given,
)
from ._utils import is_given, is_mapping_t, get_async_library
from ._compat import cached_property
from ._exceptions import APIStatusError, ChainflowDataGatewayError
from ._base_client import (
    DEFAULT_MAX_RETRIES,
    SyncAPIClient,
    AsyncAPIClient,
)
from ._streaming import Stream as Stream, AsyncStream as AsyncStream
from ._version import __version__

if TYPE_CHECKING:
    from .resources import (
        gateway,
        price_feeds,
        trading_view,
        markets,
        explorer,
        coin_gecko,
        coin_market_cap,
        web_sockets,
    )
    from .resources.gateway import GatewayResource, AsyncGatewayResource
    from .resources.price_feeds import PriceFeedsResource, AsyncPriceFeedsResource
    from .resources.trading_view import TradingViewResource, AsyncTradingViewResource
    from .resources.markets import MarketsResource, AsyncMarketsResource
    from .resources.explorer import ExplorerResource, AsyncExplorerResource
    from .resources.coin_gecko import CoinGeckoResource, AsyncCoinGeckoResource
    from .resources.coin_market_cap import CoinMarketCapResource, AsyncCoinMarketCapResource
    from .resources.web_sockets import WebSocketsResource, AsyncWebSocketsResource

# Serializes lazy resource imports so concurrent cold access from multiple
# threads cannot deadlock on CPython import locks (see CPython 3.14).
_RESOURCE_IMPORT_LOCK = threading.RLock()

__all__ = [
    "ChainflowDataGateway",
    "AsyncChainflowDataGateway",
    "Client",
    "AsyncClient",
    "Timeout",
    "Transport",
    "ProxiesTypes",
    "RequestOptions",
]


class ChainflowDataGateway(SyncAPIClient):
    # client options
    rest_api_key: str
    ws_mint_key: str | None
    ws_token: str | None

    websocket_base_url: str | httpx.URL | None
    """Base URL for WebSocket connections.

    If not specified, the default base URL will be used, with 'wss://' replacing the
    'http://' or 'https://' scheme. For example: 'http://example.com' becomes
    'wss://example.com'
    """

    def __init__(
        self,
        *,
        rest_api_key: str | None = None,
        ws_mint_key: str | None = None,
        ws_token: str | None = None,
        base_url: str | httpx.URL | None = None,
        websocket_base_url: str | httpx.URL | None = None,
        timeout: float | Timeout | None | NotGiven = not_given,
        max_retries: int = DEFAULT_MAX_RETRIES,
        default_headers: Mapping[str, str] | None = None,
        default_query: Mapping[str, object] | None = None,
        # Configure a custom httpx client.
        # We provide a `DefaultHttpxClient` class that you can pass to retain the default values we use for `limits`, `timeout` & `follow_redirects`.
        # See the [httpx documentation](https://www.python-httpx.org/api/#client) for more details.
        http_client: httpx.Client | None = None,
        # Enable or disable schema validation for data returned by the API.
        # When enabled an error APIResponseValidationError is raised
        # if the API responds with invalid data for the expected schema.
        #
        # This parameter may be removed or changed in the future.
        # If you rely on this feature, please open a GitHub issue
        # outlining your use-case to help us decide if it should be
        # part of our public interface in the future.
        _strict_response_validation: bool = False,
    ) -> None:
        """Construct a new synchronous ChainflowDataGateway client instance.

        This automatically infers the following arguments from their corresponding environment variables if they are not provided:
        - `rest_api_key` from `REST_API_KEY`
        - `ws_mint_key` from `WS_MINT_KEY`
        - `ws_token` from `WS_TOKEN`
        """
        if rest_api_key is None:
            rest_api_key = os.environ.get("REST_API_KEY")
        if rest_api_key is None:
            raise ChainflowDataGatewayError(
                "The rest_api_key client option must be set either by passing rest_api_key to the client or by setting the REST_API_KEY environment variable"
            )
        self.rest_api_key = rest_api_key
        if ws_mint_key is None:
            ws_mint_key = os.environ.get("WS_MINT_KEY")
        self.ws_mint_key = ws_mint_key
        if ws_token is None:
            ws_token = os.environ.get("WS_TOKEN")
        self.ws_token = ws_token
        self.websocket_base_url = websocket_base_url
        if base_url is None:
            base_url = os.environ.get("SIDIORALABS_BASE_URL")
        if base_url is None:
            base_url = "https://data.feed.chainflowtrading.com"
        custom_headers_env = os.environ.get("SIDIORALABS_CUSTOM_HEADERS")
        if custom_headers_env is not None:
            parsed: dict[str, str] = {}
            for line in custom_headers_env.split("\n"):
                colon = line.find(":")
                if colon >= 0:
                    parsed[line[:colon].strip()] = line[colon + 1 :].strip()
            default_headers = {**parsed, **(default_headers if is_mapping_t(default_headers) else {})}
        super().__init__(
            version=__version__,
            base_url=base_url,
            max_retries=max_retries,
            timeout=timeout,
            http_client=http_client,
            custom_headers=default_headers,
            custom_query=default_query,
            _strict_response_validation=_strict_response_validation,
        )
        self._idempotency_header = None
        self._default_stream_cls = Stream

    @cached_property
    def gateway(self) -> "GatewayResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import GatewayResource
        return GatewayResource(self)

    @cached_property
    def price_feeds(self) -> "PriceFeedsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import PriceFeedsResource
        return PriceFeedsResource(self)

    @cached_property
    def trading_view(self) -> "TradingViewResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import TradingViewResource
        return TradingViewResource(self)

    @cached_property
    def markets(self) -> "MarketsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import MarketsResource
        return MarketsResource(self)

    @cached_property
    def explorer(self) -> "ExplorerResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import ExplorerResource
        return ExplorerResource(self)

    @cached_property
    def coin_gecko(self) -> "CoinGeckoResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import CoinGeckoResource
        return CoinGeckoResource(self)

    @cached_property
    def coin_market_cap(self) -> "CoinMarketCapResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import CoinMarketCapResource
        return CoinMarketCapResource(self)

    @cached_property
    def web_sockets(self) -> "WebSocketsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import WebSocketsResource
        return WebSocketsResource(self)

    @cached_property
    def with_raw_response(self) -> ChainflowDataGatewayWithRawResponse:
        return ChainflowDataGatewayWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> ChainflowDataGatewayWithStreamedResponse:
        return ChainflowDataGatewayWithStreamedResponse(self)

    @property
    @override
    def qs(self) -> Querystring:
        return Querystring(array_format="comma")

    @property
    @override
    def auth_headers(self) -> dict[str, str]:
        return {
            **self._rest_api_key_header_auth,
            **self._ws_mint_key_header_auth,
        }

    @override
    def _auth_query(self, security: dict[str, bool]) -> dict[str, str]:
        _ = security
        return {
            **self._ws_token_query_auth,
        }

    @override
    def _auth_cookies(self, security: dict[str, bool]) -> dict[str, str]:
        _ = security
        return {}

    @property
    def _rest_api_key_header_auth(self) -> dict[str, str]:
        value = self.rest_api_key
        if value is None:
            return {}
        return {"X-API-Key": value}

    @property
    def _ws_mint_key_header_auth(self) -> dict[str, str]:
        value = self.ws_mint_key
        if value is None:
            return {}
        return {"X-API-Key": value}

    @property
    def _ws_token_query_auth(self) -> dict[str, str]:
        value = self.ws_token
        if value is None:
            return {}
        return {"token": value}

    @property
    @override
    def default_headers(self) -> dict[str, str | Omit]:
        return {
            **super().default_headers,
            "X-Scalar-Async": "false",
            **self._custom_headers,
        }

    @override
    def _validate_headers(
        self,
        headers: Headers,
        custom_headers: Headers,
        params: Mapping[str, object],
        cookies: Mapping[str, str],
    ) -> None:
        if headers.get("X-API-Key"):
            return
        if isinstance(custom_headers.get("X-API-Key"), Omit):
            return
        if params.get("token") is not None:
            return
        raise TypeError(
            '"Could not resolve authentication method. Expected either rest_api_key, ws_mint_key or ws_token to be set. Or for the `X-API-Key` headers to be explicitly omitted"'
        )

    def copy(
        self,
        *,
        rest_api_key: str | None = None,
        ws_mint_key: str | None = None,
        ws_token: str | None = None,
        websocket_base_url: str | httpx.URL | None = None,
        base_url: str | httpx.URL | None = None,
        timeout: float | Timeout | None | NotGiven = not_given,
        http_client: httpx.Client | None = None,
        max_retries: int | NotGiven = not_given,
        default_headers: Mapping[str, str] | None = None,
        set_default_headers: Mapping[str, str] | None = None,
        default_query: Mapping[str, object] | None = None,
        set_default_query: Mapping[str, object] | None = None,
        _extra_kwargs: Mapping[str, Any] = {},
    ) -> Self:
        """Create a new client reusing this client's options with optional overrides."""
        if default_headers is not None and set_default_headers is not None:
            raise ValueError("The `default_headers` and `set_default_headers` arguments are mutually exclusive")
        if default_query is not None and set_default_query is not None:
            raise ValueError("The `default_query` and `set_default_query` arguments are mutually exclusive")
        headers = self._custom_headers
        if default_headers is not None:
            headers = {**headers, **default_headers}
        elif set_default_headers is not None:
            headers = set_default_headers
        params = self._custom_query
        if default_query is not None:
            params = {**params, **default_query}
        elif set_default_query is not None:
            params = set_default_query
        http_client = http_client or self._client
        return self.__class__(
            rest_api_key=rest_api_key or self.rest_api_key,
            ws_mint_key=ws_mint_key or self.ws_mint_key,
            ws_token=ws_token or self.ws_token,
            websocket_base_url=websocket_base_url or self.websocket_base_url,
            base_url=base_url or self.base_url,
            timeout=self.timeout if isinstance(timeout, NotGiven) else timeout,
            http_client=http_client,
            max_retries=max_retries if is_given(max_retries) else self.max_retries,
            default_headers=headers,
            default_query=params,
            _strict_response_validation=self._strict_response_validation,
            **_extra_kwargs,
        )

    with_options = copy

    @override
    def _make_status_error(self, err_msg: str, *, body: object, response: httpx.Response) -> APIStatusError:
        if response.status_code == 400:
            return _exceptions.BadRequestError(err_msg, response=response, body=body)
        if response.status_code == 401:
            return _exceptions.AuthenticationError(err_msg, response=response, body=body)
        if response.status_code == 403:
            return _exceptions.PermissionDeniedError(err_msg, response=response, body=body)
        if response.status_code == 404:
            return _exceptions.NotFoundError(err_msg, response=response, body=body)
        if response.status_code == 409:
            return _exceptions.ConflictError(err_msg, response=response, body=body)
        if response.status_code == 422:
            return _exceptions.UnprocessableEntityError(err_msg, response=response, body=body)
        if response.status_code == 429:
            return _exceptions.RateLimitError(err_msg, response=response, body=body)
        if response.status_code >= 500:
            return _exceptions.InternalServerError(err_msg, response=response, body=body)
        return APIStatusError(err_msg, response=response, body=body)


class AsyncChainflowDataGateway(AsyncAPIClient):
    # client options
    rest_api_key: str
    ws_mint_key: str | None
    ws_token: str | None

    websocket_base_url: str | httpx.URL | None
    """Base URL for WebSocket connections.

    If not specified, the default base URL will be used, with 'wss://' replacing the
    'http://' or 'https://' scheme. For example: 'http://example.com' becomes
    'wss://example.com'
    """

    def __init__(
        self,
        *,
        rest_api_key: str | None = None,
        ws_mint_key: str | None = None,
        ws_token: str | None = None,
        base_url: str | httpx.URL | None = None,
        websocket_base_url: str | httpx.URL | None = None,
        timeout: float | Timeout | None | NotGiven = not_given,
        max_retries: int = DEFAULT_MAX_RETRIES,
        default_headers: Mapping[str, str] | None = None,
        default_query: Mapping[str, object] | None = None,
        # Configure a custom httpx client.
        # We provide a `DefaultAsyncHttpxClient` class that you can pass to retain the default values we use for `limits`, `timeout` & `follow_redirects`.
        # See the [httpx documentation](https://www.python-httpx.org/api/#asyncclient) for more details.
        http_client: httpx.AsyncClient | None = None,
        # Enable or disable schema validation for data returned by the API.
        # When enabled an error APIResponseValidationError is raised
        # if the API responds with invalid data for the expected schema.
        #
        # This parameter may be removed or changed in the future.
        # If you rely on this feature, please open a GitHub issue
        # outlining your use-case to help us decide if it should be
        # part of our public interface in the future.
        _strict_response_validation: bool = False,
    ) -> None:
        """Construct a new async AsyncChainflowDataGateway client instance.

        This automatically infers the following arguments from their corresponding environment variables if they are not provided:
        - `rest_api_key` from `REST_API_KEY`
        - `ws_mint_key` from `WS_MINT_KEY`
        - `ws_token` from `WS_TOKEN`
        """
        if rest_api_key is None:
            rest_api_key = os.environ.get("REST_API_KEY")
        if rest_api_key is None:
            raise ChainflowDataGatewayError(
                "The rest_api_key client option must be set either by passing rest_api_key to the client or by setting the REST_API_KEY environment variable"
            )
        self.rest_api_key = rest_api_key
        if ws_mint_key is None:
            ws_mint_key = os.environ.get("WS_MINT_KEY")
        self.ws_mint_key = ws_mint_key
        if ws_token is None:
            ws_token = os.environ.get("WS_TOKEN")
        self.ws_token = ws_token
        self.websocket_base_url = websocket_base_url
        if base_url is None:
            base_url = os.environ.get("SIDIORALABS_BASE_URL")
        if base_url is None:
            base_url = "https://data.feed.chainflowtrading.com"
        custom_headers_env = os.environ.get("SIDIORALABS_CUSTOM_HEADERS")
        if custom_headers_env is not None:
            parsed: dict[str, str] = {}
            for line in custom_headers_env.split("\n"):
                colon = line.find(":")
                if colon >= 0:
                    parsed[line[:colon].strip()] = line[colon + 1 :].strip()
            default_headers = {**parsed, **(default_headers if is_mapping_t(default_headers) else {})}
        super().__init__(
            version=__version__,
            base_url=base_url,
            max_retries=max_retries,
            timeout=timeout,
            http_client=http_client,
            custom_headers=default_headers,
            custom_query=default_query,
            _strict_response_validation=_strict_response_validation,
        )
        self._idempotency_header = None
        self._default_stream_cls = AsyncStream

    @cached_property
    def gateway(self) -> "AsyncGatewayResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import AsyncGatewayResource
        return AsyncGatewayResource(self)

    @cached_property
    def price_feeds(self) -> "AsyncPriceFeedsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import AsyncPriceFeedsResource
        return AsyncPriceFeedsResource(self)

    @cached_property
    def trading_view(self) -> "AsyncTradingViewResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import AsyncTradingViewResource
        return AsyncTradingViewResource(self)

    @cached_property
    def markets(self) -> "AsyncMarketsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import AsyncMarketsResource
        return AsyncMarketsResource(self)

    @cached_property
    def explorer(self) -> "AsyncExplorerResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import AsyncExplorerResource
        return AsyncExplorerResource(self)

    @cached_property
    def coin_gecko(self) -> "AsyncCoinGeckoResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import AsyncCoinGeckoResource
        return AsyncCoinGeckoResource(self)

    @cached_property
    def coin_market_cap(self) -> "AsyncCoinMarketCapResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import AsyncCoinMarketCapResource
        return AsyncCoinMarketCapResource(self)

    @cached_property
    def web_sockets(self) -> "AsyncWebSocketsResource":
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import AsyncWebSocketsResource
        return AsyncWebSocketsResource(self)

    @cached_property
    def with_raw_response(self) -> AsyncChainflowDataGatewayWithRawResponse:
        return AsyncChainflowDataGatewayWithRawResponse(self)

    @cached_property
    def with_streaming_response(self) -> AsyncChainflowDataGatewayWithStreamedResponse:
        return AsyncChainflowDataGatewayWithStreamedResponse(self)

    @property
    @override
    def qs(self) -> Querystring:
        return Querystring(array_format="comma")

    @property
    @override
    def auth_headers(self) -> dict[str, str]:
        return {
            **self._rest_api_key_header_auth,
            **self._ws_mint_key_header_auth,
        }

    @override
    def _auth_query(self, security: dict[str, bool]) -> dict[str, str]:
        _ = security
        return {
            **self._ws_token_query_auth,
        }

    @override
    def _auth_cookies(self, security: dict[str, bool]) -> dict[str, str]:
        _ = security
        return {}

    @property
    def _rest_api_key_header_auth(self) -> dict[str, str]:
        value = self.rest_api_key
        if value is None:
            return {}
        return {"X-API-Key": value}

    @property
    def _ws_mint_key_header_auth(self) -> dict[str, str]:
        value = self.ws_mint_key
        if value is None:
            return {}
        return {"X-API-Key": value}

    @property
    def _ws_token_query_auth(self) -> dict[str, str]:
        value = self.ws_token
        if value is None:
            return {}
        return {"token": value}

    @property
    @override
    def default_headers(self) -> dict[str, str | Omit]:
        return {
            **super().default_headers,
            "X-Scalar-Async": f"async:{get_async_library()}",
            **self._custom_headers,
        }

    @override
    def _validate_headers(
        self,
        headers: Headers,
        custom_headers: Headers,
        params: Mapping[str, object],
        cookies: Mapping[str, str],
    ) -> None:
        if headers.get("X-API-Key"):
            return
        if isinstance(custom_headers.get("X-API-Key"), Omit):
            return
        if params.get("token") is not None:
            return
        raise TypeError(
            '"Could not resolve authentication method. Expected either rest_api_key, ws_mint_key or ws_token to be set. Or for the `X-API-Key` headers to be explicitly omitted"'
        )

    def copy(
        self,
        *,
        rest_api_key: str | None = None,
        ws_mint_key: str | None = None,
        ws_token: str | None = None,
        websocket_base_url: str | httpx.URL | None = None,
        base_url: str | httpx.URL | None = None,
        timeout: float | Timeout | None | NotGiven = not_given,
        http_client: httpx.AsyncClient | None = None,
        max_retries: int | NotGiven = not_given,
        default_headers: Mapping[str, str] | None = None,
        set_default_headers: Mapping[str, str] | None = None,
        default_query: Mapping[str, object] | None = None,
        set_default_query: Mapping[str, object] | None = None,
        _extra_kwargs: Mapping[str, Any] = {},
    ) -> Self:
        """Create a new client reusing this client's options with optional overrides."""
        if default_headers is not None and set_default_headers is not None:
            raise ValueError("The `default_headers` and `set_default_headers` arguments are mutually exclusive")
        if default_query is not None and set_default_query is not None:
            raise ValueError("The `default_query` and `set_default_query` arguments are mutually exclusive")
        headers = self._custom_headers
        if default_headers is not None:
            headers = {**headers, **default_headers}
        elif set_default_headers is not None:
            headers = set_default_headers
        params = self._custom_query
        if default_query is not None:
            params = {**params, **default_query}
        elif set_default_query is not None:
            params = set_default_query
        http_client = http_client or self._client
        return self.__class__(
            rest_api_key=rest_api_key or self.rest_api_key,
            ws_mint_key=ws_mint_key or self.ws_mint_key,
            ws_token=ws_token or self.ws_token,
            websocket_base_url=websocket_base_url or self.websocket_base_url,
            base_url=base_url or self.base_url,
            timeout=self.timeout if isinstance(timeout, NotGiven) else timeout,
            http_client=http_client,
            max_retries=max_retries if is_given(max_retries) else self.max_retries,
            default_headers=headers,
            default_query=params,
            _strict_response_validation=self._strict_response_validation,
            **_extra_kwargs,
        )

    with_options = copy

    @override
    def _make_status_error(self, err_msg: str, *, body: object, response: httpx.Response) -> APIStatusError:
        if response.status_code == 400:
            return _exceptions.BadRequestError(err_msg, response=response, body=body)
        if response.status_code == 401:
            return _exceptions.AuthenticationError(err_msg, response=response, body=body)
        if response.status_code == 403:
            return _exceptions.PermissionDeniedError(err_msg, response=response, body=body)
        if response.status_code == 404:
            return _exceptions.NotFoundError(err_msg, response=response, body=body)
        if response.status_code == 409:
            return _exceptions.ConflictError(err_msg, response=response, body=body)
        if response.status_code == 422:
            return _exceptions.UnprocessableEntityError(err_msg, response=response, body=body)
        if response.status_code == 429:
            return _exceptions.RateLimitError(err_msg, response=response, body=body)
        if response.status_code >= 500:
            return _exceptions.InternalServerError(err_msg, response=response, body=body)
        return APIStatusError(err_msg, response=response, body=body)


class ChainflowDataGatewayWithRawResponse:
    _client: ChainflowDataGateway

    def __init__(self, client: ChainflowDataGateway) -> None:
        self._client = client

    @cached_property
    def gateway(self) -> gateway.GatewayResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import GatewayResourceWithRawResponse
        return GatewayResourceWithRawResponse(self._client.gateway)

    @cached_property
    def price_feeds(self) -> price_feeds.PriceFeedsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import PriceFeedsResourceWithRawResponse
        return PriceFeedsResourceWithRawResponse(self._client.price_feeds)

    @cached_property
    def trading_view(self) -> trading_view.TradingViewResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import TradingViewResourceWithRawResponse
        return TradingViewResourceWithRawResponse(self._client.trading_view)

    @cached_property
    def markets(self) -> markets.MarketsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import MarketsResourceWithRawResponse
        return MarketsResourceWithRawResponse(self._client.markets)

    @cached_property
    def explorer(self) -> explorer.ExplorerResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import ExplorerResourceWithRawResponse
        return ExplorerResourceWithRawResponse(self._client.explorer)

    @cached_property
    def coin_gecko(self) -> coin_gecko.CoinGeckoResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import CoinGeckoResourceWithRawResponse
        return CoinGeckoResourceWithRawResponse(self._client.coin_gecko)

    @cached_property
    def coin_market_cap(self) -> coin_market_cap.CoinMarketCapResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import CoinMarketCapResourceWithRawResponse
        return CoinMarketCapResourceWithRawResponse(self._client.coin_market_cap)

    @cached_property
    def web_sockets(self) -> web_sockets.WebSocketsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import WebSocketsResourceWithRawResponse
        return WebSocketsResourceWithRawResponse(self._client.web_sockets)


class AsyncChainflowDataGatewayWithRawResponse:
    _client: AsyncChainflowDataGateway

    def __init__(self, client: AsyncChainflowDataGateway) -> None:
        self._client = client

    @cached_property
    def gateway(self) -> gateway.AsyncGatewayResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import AsyncGatewayResourceWithRawResponse
        return AsyncGatewayResourceWithRawResponse(self._client.gateway)

    @cached_property
    def price_feeds(self) -> price_feeds.AsyncPriceFeedsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import AsyncPriceFeedsResourceWithRawResponse
        return AsyncPriceFeedsResourceWithRawResponse(self._client.price_feeds)

    @cached_property
    def trading_view(self) -> trading_view.AsyncTradingViewResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import AsyncTradingViewResourceWithRawResponse
        return AsyncTradingViewResourceWithRawResponse(self._client.trading_view)

    @cached_property
    def markets(self) -> markets.AsyncMarketsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import AsyncMarketsResourceWithRawResponse
        return AsyncMarketsResourceWithRawResponse(self._client.markets)

    @cached_property
    def explorer(self) -> explorer.AsyncExplorerResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import AsyncExplorerResourceWithRawResponse
        return AsyncExplorerResourceWithRawResponse(self._client.explorer)

    @cached_property
    def coin_gecko(self) -> coin_gecko.AsyncCoinGeckoResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import AsyncCoinGeckoResourceWithRawResponse
        return AsyncCoinGeckoResourceWithRawResponse(self._client.coin_gecko)

    @cached_property
    def coin_market_cap(self) -> coin_market_cap.AsyncCoinMarketCapResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import AsyncCoinMarketCapResourceWithRawResponse
        return AsyncCoinMarketCapResourceWithRawResponse(self._client.coin_market_cap)

    @cached_property
    def web_sockets(self) -> web_sockets.AsyncWebSocketsResourceWithRawResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import AsyncWebSocketsResourceWithRawResponse
        return AsyncWebSocketsResourceWithRawResponse(self._client.web_sockets)


class ChainflowDataGatewayWithStreamedResponse:
    _client: ChainflowDataGateway

    def __init__(self, client: ChainflowDataGateway) -> None:
        self._client = client

    @cached_property
    def gateway(self) -> gateway.GatewayResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import GatewayResourceWithStreamingResponse
        return GatewayResourceWithStreamingResponse(self._client.gateway)

    @cached_property
    def price_feeds(self) -> price_feeds.PriceFeedsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import PriceFeedsResourceWithStreamingResponse
        return PriceFeedsResourceWithStreamingResponse(self._client.price_feeds)

    @cached_property
    def trading_view(self) -> trading_view.TradingViewResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import TradingViewResourceWithStreamingResponse
        return TradingViewResourceWithStreamingResponse(self._client.trading_view)

    @cached_property
    def markets(self) -> markets.MarketsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import MarketsResourceWithStreamingResponse
        return MarketsResourceWithStreamingResponse(self._client.markets)

    @cached_property
    def explorer(self) -> explorer.ExplorerResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import ExplorerResourceWithStreamingResponse
        return ExplorerResourceWithStreamingResponse(self._client.explorer)

    @cached_property
    def coin_gecko(self) -> coin_gecko.CoinGeckoResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import CoinGeckoResourceWithStreamingResponse
        return CoinGeckoResourceWithStreamingResponse(self._client.coin_gecko)

    @cached_property
    def coin_market_cap(self) -> coin_market_cap.CoinMarketCapResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import CoinMarketCapResourceWithStreamingResponse
        return CoinMarketCapResourceWithStreamingResponse(self._client.coin_market_cap)

    @cached_property
    def web_sockets(self) -> web_sockets.WebSocketsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import WebSocketsResourceWithStreamingResponse
        return WebSocketsResourceWithStreamingResponse(self._client.web_sockets)


class AsyncChainflowDataGatewayWithStreamedResponse:
    _client: AsyncChainflowDataGateway

    def __init__(self, client: AsyncChainflowDataGateway) -> None:
        self._client = client

    @cached_property
    def gateway(self) -> gateway.AsyncGatewayResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.gateway import AsyncGatewayResourceWithStreamingResponse
        return AsyncGatewayResourceWithStreamingResponse(self._client.gateway)

    @cached_property
    def price_feeds(self) -> price_feeds.AsyncPriceFeedsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.price_feeds import AsyncPriceFeedsResourceWithStreamingResponse
        return AsyncPriceFeedsResourceWithStreamingResponse(self._client.price_feeds)

    @cached_property
    def trading_view(self) -> trading_view.AsyncTradingViewResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.trading_view import AsyncTradingViewResourceWithStreamingResponse
        return AsyncTradingViewResourceWithStreamingResponse(self._client.trading_view)

    @cached_property
    def markets(self) -> markets.AsyncMarketsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.markets import AsyncMarketsResourceWithStreamingResponse
        return AsyncMarketsResourceWithStreamingResponse(self._client.markets)

    @cached_property
    def explorer(self) -> explorer.AsyncExplorerResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.explorer import AsyncExplorerResourceWithStreamingResponse
        return AsyncExplorerResourceWithStreamingResponse(self._client.explorer)

    @cached_property
    def coin_gecko(self) -> coin_gecko.AsyncCoinGeckoResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_gecko import AsyncCoinGeckoResourceWithStreamingResponse
        return AsyncCoinGeckoResourceWithStreamingResponse(self._client.coin_gecko)

    @cached_property
    def coin_market_cap(self) -> coin_market_cap.AsyncCoinMarketCapResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.coin_market_cap import AsyncCoinMarketCapResourceWithStreamingResponse
        return AsyncCoinMarketCapResourceWithStreamingResponse(self._client.coin_market_cap)

    @cached_property
    def web_sockets(self) -> web_sockets.AsyncWebSocketsResourceWithStreamingResponse:
        with _RESOURCE_IMPORT_LOCK:
            from .resources.web_sockets import AsyncWebSocketsResourceWithStreamingResponse
        return AsyncWebSocketsResourceWithStreamingResponse(self._client.web_sockets)


# Alias names for the documented `Client` / `AsyncClient` symbols.
Client = ChainflowDataGateway
AsyncClient = AsyncChainflowDataGateway
