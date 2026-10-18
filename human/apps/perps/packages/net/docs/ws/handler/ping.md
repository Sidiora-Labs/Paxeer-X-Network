# ws/handler/ping.ts

## Overview

Handler for WebSocket `ping` events. Responds by sending a `pong` message with current timestamp to keep the connection alive.

## Exports

### `PingHandler` (default export)

Class extending `BaseHandler`.

| Method | Signature | Description |
| ------ | --------- | ----------- |
| `handle` | `(_: any, webSocket: WebSocket) => void` | Sends `{ event: "pong", ts: Date.now() }` as JSON on the given socket |

## Usage example

Registered in `handler.ts` for event `"ping"`. No direct usage; `WS` uses the handler registry to invoke it when the server sends a ping.
