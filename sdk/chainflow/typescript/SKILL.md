---
name: chainflow-data-gateway-typescript-sdk
description: "TypeScript SDK for Chainflow Data Gateway API. Use when writing TypeScript code that calls Chainflow Data Gateway API with the @sidioralabs/chainflow-data-gateway package: installing it, constructing and authenticating the client, and calling API operations."
---

# Chainflow Data Gateway TypeScript SDK

Generated TypeScript client for Chainflow Data Gateway API, published as `@sidioralabs/chainflow-data-gateway`. Use the generated client instead of hand-writing HTTP requests.

## Install

```sh
npm install @sidioralabs/chainflow-data-gateway
```

## Client setup and authentication

```ts
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

const client = new ChainflowDataGateway({
  restAPIKey: process.env['REST_API_KEY'], // defaults to the REST_API_KEY env var
});
```

Provide credentials using the options below. Environment variables are read automatically when the target runtime supports them:

- `restAPIKey` (env: `REST_API_KEY`) — REST key; not the WS minting key.
- `wsMintKey` (env: `WS_MINT_KEY`) — Separate WS key used ONLY to mint symbol-bound credentials.
- `wsToken` (env: `WS_TOKEN`) — Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session.

## Calling operations

```ts
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

const client = new ChainflowDataGateway({
  restAPIKey: process.env['REST_API_KEY'], // defaults to the REST_API_KEY env var
});

const response = await client.gateway.listHealth();

console.log(response);
```

Method names, parameter shapes, and response types are generated from the API description — do not guess them. Look up the exact call signature in [api.md](./api.md) before writing a call.

## WebSockets

WebSocket endpoints open a persistent connection you can send messages to and receive messages from.

```ts
const connection = client.webSockets.connectPriceFeed('btc');

try {
  for await (const message of connection) {
    console.log(message);
  }
} finally {
  connection.close();
}
```

## Error handling

Non-success responses throw generated API errors. Error objects expose status, headers, response body, and request metadata where the target runtime supports it.

```ts
import { APIError } from '@sidioralabs/chainflow-data-gateway';

try {
  const response = await client.gateway.listHealth();
} catch (err) {
  if (err instanceof APIError) {
    console.log(err.status, err.name, err.headers);
  }
  throw err;
}
```

## Requirements

- Node.js 20+, a modern browser, or any runtime with `fetch` support

## Reference files

- [README.md](./README.md) — full feature tour: client options, request options, retries and timeouts, logging.
- [api.md](./api.md) — complete catalogue of every operation with request and response types.
