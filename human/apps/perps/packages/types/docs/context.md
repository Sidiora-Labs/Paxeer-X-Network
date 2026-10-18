# context

## Overview

Types for routing integration: route options and a router adapter used to hook the SDK into the host app’s navigation.

## Exports

### RouteOption

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| href | string | Yes | Route path/URL |
| name | string | Yes | Display or logical name |
| scope | string | No | Optional scope |
| target | string | No | Optional target (e.g. _blank) |

### RouterAdapter

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| onRouteChange | (option: RouteOption) => void | Yes | Called when the SDK wants to navigate |
| currentPath | string | No | Current path for the adapter to report |

## Usage example

```typescript
import type { RouteOption, RouterAdapter } from "@paxeer-network/types";

const adapter: RouterAdapter = {
  onRouteChange: (opt) => history.push(opt.href),
  currentPath: location.pathname,
};
```
