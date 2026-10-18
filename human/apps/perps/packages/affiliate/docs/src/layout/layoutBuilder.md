# layoutBuilder.tsx

## Overview

Exports a `layoutBuilder` function that returns component state (components map and sideOpen). Used internally for layout composition.

## Exports

| Export | Description |
|--------|-------------|
| `layoutBuilder` | Function that returns an object (e.g. components state) with useState for components and sideOpen |

## Usage Example

```ts
import { layoutBuilder } from "./layoutBuilder";
const state = layoutBuilder();
```
