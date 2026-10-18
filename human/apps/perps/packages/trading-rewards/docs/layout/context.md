# context

## Overview

Provides a React context for the layout sidebar open/close state. Used by the layout to control left sidebar visibility (currently the provider is not exported from the package barrel).

## Exports

### LayoutProvider

React context provider component.

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | ReactNode | Yes | Child tree. |

### LayoutContextValue (type)

| Field | Type | Description |
|-------|------|-------------|
| sideOpen | boolean | Whether the sidebar is open. |
| onSideOpenChange | (open: boolean) => void | Callback to set sidebar open state. |

## Usage example

```tsx
import { LayoutProvider } from "./context";

<LayoutProvider>
  {children}
</LayoutProvider>
```
