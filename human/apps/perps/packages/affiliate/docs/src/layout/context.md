# context.tsx (layout)

## Overview

Layout context for sidebar open state: `LayoutProvider` provides `sideOpen` and `onSideOpenChange`. Used by layout UI to control sidebar visibility.

## Exports

| Export | Description |
|--------|-------------|
| `LayoutProvider` | Provider component that holds sideOpen state and onSideOpenChange |
| (context not exported) | LayoutContext value: sideOpen, onSideOpenChange |

## Usage Example

```tsx
import { LayoutProvider } from "./context";
<LayoutProvider>
  <LayoutChildren />
</LayoutProvider>
```
