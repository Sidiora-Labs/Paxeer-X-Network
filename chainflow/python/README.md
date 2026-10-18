# Chainflow Data Gateway

This library provides convenient access to the Chainflow Data Gateway REST API from Python.

The full API of this library can be found in [api.md](./api.md).

<br />

## Contents

- [Installation](#installation)
- [Usage](#usage)
- [API Reference](./api.md)
- [Async](#async)
- [WebSockets](#websockets)
- [Authentication](#authentication)
- [Errors](#errors)
- [Client Options](#client-options)
- [Retries and Timeouts](#retries-and-timeouts)
- [Helpers](#helpers)
- [Logging](#logging)
- [Requirements](#requirements)

<br />

## Installation

```sh
pip install chainflow-data-gateway
```

<br />

## Usage

```python
import os

from chainflow_data_gateway import ChainflowDataGateway

client = ChainflowDataGateway(
    rest_api_key=os.environ.get("REST_API_KEY"),
)

gateway = client.gateway.list_health()

print(gateway)
```

The examples in the following sections assume a `client` configured as shown above.

See the [API reference](./api.md) for every available operation.

<br />

## Async

Every client has an `Async` counterpart (`AsyncChainflowDataGateway`) exposing the same resource tree with `await`.

```python
import asyncio

from chainflow_data_gateway import AsyncChainflowDataGateway


async def main() -> None:
    client = AsyncChainflowDataGateway()
    gateway = await client.gateway.list_health()


asyncio.run(main())
```

<br />

## WebSockets

WebSocket endpoints open a persistent connection you can send messages to and receive messages from.

```python
with client.web_sockets.connect_price_feed(market="market") as connection:
    message = connection.recv()
    print(message)
```

<br />

## Authentication

Pass credentials to the generated client constructor. Environment variables are read automatically when supported by the target runtime.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `rest_api_key` | `str \| None` | - | REST key; not the WS minting key. Defaults to REST_API_KEY. |
| `ws_mint_key` | `str \| None` | - | Separate WS key used ONLY to mint symbol-bound credentials. Defaults to WS_MINT_KEY. |
| `ws_token` | `str \| None` | - | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. Defaults to WS_TOKEN. |

Declared schemes:

- `RestApiKey` API key in header `X-API-Key`
- `WsMintKey` API key in header `X-API-Key`
- `WsToken` API key in query `token`

<br />

## Errors

Non-success responses throw generated API errors. Error objects expose status, headers, response body, and request metadata where the target runtime supports it.

```python
from chainflow_data_gateway import APIStatusError

try:
    gateway = client.gateway.list_health()
except APIStatusError as err:
    print(err.status_code, err.message)
    raise
```

Documented error statuses: `400`, `401`, `403`, `404`, `405`, `413`, `415`, `426`, `429`, `500`, `502`, `503`, `504`.

<br />

## Client Options

Configure the generated client by setting any of these options when you create it.

```python
from chainflow_data_gateway import ChainflowDataGateway

client = ChainflowDataGateway(
    timeout=60.0,
    max_retries=2,
)
```

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `rest_api_key` | `str \| None` | `os.environ.get("REST_API_KEY")` | REST key; not the WS minting key. |
| `ws_mint_key` | `str \| None` | `os.environ.get("WS_MINT_KEY")` | Separate WS key used ONLY to mint symbol-bound credentials. |
| `ws_token` | `str \| None` | `os.environ.get("WS_TOKEN")` | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. |
| `base_url` | `str \| httpx.URL \| None` | - | Override the default API base URL. |
| `timeout` | `float \| Timeout \| None` | `60.0` | Maximum time in seconds to wait for a response before aborting a request. |
| `max_retries` | `int` | `2` | Number of retries for temporary failures. |
| `default_headers` | `Mapping[str, str] \| None` | - | Headers sent with every request. |
| `default_query` | `Mapping[str, object] \| None` | - | Query parameters sent with every request. |

<br />

## Retries and Timeouts

Generated clients support request timeouts and retry temporary failures such as network errors, 408, 409, 429, and 5xx responses. Retry delays honor `Retry-After` headers when present. Tune the retry and timeout client options shown above, or override them per request.

<br />

## Helpers

- Use `client.with_raw_response.<resource>.<method>(...)` to access the raw `httpx.Response` and parse it yourself.
- Use `client.with_streaming_response.<resource>.<method>(...)` to stream a response body without buffering it.

<br />

## Logging

- Set the `SIDIORALABS_LOG` environment variable to `info` or `debug` to enable HTTP logging.
- Logs are emitted through the standard `logging` module under the `chainflow_data_gateway` logger.

<br />

## Requirements

- Python 3.9 or newer
