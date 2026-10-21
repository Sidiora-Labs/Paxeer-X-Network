# Chainflow Data Gateway

This library provides convenient access to the Chainflow Data Gateway REST API from Go.

The full API of this library can be found in [api.md](./api.md).

<br />

## Contents

- [Installation](#installation)
- [Usage](#usage)
- [API Reference](./api.md)
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
go get chainflow-data-gateway
```

<br />

## Usage

```go
package main

import (
 "context"
 "fmt"
 "os"

 sdk "chainflow-data-gateway"
 "chainflow-data-gateway/option"
)

func main() {
 client := sdk.NewClient(
  option.WithRestAPIKey(os.Getenv("REST_API_KEY")),
 )

 gateway, err := client.Gateway.ListHealth(context.Background())
 if err != nil {
  panic(err)
 }

 fmt.Println(gateway)
}
```

The examples in the following sections assume a `client` configured as shown above.

See the [API reference](./api.md) for every available operation.

<br />

## Authentication

Pass credentials to the generated client constructor. Environment variables are read automatically when supported by the target runtime.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `option.WithRestAPIKey` | `func(string) option.RequestOption` | - | REST key; not the WS minting key. Defaults to REST_API_KEY. |
| `option.WithWsMintKey` | `func(string) option.RequestOption` | - | Separate WS key used ONLY to mint symbol-bound credentials. Defaults to WS_MINT_KEY. |
| `option.WithWsToken` | `func(string) option.RequestOption` | - | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. Defaults to WS_TOKEN. |

Declared schemes:

- `RestApiKey` API key in header `X-API-Key`
- `WsMintKey` API key in header `X-API-Key`
- `WsToken` API key in query `token`

<br />

## Errors

Non-success responses return generated API errors. Error objects expose status, headers, response body, and request metadata where the target runtime supports it.

```go
gateway, err := client.Gateway.ListHealth(context.Background())
if err != nil {
 var apiErr *sdk.Error
 if errors.As(err, &apiErr) {
  fmt.Println(apiErr.StatusCode, apiErr.JSON.RawJSON())
 }
 panic(err)
}

// imports: "context", "errors", "fmt", sdk "chainflow-data-gateway"
```

Documented error statuses: `400`, `401`, `403`, `404`, `405`, `413`, `415`, `429`, `500`, `502`, `503`, `504`.

<br />

## Client Options

Configure the generated client by setting any of these options when you create it.

```go
client := sdk.NewClient(
 option.WithBaseURL("https://api.example.com"),
 option.WithMaxRetries(2),
 option.WithRequestTimeout(60*time.Second),
)

// imports: sdk "chainflow-data-gateway", "chainflow-data-gateway/option", "time"
```

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `option.WithRestAPIKey` | `func(string) option.RequestOption` | `os.Getenv("REST_API_KEY")` | REST key; not the WS minting key. |
| `option.WithWsMintKey` | `func(string) option.RequestOption` | `os.Getenv("WS_MINT_KEY")` | Separate WS key used ONLY to mint symbol-bound credentials. |
| `option.WithWsToken` | `func(string) option.RequestOption` | `os.Getenv("WS_TOKEN")` | Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session. |
| `option.WithEnvironmentProductionHttpsGatewayUseWssForWebSocketUpgrades` | `func() option.RequestOption` | - | Select the production_https_gateway_use_wss_for_web_socket_upgrades API environment. |
| `option.WithBaseURL` | `func(string) option.RequestOption` | `os.Getenv("SIDIORALABS_BASE_URL")` | Override the default API base URL. |
| `option.WithRequestTimeout` | `func(time.Duration) option.RequestOption` | - | Maximum time to wait for each request attempt. |
| `option.WithMaxRetries` | `func(int) option.RequestOption` | `2` | Number of retries for temporary failures. |
| `option.WithHTTPClient` | `func(option.HTTPClient) option.RequestOption` | - | Custom HTTP client or transport implementation. |

<br />

## Request Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `option.WithHeader` | `func(string, string) option.RequestOption` | - | Set a per-request header. |
| `option.WithQuery` | `func(string, string) option.RequestOption` | - | Set a per-request query parameter. |
| `option.WithRequestBody` | `func(string, any) option.RequestOption` | - | Override the serialized request body and content type. |
| `option.WithResponseInto` | `func(**http.Response) option.RequestOption` | - | Capture the raw HTTP response. |
| `option.WithResponseBodyInto` | `func(any) option.RequestOption` | - | Override the response deserialization target. |

<br />

## Retries and Timeouts

Generated clients support request timeouts and retry temporary failures such as network errors, 408, 409, 429, and 5xx responses. Retry delays honor `Retry-After` headers when present. Tune the retry and timeout client options shown above, or override them per request.

<br />

## Helpers

- Pass `option.WithResponseInto(&raw)` to capture the underlying `*http.Response` for a request.
- Use the generated `F`, `Null`, `Raw`, `Int`, `String`, `Float`, `Bool`, and `FileParam` helpers when setting optional params.

<br />

## Logging

- Wrap the HTTP client with `option.WithMiddleware(...)` to add request logging or tracing.

<br />

## Requirements

- Go 1.22 or newer
