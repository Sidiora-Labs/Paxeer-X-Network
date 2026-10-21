---
name: chainflow-data-gateway-python-sdk
description: "Python SDK for Chainflow Data Gateway API. Use when writing Python code that calls Chainflow Data Gateway API with the chainflow-data-gateway package: installing it, constructing and authenticating the client, and calling API operations."
---

# Chainflow Data Gateway Python SDK

Generated Python client for Chainflow Data Gateway API, published as `chainflow-data-gateway`. Use the generated client instead of hand-writing HTTP requests.

## Install

```sh
pip install chainflow-data-gateway
```

## Client setup and authentication

```python
import os

from chainflow_data_gateway import ChainflowDataGateway

client = ChainflowDataGateway(
    rest_api_key=os.environ.get("REST_API_KEY"),
)
```

Provide credentials using the options below. Environment variables are read automatically when the target runtime supports them:

- `rest_api_key` (env: `REST_API_KEY`) — REST key; not the WS minting key.
- `ws_mint_key` (env: `WS_MINT_KEY`) — Separate WS key used ONLY to mint symbol-bound credentials.
- `ws_token` (env: `WS_TOKEN`) — Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session.

## Calling operations

```python
import os

from chainflow_data_gateway import ChainflowDataGateway

client = ChainflowDataGateway(
    rest_api_key=os.environ.get("REST_API_KEY"),
)

gateway = client.gateway.list_health()

print(gateway)
```

Method names, parameter shapes, and response types are generated from the API description — do not guess them. Look up the exact call signature in [api.md](./api.md) before writing a call.

## WebSockets

WebSocket endpoints open a persistent connection you can send messages to and receive messages from.

```python
with client.web_sockets.connect_price_feed(market="market") as connection:
    message = connection.recv()
    print(message)
```

## Error handling

Non-success responses throw generated API errors. Error objects expose status, headers, response body, and request metadata where the target runtime supports it.

```python
from chainflow_data_gateway import APIStatusError

try:
    gateway = client.gateway.list_health()
except APIStatusError as err:
    print(err.status_code, err.message)
    raise
```

## Requirements

- Python 3.9 or newer

## Reference files

- [README.md](./README.md) — full feature tour: client options, retries and timeouts, logging.
- [api.md](./api.md) — complete catalogue of every operation with request and response types.
