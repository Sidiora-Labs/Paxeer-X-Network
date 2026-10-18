# Chainflow Data Gateway

This library provides convenient access to the Chainflow Data Gateway REST API from TypeScript or JavaScript.

The full API of this library can be found in [api.md](./api.md).

<br />

## Contents

- [Installation](#installation)
- [Usage](#usage)
- [API Reference](./api.md)
- [WebSockets](#websockets)
- [Authentication](#authentication)
- [Errors](#errors)
- [Client Options](#client-options)
- [Request Options](#request-options)
- [Retries and Timeouts](#retries-and-timeouts)
- [Helpers](#helpers)
- [Logging](#logging)
- [Requirements](#requirements)

<br />

## Installation

```sh
npm install @sidioralabs/chainflow-data-gateway
```

<br />

## Usage

```ts
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

const client = new ChainflowDataGateway({
  restAPIKey: process.env['REST_API_KEY'], // defaults to the REST_API_KEY env var
});

const response = await client.gateway.listHealth();

console.log(response);
```

The examples in the following sections assume a `client` configured as shown above.

See the [API reference](./api.md) for every available operation.

<br />

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

<br />

## Authentication

Pass credentials to the generated client constructor. Environment variables are read automatically when supported by the target runtime.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `restAPIKey` | `string \| AuthTokenProvider` | - | REST key; not the WS minting key. Defaults to REST_API_KEY. |
| `wsMintKey` | `string \| AuthTokenProvider \| null` | - | Separate WS key used ONLY to mint symbol-bound credentials. Defaults to WS_MINT_KEY. |
| `wsToken` | `string \| AuthTokenProvider \| null` | - | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. Defaults to WS_TOKEN. |

Declared schemes:

- `RestApiKey` API key in header `X-API-Key`
- `WsMintKey` API key in header `X-API-Key`
- `WsToken` API key in query `token`

<br />

## Errors

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

Documented error statuses: `400`, `401`, `403`, `404`, `405`, `413`, `415`, `426`, `429`, `500`, `502`, `503`, `504`.

<br />

## Client Options

Configure the generated client by setting any of these options when you create it.

```ts
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

const client = new ChainflowDataGateway({
  timeout: 60000,
  maxRetries: 2,
  logLevel: 'debug',
});
```

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `restAPIKey` | `string \| AuthTokenProvider` | `process.env["REST_API_KEY"]` | REST key; not the WS minting key. |
| `wsMintKey` | `string \| AuthTokenProvider \| null` | `process.env["WS_MINT_KEY"]` | Separate WS key used ONLY to mint symbol-bound credentials. |
| `wsToken` | `string \| AuthTokenProvider \| null` | `process.env["WS_TOKEN"]` | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. |
| `baseURL` | `string \| null` | `process.env["SIDIORALABS_BASE_URL"]` | Override the default API base URL. Pass `null` when selecting a configured environment. |
| `timeout` | `number` | `60000` | Maximum time in milliseconds to wait for a response before aborting a request. |
| `maxRetries` | `number` | `2` | Number of retries for temporary failures. |
| `defaultHeaders` | `HeadersInit` | - | Headers sent with every request. |
| `defaultQuery` | `Record<string, string \| undefined>` | - | Query parameters sent with every request. |
| `fetchOptions` | `RequestInit` | - | Additional fetch options sent with every request. |
| `fetch` | `Fetch` | - | Custom fetch implementation. |
| `logLevel` | `"off" \| "error" \| "warn" \| "info" \| "debug" \| null` | `process.env["SIDIORALABS_LOG"]` | Controls request and retry debug logging. |
| `logger` | `Logger \| null` | `console` | Custom logger implementation. |

<br />

## Request Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `headers` | `HeadersInit` | - | Per-request headers. |
| `query` | `Record<string, unknown>` | - | Per-request query parameters. |
| `body` | `unknown` | - | Override the generated request body. |
| `timeout` | `number` | - | Per-request timeout in milliseconds. |
| `maxRetries` | `number` | - | Per-request retry count. |
| `signal` | `AbortSignal` | - | Abort an in-flight request. |
| `fetchOptions` | `RequestInit` | - | Per-request fetch options. |
| `idempotencyKey` | `string` | - | Idempotency key for retry-safe operations. Applies to this request and its retries. |

<br />

## Retries and Timeouts

Generated clients support request timeouts and retry temporary failures such as network errors, 408, 409, 429, and 5xx responses. Retry delays honor `Retry-After` headers when present. Tune the retry and timeout client options shown above, or override them per request.

<br />

## Helpers

- Use `.withResponse()` on any request to inspect both parsed data and the raw `Response` object.
- Every operation returns an `APIPromise`, so you can `await` it directly or chain `.withResponse()`.

<br />

## Logging

- Set `logLevel: "debug"` to log request URLs, options, response status, response headers, and retry attempts.
- Pass a custom `logger` to route logs into your own observability pipeline.
- Set `logLevel: null` to disable environment-driven logging.

<br />

## Requirements

- Node.js 20+, a modern browser, or any runtime with `fetch` support