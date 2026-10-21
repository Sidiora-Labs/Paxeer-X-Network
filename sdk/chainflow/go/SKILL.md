---
name: chainflow-data-gateway-go-sdk
description: "Go SDK for Chainflow Data Gateway API. Use when writing Go code that calls Chainflow Data Gateway API with the chainflow-data-gateway package: installing it, constructing and authenticating the client, and calling API operations."
---

# Chainflow Data Gateway Go SDK

Generated Go client for Chainflow Data Gateway API, published as `chainflow-data-gateway`. Use the generated client instead of hand-writing HTTP requests.

## Install

```sh
go get chainflow-data-gateway
```

## Client setup and authentication

```go
import (
	"context"
	"fmt"

	sdk "chainflow-data-gateway"
)

client := sdk.NewClient()
```

Provide credentials using the options below. Environment variables are read automatically when the target runtime supports them:

- `option.WithRestAPIKey` (env: `REST_API_KEY`) — REST key; not the WS minting key.
- `option.WithWsMintKey` (env: `WS_MINT_KEY`) — Separate WS key used ONLY to mint symbol-bound credentials.
- `option.WithWsToken` (env: `WS_TOKEN`) — Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session.

## Calling operations

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

Method names, parameter shapes, and response types are generated from the API description — do not guess them. Look up the exact call signature in [api.md](./api.md) before writing a call.

## Error handling

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

## Requirements

- Go 1.22 or newer

## Reference files

- [README.md](./README.md) — full feature tour: client options, request options, retries and timeouts, logging.
- [api.md](./api.md) — complete catalogue of every operation with request and response types.
