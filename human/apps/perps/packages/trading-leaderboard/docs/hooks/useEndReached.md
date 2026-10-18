# useEndReached.ts

## Overview

Hook that observes a sentinel DOM element and invokes a callback when it scrolls into view (e.g. for infinite scroll).

## Exports

### `useEndReached(sentinelRef, onEndReached?)`

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sentinelRef` | `MutableRefObject<HTMLDivElement \| null>` | Yes | Ref to the sentinel element to observe |
| `onEndReached` | `() => void` | No | Callback when the sentinel becomes intersecting |

Uses `IntersectionObserver` with `root: null`, `threshold: 0`.

## Usage example

```tsx
import { useRef } from "react";
import { useEndReached } from "./hooks/useEndReached";

function List() {
  const sentinelRef = useRef<HTMLDivElement>(null);
  useEndReached(sentinelRef, () => loadMore());
  return (
    <>
      {/* list items */}
      <div ref={sentinelRef} />
    </>
  );
}
```
