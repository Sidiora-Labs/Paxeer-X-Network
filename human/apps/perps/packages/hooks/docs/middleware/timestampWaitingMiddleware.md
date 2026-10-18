# timestampWaitingMiddleware

## Overview

SWR middleware that waits for the global `__PAXEER_NETWORK_timestamp_offset` to be initialized before running fetchers. Uses a singleton promise to avoid duplicate polling. Exports `resetTimestampOffsetState` to clear ready state.

## Exports

### `timestampWaitingMiddleware`

`Middleware` — wraps the SWR hook so that the fetcher only runs after timestamp offset is ready.

### `resetTimestampOffsetState`

Function to reset the internal timestamp-offset state (e.g. for tests or re-init).

## Usage example

```ts
import { timestampWaitingMiddleware } from "@paxeer-network/hooks";
import useSWR from "swr";

useSWR(key, fetcher, { use: [timestampWaitingMiddleware] });
```
